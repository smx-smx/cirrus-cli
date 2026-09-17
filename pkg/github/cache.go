package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

const cacheServicePath = "github.actions.results.api.v1.CacheService"

type GHACacheClient struct {
	baseURL      string
	runtimeToken string
	httpClient   *http.Client
}

func NewGHACacheClient() (*GHACacheClient, error) {
	resultsURL := os.Getenv(EnvResultsURL)
	if resultsURL == "" {
		return nil, fmt.Errorf("%s is not set", EnvResultsURL)
	}

	token := os.Getenv(EnvRuntimeToken)
	if token == "" {
		return nil, fmt.Errorf("%s is not set", EnvRuntimeToken)
	}

	// ACTIONS_RESULTS_URL contains a path prefix (e.g. /v2/runs/...),
	// so strip it down to scheme://host like the artifact client does.
	u, err := url.Parse(resultsURL)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", EnvResultsURL, err)
	}

	return &GHACacheClient{
		baseURL:      u.Scheme + "://" + u.Host,
		runtimeToken: token,
		httpClient:   &http.Client{},
	}, nil
}

// The request shapes below mirror the official @actions/cache Twirp client
// (protobuf field names, JSON encoding). Notably, no metadata is sent: the
// service derives the repository and scopes from the Bearer token. Sending
// metadata with mistyped fields (e.g. repository_id as a string) makes the
// service reject the call with HTTP 400 "malformed".

type createCacheEntryRequest struct {
	Key     string `json:"key"`
	Version string `json:"version"`
}

type createCacheEntryResponse struct {
	Ok              bool   `json:"ok"`
	SignedUploadURL string `json:"signed_upload_url"`
	Message         string `json:"message"`
}

type getDownloadURLRequest struct {
	Key         string   `json:"key"`
	RestoreKeys []string `json:"restore_keys"`
	Version     string   `json:"version"`
}

type getDownloadURLResponse struct {
	Ok                bool   `json:"ok"`
	SignedDownloadURL string `json:"signed_download_url"`
	MatchedKey        string `json:"matched_key"`
}

type finalizeUploadRequest struct {
	Key       string `json:"key"`
	SizeBytes int64  `json:"size_bytes"`
	Version   string `json:"version"`
}

type finalizeUploadResponse struct {
	Ok      bool   `json:"ok"`
	EntryID int64  `json:"entry_id,string"`
	Message string `json:"message"`
}

func (c *GHACacheClient) CreateCacheEntry(key, version string) (*createCacheEntryResponse, error) {
	req := createCacheEntryRequest{
		Key:     key,
		Version: version,
	}

	resp := &createCacheEntryResponse{}
	if err := c.twirpCall("CreateCacheEntry", req, resp); err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("CreateCacheEntry: response from backend was not ok: %s", resp.Message)
	}

	return resp, nil
}

func (c *GHACacheClient) GetCacheEntryDownloadURL(key, version string, restoreKeys []string) (*getDownloadURLResponse, error) {
	req := getDownloadURLRequest{
		Key:         key,
		RestoreKeys: restoreKeys,
		Version:     version,
	}
	if req.RestoreKeys == nil {
		req.RestoreKeys = []string{}
	}

	resp := &getDownloadURLResponse{}
	if err := c.twirpCall("GetCacheEntryDownloadURL", req, resp); err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("GetCacheEntryDownloadURL: response from backend was not ok")
	}

	return resp, nil
}

func (c *GHACacheClient) FinalizeCacheEntryUpload(key, version string, sizeBytes int64) error {
	req := finalizeUploadRequest{
		Key:       key,
		SizeBytes: sizeBytes,
		Version:   version,
	}

	resp := &finalizeUploadResponse{}
	if err := c.twirpCall("FinalizeCacheEntryUpload", req, resp); err != nil {
		return err
	}

	if !resp.Ok {
		return fmt.Errorf("FinalizeCacheEntryUpload: response from backend was not ok: %s", resp.Message)
	}

	return nil
}

func (c *GHACacheClient) twirpCall(method string, req, resp interface{}) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	twirpURL := fmt.Sprintf("%s/twirp/%s/%s", c.baseURL, cacheServicePath, method)

	httpReq, err := http.NewRequest("POST", twirpURL, bytes.NewReader(body))
	if err != nil {
		return err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.runtimeToken)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return err
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return fmt.Errorf("twirp call %s/%s failed (HTTP %d): %s", cacheServicePath, method, httpResp.StatusCode, string(respBody))
	}

	if err := json.Unmarshal(respBody, resp); err != nil {
		return fmt.Errorf("failed to parse %s/%s response: %w (body: %s)", cacheServicePath, method, err, string(respBody))
	}

	return nil
}
