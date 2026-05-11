package github

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type GHAClient struct {
	baseURL      string
	runtimeToken string
	runBackendID string
	jobBackendID string
	httpClient   *http.Client
}

func NewGHAClient() (*GHAClient, error) {
	resultsURL := os.Getenv(EnvResultsURL)
	if resultsURL == "" {
		return nil, fmt.Errorf("%s is not set", EnvResultsURL)
	}

	token := os.Getenv(EnvRuntimeToken)
	if token == "" {
		return nil, fmt.Errorf("%s is not set", EnvRuntimeToken)
	}

	u, err := url.Parse(resultsURL)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", EnvResultsURL, err)
	}
	baseURL := u.Scheme + "://" + u.Host

	runBackendID, jobBackendID, err := decodeBackendIDs(token)
	if err != nil {
		return nil, fmt.Errorf("failed to decode runtime token: %w", err)
	}

	return &GHAClient{
		baseURL:      baseURL,
		runtimeToken: token,
		runBackendID: runBackendID,
		jobBackendID: jobBackendID,
		httpClient:   &http.Client{},
	}, nil
}

func decodeBackendIDs(token string) (string, string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", fmt.Errorf("invalid JWT token format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims struct {
		Scp string `json:"scp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", "", fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	for _, scope := range strings.Split(claims.Scp, " ") {
		if strings.HasPrefix(scope, "Actions.Results:") {
			parts := strings.Split(scope, ":")
			if len(parts) != 3 {
				return "", "", fmt.Errorf("invalid Actions.Results scope format: %s", scope)
			}
			return parts[1], parts[2], nil
		}
	}

	return "", "", fmt.Errorf("Actions.Results scope not found in token")
}

type createArtifactRequest struct {
	Version                 int    `json:"version"`
	Name                    string `json:"name"`
	WorkflowRunBackendID    string `json:"workflow_run_backend_id"`
	WorkflowJobRunBackendID string `json:"workflow_job_run_backend_id"`
	MimeType                string `json:"mime_type"`
	ExpiresAfter            string `json:"expires_after,omitempty"`
}

type createArtifactResponse struct {
	Ok              bool   `json:"ok"`
	SignedUploadURL string `json:"signed_upload_url"`
}

type finalizeArtifactRequest struct {
	Name                    string `json:"name"`
	Size                    int64  `json:"size"`
	Hash                    string `json:"hash,omitempty"`
	WorkflowRunBackendID    string `json:"workflow_run_backend_id"`
	WorkflowJobRunBackendID string `json:"workflow_job_run_backend_id"`
}

type finalizeArtifactResponse struct {
	Ok         bool  `json:"ok"`
	ArtifactID int64 `json:"artifact_id,string"`
}

func (c *GHAClient) CreateArtifact(name string) (*createArtifactResponse, error) {
	req := createArtifactRequest{
		WorkflowRunBackendID:    c.runBackendID,
		WorkflowJobRunBackendID: c.jobBackendID,
		Name:     name,
		MimeType: "application/zip",
		Version:  7,
	}

	resp := &createArtifactResponse{}
	if err := c.twirpCall("github.actions.results.api.v1.ArtifactService", "CreateArtifact", req, resp); err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("CreateArtifact: response from backend was not ok")
	}

	return resp, nil
}

func (c *GHAClient) UploadBlob(signedURL string, reader io.Reader, size int64, sha256Hash string) error {
	httpReq, err := http.NewRequest("PUT", signedURL, reader)
	if err != nil {
		return err
	}

	httpReq.ContentLength = size
	httpReq.Header.Set("Content-Type", "application/zip")
	httpReq.Header.Set("x-ms-blob-type", "BlockBlob")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("blob upload failed with status %d: %s", httpResp.StatusCode, string(body))
	}

	return nil
}

func (c *GHAClient) FinalizeArtifact(name string, size int64, hash string) error {
	req := finalizeArtifactRequest{
		WorkflowRunBackendID:    c.runBackendID,
		WorkflowJobRunBackendID: c.jobBackendID,
		Name: name,
		Size: size,
		Hash: hash,
	}

	resp := &finalizeArtifactResponse{}
	if err := c.twirpCall("github.actions.results.api.v1.ArtifactService", "FinalizeArtifact", req, resp); err != nil {
		return err
	}

	if !resp.Ok {
		return fmt.Errorf("FinalizeArtifact: response from backend was not ok")
	}

	return nil
}

func (c *GHAClient) twirpCall(service, method string, req, resp interface{}) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	twirpURL := fmt.Sprintf("%s/twirp/%s/%s", c.baseURL, service, method)

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
		return fmt.Errorf("twirp call %s/%s failed (HTTP %d): %s", service, method, httpResp.StatusCode, string(respBody))
	}

	if err := json.Unmarshal(respBody, resp); err != nil {
		return fmt.Errorf("failed to parse %s/%s response: %w (body: %s)", service, method, err, string(respBody))
	}

	return nil
}
