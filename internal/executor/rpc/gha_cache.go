package rpc

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/cirruslabs/cirrus-cli/pkg/api"
	"github.com/cirruslabs/cirrus-cli/pkg/github"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const ghaCacheVersion = "1"

func (r *RPC) ghaCacheEnabled() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true" && os.Getenv("ACTIONS_RESULTS_URL") != ""
}

func (r *RPC) ghaCacheHTTPBase() string {
	httpPort := r.ghaHTTPListener.Addr().(*net.TCPAddr).Port
	grpcEndpoint := r.listener.DirectEndpoint()
	host := strings.TrimRight(grpcEndpoint, "/")
	return fmt.Sprintf("http://%s:%d", extractHost(host), httpPort)
}

func extractHost(endpoint string) string {
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "unix:")
	if colon := strings.LastIndex(endpoint, ":"); colon >= 0 {
		return endpoint[:colon]
	}
	return endpoint
}

func (r *RPC) handleGHACacheDownload(w http.ResponseWriter, req *http.Request) {
	key := strings.TrimPrefix(req.URL.Path, "/cirrus-gha-cache/download/")
	if key == "" {
		http.Error(w, "missing cache key", http.StatusBadRequest)
		return
	}

	var restoreKeys []string
	if rk := req.URL.Query().Get("restore_keys"); rk != "" {
		restoreKeys = strings.Split(rk, ",")
	}

	client, err := github.NewGHACacheClient()
	if err != nil {
		r.logger.Warnf("failed to create GHA cache client for download: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	downloadResp, err := client.GetCacheEntryDownloadURL(key, ghaCacheVersion, restoreKeys)
	if err != nil {
		r.logger.Warnf("GHA cache download lookup failed for key %s: %v", key, err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	r.logger.Debugf("GHA cache download: key=%s matched_key=%s url=%s", key, downloadResp.MatchedKey, downloadResp.SignedDownloadURL)

	downloadReq, err := http.NewRequestWithContext(req.Context(), http.MethodGet, downloadResp.SignedDownloadURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	downloadHTTPResp, err := http.DefaultClient.Do(downloadReq)
	if err != nil {
		r.logger.Warnf("GHA cache download failed for key %s: %v", key, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer downloadHTTPResp.Body.Close()

	if downloadHTTPResp.StatusCode < 200 || downloadHTTPResp.StatusCode >= 300 {
		body, _ := io.ReadAll(downloadHTTPResp.Body)
		r.logger.Warnf("GHA cache download returned status %d for key %s: %s", downloadHTTPResp.StatusCode, key, string(body))
		http.Error(w, "cache download failed", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	io.Copy(w, downloadHTTPResp.Body)
}

func (r *RPC) handleGHACacheUpload(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := strings.TrimPrefix(req.URL.Path, "/cirrus-gha-cache/upload/")
	if key == "" {
		http.Error(w, "missing cache key", http.StatusBadRequest)
		return
	}

	client, err := github.NewGHACacheClient()
	if err != nil {
		r.logger.Warnf("failed to create GHA cache client for upload: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	createResp, err := client.CreateCacheEntry(key, ghaCacheVersion)
	if err != nil {
		r.logger.Warnf("GHA cache CreateCacheEntry failed for key %s: %v", key, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	r.logger.Debugf("GHA cache upload: key=%s upload_url=%s", key, createResp.SignedUploadURL)

	uploadReq, err := http.NewRequestWithContext(req.Context(), http.MethodPut, createResp.SignedUploadURL, req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	uploadReq.ContentLength = req.ContentLength
	uploadReq.Header.Set("Content-Type", "application/octet-stream")
	uploadReq.Header.Set("x-ms-blob-type", "BlockBlob")

	uploadResp, err := http.DefaultClient.Do(uploadReq)
	if err != nil {
		r.logger.Warnf("GHA cache blob upload failed for key %s: %v", key, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode < 200 || uploadResp.StatusCode >= 300 {
		body, _ := io.ReadAll(uploadResp.Body)
		r.logger.Warnf("GHA cache blob upload returned status %d for key %s: %s", uploadResp.StatusCode, key, string(body))
		http.Error(w, fmt.Sprintf("cache blob upload failed: %d", uploadResp.StatusCode), http.StatusBadGateway)
		return
	}

	if err := client.FinalizeCacheEntryUpload(key, ghaCacheVersion, req.ContentLength); err != nil {
		r.logger.Warnf("GHA cache FinalizeCacheEntryUpload failed for key %s: %v", key, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	r.logger.Debugf("GHA cache upload completed for key %s (size=%d)", key, req.ContentLength)
	w.WriteHeader(http.StatusOK)
}

func (r *RPC) handleGHACacheInfo(w http.ResponseWriter, req *http.Request) {
	key := strings.TrimPrefix(req.URL.Path, "/cirrus-gha-cache/info/")
	if key == "" {
		http.Error(w, "missing cache key", http.StatusBadRequest)
		return
	}

	var restoreKeys []string
	if rk := req.URL.Query().Get("restore_keys"); rk != "" {
		restoreKeys = strings.Split(rk, ",")
	}

	client, err := github.NewGHACacheClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	downloadResp, err := client.GetCacheEntryDownloadURL(key, ghaCacheVersion, restoreKeys)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("X-Matched-Key", downloadResp.MatchedKey)
	w.WriteHeader(http.StatusOK)
}

func (r *RPC) ghaCacheInfo(ctx context.Context, req *api.CacheInfoRequest) (*api.CacheInfoResponse, error) {
	client, err := github.NewGHACacheClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create GHA cache client: %v", err)
	}

	downloadResp, err := client.GetCacheEntryDownloadURL(req.CacheKey, ghaCacheVersion, req.CacheKeyPrefixes)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "cache entry not found")
	}

	r.logger.Debugf("GHA CacheInfo: key=%s matched_key=%s", req.CacheKey, downloadResp.MatchedKey)

	return &api.CacheInfoResponse{
		Info: &api.CacheInfo{
			Key: downloadResp.MatchedKey,
		},
	}, nil
}
