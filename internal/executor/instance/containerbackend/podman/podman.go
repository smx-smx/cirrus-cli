package podman

import (
	"encoding/base64"
	"encoding/json"
	"github.com/containers/image/v5/pkg/docker/config"
	"github.com/containers/image/v5/types"
)

func XRegistryAuthForImage(reference string) (string, error) {
	authConfig, err := config.GetCredentials(&types.SystemContext{}, reference)
	if err != nil {
		return "", err
	}

	authConfigJSON, err := json.Marshal(&authConfig)
	if err != nil {
		return "", err
	}

	// Docker-compatible registries expect standard base64 (not URL-safe).
	return base64.StdEncoding.EncodeToString(authConfigJSON), nil
}
