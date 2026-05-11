package github

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type UploadResult struct {
	Uploaded []string
	Failed   []UploadFailure
}

type UploadFailure struct {
	TaskName string
	Err      error
}

func UploadArtifactsFromDir(ctx context.Context, artifactsDir string) (*UploadResult, error) {
	entries, err := os.ReadDir(artifactsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read artifacts directory: %w", err)
	}

	result := &UploadResult{}

	if len(entries) == 0 {
		return result, nil
	}

	var client *GHAClient

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		taskName := entry.Name()
		taskDir := filepath.Join(artifactsDir, taskName)

		if !dirHasFiles(taskDir) {
			continue
		}

		if client == nil {
			client, err = NewGHAClient()
			if err != nil {
				return nil, fmt.Errorf("failed to create GHA artifact client: %w", err)
			}
		}

		if err := uploadTaskArtifacts(client, taskName, taskDir); err != nil {
			result.Failed = append(result.Failed, UploadFailure{TaskName: taskName, Err: err})
			continue
		}

		result.Uploaded = append(result.Uploaded, taskName)
	}

	return result, nil
}

func dirHasFiles(dir string) bool {
	found := false
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func uploadTaskArtifacts(client *GHAClient, taskName, taskDir string) error {
	tmpFile, err := os.CreateTemp("", "cirrus-gha-artifact-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	hash, size, err := zipDir(taskDir, tmpFile)
	if err != nil {
		return err
	}

	if size == 0 {
		return nil
	}

	if _, err := tmpFile.Seek(0, 0); err != nil {
		return err
	}

	createResp, err := client.CreateArtifact(taskName)
	if err != nil {
		return err
	}

	if err := client.UploadBlob(createResp.SignedUploadURL, tmpFile, size, hash); err != nil {
		return err
	}

	return client.FinalizeArtifact(taskName, size, hash)
}

func zipDir(sourceDir string, destFile *os.File) (string, int64, error) {
	hasher := sha256.New()
	hashWriter := io.MultiWriter(destFile, hasher)

	zw := zip.NewWriter(hashWriter)

	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = relPath
		header.Method = zip.Deflate

		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		if _, err := io.Copy(w, file); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return "", 0, err
	}

	if err := zw.Close(); err != nil {
		return "", 0, err
	}

	info, err := destFile.Stat()
	if err != nil {
		return "", 0, err
	}

	hash := fmt.Sprintf("sha256:%x", hasher.Sum(nil))

	return hash, info.Size(), nil
}
