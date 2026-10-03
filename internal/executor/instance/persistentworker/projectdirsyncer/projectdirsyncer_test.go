package projectdirsyncer

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteTarArchiveRoundTrip(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "data.txt"), []byte("data"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "nested.txt"), []byte("nested"), 0644))
	if err := os.Symlink("data.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	var buf bytes.Buffer
	require.NoError(t, writeTarArchive(&buf, dir))

	entries := map[string]*tar.Header{}
	contents := map[string]string{}

	reader := tar.NewReader(&buf)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		entries[header.Name] = header
		if header.Typeflag == tar.TypeReg {
			body, err := io.ReadAll(reader)
			require.NoError(t, err)
			contents[header.Name] = string(body)
		}
	}

	// No entry for the base directory itself, slash-separated names.
	for name := range entries {
		assert.NotContains(t, name, "\\")
	}
	_, hasDot := entries["."]
	assert.False(t, hasDot)

	require.Contains(t, entries, "run.sh")
	assert.Equal(t, int64(0755), entries["run.sh"].Mode)
	assert.Equal(t, "#!/bin/sh\necho hi\n", contents["run.sh"])

	require.Contains(t, entries, "data.txt")
	assert.Equal(t, int64(0644), entries["data.txt"].Mode)
	assert.Equal(t, "data", contents["data.txt"])

	require.Contains(t, entries, "sub/")
	require.Contains(t, entries, "sub/nested.txt")
	assert.Equal(t, "nested", contents["sub/nested.txt"])

	require.Contains(t, entries, "link.txt")
	assert.Equal(t, byte(tar.TypeSymlink), entries["link.txt"].Typeflag)
	assert.Equal(t, "data.txt", entries["link.txt"].Linkname)
}

func TestShQuote(t *testing.T) {
	assert.Equal(t, `'/tmp/work dir'`, shQuote("/tmp/work dir"))
	assert.Equal(t, `'/tmp/we'\''ird'`, shQuote("/tmp/we'ird"))
}
