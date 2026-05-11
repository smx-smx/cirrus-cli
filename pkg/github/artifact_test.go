package github

import (
	"archive/zip"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeBackendIDs(t *testing.T) {
	token := "header.eyJzY3AiOiJBY3Rpb25zLlJlc3VsdHM6cnVuLTEyMzpqb2ItNDU2In0.dummy"

	runID, jobID, err := decodeBackendIDs(token)
	require.NoError(t, err)
	assert.Equal(t, "run-123", runID)
	assert.Equal(t, "job-456", jobID)
}

func TestDecodeBackendIDsMultipleScopes(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"scp":"Actions.OtherScope Actions.Results:aaa:bbb Actions.ThirdScope"}`))
	token := "header." + payload + ".dummy"

	runID, jobID, err := decodeBackendIDs(token)
	require.NoError(t, err)
	assert.Equal(t, "aaa", runID)
	assert.Equal(t, "bbb", jobID)
}

func TestDecodeBackendIDsMissingScope(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"scp":"Actions.OtherScope"}`))
	token := "header." + payload + ".dummy"

	_, _, err := decodeBackendIDs(token)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "Actions.Results scope not found")
}

func TestDecodeBackendIDsInvalidScopeFormat(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"scp":"Actions.Results:only-two"}`))
	token := "header." + payload + ".dummy"

	_, _, err := decodeBackendIDs(token)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "invalid Actions.Results scope format")
}

func TestDecodeBackendIDsInvalidFormat(t *testing.T) {
	_, _, err := decodeBackendIDs("not-a-jwt")
	assert.Error(t, err)
	assert.ErrorContains(t, err, "invalid JWT token format")
}

func TestDecodeBackendIDsInvalidBase64(t *testing.T) {
	_, _, err := decodeBackendIDs("header.!!!invalid-base64!!!.sig")
	assert.Error(t, err)
	assert.ErrorContains(t, err, "failed to decode JWT payload")
}

func TestDecodeBackendIDsInvalidJSON(t *testing.T) {
	_, _, err := decodeBackendIDs("header.bm90LWpzb24.sig")
	assert.Error(t, err)
	assert.ErrorContains(t, err, "failed to parse JWT claims")
}

func TestDecodeBackendIDsNoScpClaim(t *testing.T) {
	token := "header.eyJvdGhlciI6InZhbHVlIn0.sig"
	_, _, err := decodeBackendIDs(token)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "Actions.Results scope not found")
}

func TestDirHasFiles(t *testing.T) {
	dir := t.TempDir()

	assert.False(t, dirHasFiles(dir))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0600))
	assert.True(t, dirHasFiles(dir))

	emptyDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(emptyDir, "subdir"), 0700))
	assert.False(t, dirHasFiles(emptyDir))
}

func TestZipDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("content a"), 0600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("content b"), 0600))

	zipFile, err := os.CreateTemp("", "test-zip-*.zip")
	require.NoError(t, err)
	defer os.Remove(zipFile.Name())
	defer zipFile.Close()

	hash, size, err := zipDir(dir, zipFile)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.True(t, size > 0)
	assert.Contains(t, hash, "sha256:")

	zipFile.Seek(0, 0)
	filesInZip, err := listZipEntries(zipFile.Name())
	require.NoError(t, err)
	assert.Contains(t, filesInZip, "a.txt")
	assert.Contains(t, filesInZip, "sub/b.txt")
}

func TestZipDirEmpty(t *testing.T) {
	dir := t.TempDir()

	zipFile, err := os.CreateTemp("", "test-zip-*.zip")
	require.NoError(t, err)
	defer os.Remove(zipFile.Name())
	defer zipFile.Close()

	hash, _, err := zipDir(dir, zipFile)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestUploadArtifactsFromDirNoEnv(t *testing.T) {
	dir := t.TempDir()
	result, err := UploadArtifactsFromDir(t.Context(), dir)
	assert.NoError(t, err)
	assert.Empty(t, result.Uploaded)
	assert.Empty(t, result.Failed)
}

func TestUploadArtifactsFromDirMissingEnv(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0600))

	result, err := UploadArtifactsFromDir(t.Context(), dir)
	assert.NoError(t, err)
	assert.Empty(t, result.Uploaded)
	assert.Empty(t, result.Failed)
}

func TestUploadArtifactsFromDirHasSubdirsNoEnv(t *testing.T) {
	dir := t.TempDir()
	taskDir := filepath.Join(dir, "test-task")
	require.NoError(t, os.MkdirAll(taskDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(taskDir, "data.txt"), []byte("hello"), 0600))

	_, err := UploadArtifactsFromDir(t.Context(), dir)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "ACTIONS_RESULTS_URL")
}

func listZipEntries(zipPath string) ([]string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var names []string
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	return names, nil
}
