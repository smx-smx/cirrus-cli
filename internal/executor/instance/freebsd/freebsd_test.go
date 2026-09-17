package freebsd

import (
	"strings"
	"testing"

	"github.com/cirruslabs/cirrus-cli/pkg/parser/instance"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupportedImageFamilies(t *testing.T) {
	assert.Contains(t, SupportedImageFamilies(), "freebsd-14-4")
}

func TestResolveImageURLFamily(t *testing.T) {
	imageURL, checksumURL, err := resolveImageURL("freebsd-14-4", "")
	require.NoError(t, err)
	assert.Equal(t,
		"https://download.freebsd.org/releases/CI-IMAGES/14.4-RELEASE/amd64/Latest/FreeBSD-14.4-RELEASE-amd64-BASIC-CI.raw.xz",
		imageURL)
	assert.Equal(t,
		"https://download.freebsd.org/releases/CI-IMAGES/14.4-RELEASE/amd64/Latest/CHECKSUM.SHA256",
		checksumURL)
}

func TestResolveImageURLUnknownFamily(t *testing.T) {
	_, _, err := resolveImageURL("freebsd-14-3", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown FreeBSD image family")
}

func TestResolveImageURLExplicitURL(t *testing.T) {
	imageURL, checksumURL, err := resolveImageURL("", "https://download.freebsd.org/releases/CI-IMAGES/14.4-RELEASE/amd64/Latest/FreeBSD-14.4-RELEASE-amd64-BASIC-CI.raw.xz")
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(imageURL, ".raw.xz"))
	assert.True(t, strings.HasSuffix(checksumURL, "CHECKSUM.SHA256"))
}

func TestResolveImageURLBareName(t *testing.T) {
	_, _, err := resolveImageURL("", "FreeBSD-14.4-RELEASE-amd64-BASIC-CI.raw.xz")
	require.Error(t, err)
}

func TestMatchChecksumLine(t *testing.T) {
	// BSD flavor, as shipped by download.freebsd.org
	hash, ok := matchChecksumLine(
		"SHA256 (FreeBSD-14.4-RELEASE-amd64-BASIC-CI.raw.xz) = 627937af554eb6522891875ddc8cf5b86b40e9a12143c1b951f830b3f691de49",
		"FreeBSD-14.4-RELEASE-amd64-BASIC-CI.raw.xz")
	require.True(t, ok)
	assert.Equal(t, "627937af554eb6522891875ddc8cf5b86b40e9a12143c1b951f830b3f691de49", hash)

	// GNU coreutils flavor
	hash, ok = matchChecksumLine(
		"627937af554eb6522891875ddc8cf5b86b40e9a12143c1b951f830b3f691de49  FreeBSD-14.4-RELEASE-amd64-BASIC-CI.raw.xz",
		"FreeBSD-14.4-RELEASE-amd64-BASIC-CI.raw.xz")
	require.True(t, ok)
	assert.Equal(t, "627937af554eb6522891875ddc8cf5b86b40e9a12143c1b951f830b3f691de49", hash)

	// Other files don't match
	_, ok = matchChecksumLine(
		"SHA256 (FreeBSD-14.4-RELEASE-amd64-BASIC-CI-ufs.raw.xz) = 627937af554eb6522891875ddc8cf5b86b40e9a12143c1b951f830b3f691de49",
		"FreeBSD-14.4-RELEASE-amd64-BASIC-CI.raw.xz")
	assert.False(t, ok)
}

func TestConfigFromEnvironment(t *testing.T) {
	_, ok := ConfigFromEnvironment(map[string]string{})
	assert.False(t, ok)

	config, ok := ConfigFromEnvironment(map[string]string{
		instance.EnvFreeBSDImageFamily: "freebsd-14-4",
		instance.EnvFreeBSDCPU:         "4",
		instance.EnvFreeBSDMemory:      "8192",
	})
	require.True(t, ok)
	assert.Equal(t, "freebsd-14-4", config.ImageFamily)
	assert.Equal(t, 4, config.CPU)
	assert.Equal(t, uint32(8192), config.Memory)
}

func TestConfigFromEnvironmentDefaults(t *testing.T) {
	config, ok := ConfigFromEnvironment(map[string]string{
		instance.EnvFreeBSDImageFamily: "freebsd-14-4",
	})
	require.True(t, ok)
	assert.Equal(t, defaultCPU, config.CPU)
	assert.Equal(t, uint32(defaultMemory), config.Memory)
}
