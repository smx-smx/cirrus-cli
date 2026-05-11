package github

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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

	return &GHACacheClient{
		baseURL:      resultsURL,
		runtimeToken: token,
		httpClient:   &http.Client{},
	}, nil
}

type cacheScope struct {
	Scope      string `json:"scope"`
	Permission string `json:"permission"`
}

type cacheMetadata struct {
	RepositoryID string       `json:"repository_id"`
	Scopes       []cacheScope `json:"scope"`
}

type createCacheEntryRequest struct {
	Metadata cacheMetadata `json:"metadata"`
	Key      string        `json:"key"`
	Version  string        `json:"version"`
}

type createCacheEntryResponse struct {
	Ok              bool   `json:"ok"`
	SignedUploadURL string `json:"signed_upload_url"`
}

type getDownloadURLRequest struct {
	Metadata    cacheMetadata `json:"metadata"`
	Key         string        `json:"key"`
	RestoreKeys []string      `json:"restore_keys"`
	Version     string        `json:"version"`
}

type getDownloadURLResponse struct {
	Ok                 bool   `json:"ok"`
	SignedDownloadURL  string `json:"signed_download_url"`
	MatchedKey         string `json:"matched_key"`
}

type finalizeUploadRequest struct {
	Metadata  cacheMetadata `json:"metadata"`
	Key       string        `json:"key"`
	SizeBytes int64         `json:"size_bytes"`
	Version   string        `json:"version"`
}

type finalizeUploadResponse struct {
	Ok      bool  `json:"ok"`
	EntryID int64 `json:"entry_id"`
}

func (c *GHACacheClient) metadata() cacheMetadata {
	var scopes []cacheScope

	scopesFromJWT := parseCacheScopes(c.runtimeToken)
	if scopesFromJWT != nil {
		scopes = scopesFromJWT
	}

	repoID := os.Getenv("GITHUB_REPOSITORY_ID")

	return cacheMetadata{
		RepositoryID: repoID,
		Scopes:       scopes,
	}
}

func (c *GHACacheClient) CreateCacheEntry(key, version string) (*createCacheEntryResponse, error) {
	req := createCacheEntryRequest{
		Metadata: c.metadata(),
		Key:      key,
		Version:  version,
	}

	resp := &createCacheEntryResponse{}
	if err := c.twirpCall("CreateCacheEntry", req, resp); err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("CreateCacheEntry: response from backend was not ok")
	}

	return resp, nil
}

func (c *GHACacheClient) GetCacheEntryDownloadURL(key, version string, restoreKeys []string) (*getDownloadURLResponse, error) {
	req := getDownloadURLRequest{
		Metadata:    c.metadata(),
		Key:         key,
		RestoreKeys: restoreKeys,
		Version:     version,
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
		Metadata:  c.metadata(),
		Key:       key,
		SizeBytes: sizeBytes,
		Version:   version,
	}

	resp := &finalizeUploadResponse{}
	if err := c.twirpCall("FinalizeCacheEntryUpload", req, resp); err != nil {
		return err
	}

	if !resp.Ok {
		return fmt.Errorf("FinalizeCacheEntryUpload: response from backend was not ok")
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

func parseCacheScopes(token string) []cacheScope {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}

	var claims struct {
		Scp string `json:"scp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}

	var scopes []cacheScope
	for _, scope := range strings.Split(claims.Scp, " ") {
		if !strings.Contains(scope, ":") {
			continue
		}

		parts := strings.SplitN(scope, ":", 2)
		scopes = append(scopes, cacheScope{
			Scope:      parts[0],
			Permission: parts[1],
		})
	}

	if len(scopes) == 0 {
		return nil
	}

	return scopes
}
