package freebsd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/cirruslabs/echelon"
)

// Official FreeBSD BASIC-CI images, purpose-built for running test suites
// on Linux-hosted CI infrastructure (sshd and DHCP out of the box).
//
// See https://www.freebsd.org/where/ ("BASIC-CI images are minimal FreeBSD
// builds [...] to enable custom automated scripting on startup. These are
// used by other projects to run test suites").
//
// Note: this is as close to the images Cirrus CI used as one can get
// outside of GCP: on Cirrus Cloud freebsd_instance was sugar for a
// compute_engine_instance with image_project freebsd-org-cloud-dev,
// which is not downloadable without GCP credentials.
const imageBaseURL = "https://download.freebsd.org/releases/CI-IMAGES"

// imageFamily maps a freebsd_instance image_family to the upstream release
// directory and BASIC-CI image file name.
var imageFamilies = map[string]struct {
	ReleaseDir string
	File       string
}{
	"freebsd-14-4": {
		ReleaseDir: "14.4-RELEASE",
		File:       "FreeBSD-14.4-RELEASE-amd64-BASIC-CI.raw.xz",
	},
	"freebsd-14-5": {
		ReleaseDir: "14.5-RELEASE",
		File:       "FreeBSD-14.5-RELEASE-amd64-BASIC-CI.raw.xz",
	},
	"freebsd-15-0": {
		ReleaseDir: "15.0-RELEASE",
		File:       "FreeBSD-15.0-RELEASE-amd64-BASIC-CI.raw.xz",
	},
	"freebsd-15-1": {
		ReleaseDir: "15.1-RELEASE",
		File:       "FreeBSD-15.1-RELEASE-amd64-BASIC-CI.raw.xz",
	},
}

// SupportedImageFamilies returns the image_family values known to the backend.
func SupportedImageFamilies() []string {
	families := make([]string, 0, len(imageFamilies))
	for family := range imageFamilies {
		families = append(families, family)
	}
	return families
}

// resolveImageURL maps an image_family (or an image_name override) to the
// image archive URL and the URL of the CHECKSUM.SHA256 file that covers it.
func resolveImageURL(imageFamily string, imageName string) (string, string, error) {
	// Explicit image: a full HTTPS URL to a .raw.xz image.
	if imageName != "" {
		if !strings.HasPrefix(imageName, "https://") {
			return "", "", fmt.Errorf("%w: image_name must be a full HTTPS URL to a .raw.xz image "+
				"(e.g. https://download.freebsd.org/releases/CI-IMAGES/14.4-RELEASE/amd64/Latest/FreeBSD-14.4-RELEASE-amd64-BASIC-CI.raw.xz), got %q",
				ErrFailed, imageName)
		}
		checksumURL := resolveChecksumURL(imageName)
		if checksumURL == "" {
			return "", "", fmt.Errorf("%w: cannot derive a checksum URL from %q",
				ErrFailed, imageName)
		}
		return imageName, checksumURL, nil
	}

	family, ok := imageFamilies[imageFamily]
	if !ok {
		return "", "", fmt.Errorf("%w: unknown FreeBSD image family %q (supported: %s), "+
			"or use image_name with a full HTTPS URL to a .raw.xz image",
			ErrFailed, imageFamily, strings.Join(SupportedImageFamilies(), ", "))
	}

	imageURL := fmt.Sprintf("%s/%s/amd64/Latest/%s", imageBaseURL, family.ReleaseDir, family.File)
	return imageURL, candidateChecksumURL(family.ReleaseDir), nil
}

func candidateChecksumURL(releaseDir string) string {
	return fmt.Sprintf("%s/%s/amd64/Latest/CHECKSUM.SHA256", imageBaseURL, releaseDir)
}

func resolveChecksumURL(imageURL string) string {
	u, err := url.Parse(imageURL)
	if err != nil {
		return ""
	}
	u.Path = path.Join(path.Dir(u.Path), "CHECKSUM.SHA256")
	return u.String()
}

// ensureImage downloads (unless cached), checksum-verifies and decompresses
// the FreeBSD image, returning the path to the raw disk image.
func ensureImage(ctx context.Context, logger *echelon.Logger, imageFamily string, imageName string) (string, error) {
	imageURL, checksumURL, err := resolveImageURL(imageFamily, imageName)
	if err != nil {
		return "", err
	}

	cacheDir, err := imageCacheDir()
	if err != nil {
		return "", err
	}

	archiveName := path.Base(imageURL)
	archivePath := filepath.Join(cacheDir, archiveName)
	rawPath := strings.TrimSuffix(archivePath, ".xz")

	if _, err := os.Stat(rawPath); err == nil {
		logger.Debugf("re-using cached FreeBSD image %s", rawPath)
		return rawPath, nil
	}

	downloadLogger := logger.Scoped("downloading FreeBSD image")
	downloadLogger.Infof("from %s...", imageURL)
	if err := downloadFile(ctx, imageURL, archivePath+".tmp"); err != nil {
		downloadLogger.Finish(false)
		return "", err
	}
	downloadLogger.Finish(true)

	checksumLogger := logger.Scoped("verifying FreeBSD image checksum")
	if err := func() error {
		expectedSHA256, err := fetchChecksum(ctx, checksumURL, archiveName)
		if err != nil {
			return err
		}
		return verifySHA256(archivePath+".tmp", expectedSHA256)
	}(); err != nil {
		checksumLogger.Finish(false)
		_ = os.Remove(archivePath + ".tmp")
		return "", err
	}
	checksumLogger.Finish(true)
	if err := os.Rename(archivePath+".tmp", archivePath); err != nil {
		return "", err
	}

	decompressLogger := logger.Scoped("decompressing FreeBSD image")
	if err := decompressXZ(ctx, archivePath, rawPath+".tmp"); err != nil {
		decompressLogger.Finish(false)
		return "", err
	}
	decompressLogger.Finish(true)
	if err := os.Rename(rawPath+".tmp", rawPath); err != nil {
		return "", err
	}

	return rawPath, nil
}

func imageCacheDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cacheDir, "cirrus", "freebsd-images")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

func downloadFile(ctx context.Context, rawURL string, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: failed to download %s: %v", ErrFailed, rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: failed to download %s: HTTP %d", ErrFailed, rawURL, resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("%w: failed to download %s: %v", ErrFailed, rawURL, err)
	}

	return nil
}

func fetchChecksum(ctx context.Context, checksumURL string, archiveName string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: failed to download %s: %v", ErrFailed, checksumURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: failed to download %s: HTTP %d", ErrFailed, checksumURL, resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if hash, ok := matchChecksumLine(scanner.Text(), archiveName); ok {
			return hash, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("%w: failed to read %s: %v", ErrFailed, checksumURL, err)
	}

	return "", fmt.Errorf("%w: no checksum found for %s in %s", ErrFailed, archiveName, checksumURL)
}

// matchChecksumLine matches both checksum file flavors in the wild:
// GNU coreutils ("<sha256>  <filename>") and BSD ("SHA256 (<filename>) = <sha256>",
// which is what download.freebsd.org ships).
func matchChecksumLine(line string, archiveName string) (string, bool) {
	fields := strings.Fields(line)

	// GNU: "<sha256>  <filename>"
	if len(fields) == 2 && fields[1] == archiveName {
		return fields[0], true
	}

	// BSD: "SHA256 (<filename>) = <sha256>"
	if len(fields) == 4 && fields[0] == "SHA256" &&
		strings.Trim(fields[1], "()") == archiveName && fields[2] == "=" {
		return fields[3], true
	}

	return "", false
}

func verifySHA256(filePath string, expectedHex string) error {
	expected, err := hex.DecodeString(expectedHex)
	if err != nil {
		return fmt.Errorf("%w: invalid expected checksum %q: %v", ErrFailed, expectedHex, err)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return err
	}

	if !equalBytes(sum.Sum(nil), expected) {
		return fmt.Errorf("%w: checksum mismatch for %s", ErrFailed, filePath)
	}

	return nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func decompressXZ(ctx context.Context, src string, dest string) error {
	xzPath, err := exec.LookPath("xz")
	if err != nil {
		return fmt.Errorf("%w: the xz utility is required to decompress the FreeBSD image "+
			"(e.g. sudo apt-get install -y xz-utils): %v", ErrFailed, err)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	//nolint:gosec // xzPath comes from LookPath, src is a cache path we control
	cmd := exec.CommandContext(ctx, xzPath, "-d", "-c", src)
	cmd.Stdout = out

	if err := cmd.Run(); err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("%w: failed to decompress %s: %v", ErrFailed, src, err)
	}

	return nil
}
