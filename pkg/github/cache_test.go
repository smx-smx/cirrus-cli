package github

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The service derives repository/scopes from the Bearer token (like the
// official @actions/cache client, which sends no metadata), so the requests
// must not contain a metadata object at all.
func TestCacheRequestShapes(t *testing.T) {
	createBytes, err := json.Marshal(createCacheEntryRequest{Key: "k", Version: "1"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"k","version":"1"}`, string(createBytes))

	downloadBytes, err := json.Marshal(getDownloadURLRequest{Key: "k", RestoreKeys: []string{}, Version: "1"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"k","restore_keys":[],"version":"1"}`, string(downloadBytes))

	finalizeBytes, err := json.Marshal(finalizeUploadRequest{Key: "k", SizeBytes: 42, Version: "1"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"k","size_bytes":42,"version":"1"}`, string(finalizeBytes))
}

func TestCacheResponseShapes(t *testing.T) {
	var createResp createCacheEntryResponse
	require.NoError(t, json.Unmarshal([]byte(
		`{"ok":true,"signed_upload_url":"https://example.invalid/u","message":""}`), &createResp))
	assert.True(t, createResp.Ok)
	assert.Equal(t, "https://example.invalid/u", createResp.SignedUploadURL)

	var downloadResp getDownloadURLResponse
	require.NoError(t, json.Unmarshal([]byte(
		`{"ok":true,"signed_download_url":"https://example.invalid/d","matched_key":"k"}`), &downloadResp))
	assert.True(t, downloadResp.Ok)
	assert.Equal(t, "k", downloadResp.MatchedKey)

	var finalizeResp finalizeUploadResponse
	require.NoError(t, json.Unmarshal([]byte(`{"ok":true,"entry_id":"123"}`), &finalizeResp))
	assert.True(t, finalizeResp.Ok)
	assert.Equal(t, int64(123), finalizeResp.EntryID)
}
