package instance

import (
	"strings"
	"testing"

	"github.com/cirruslabs/cirrus-cli/internal/executor/options"
	"github.com/stretchr/testify/assert"
)

func TestAddOCILabelsFull(t *testing.T) {
	labels := make(map[string]string)
	opts := options.ContainerOptions{
		GitHubServerURL:         "https://github.com",
		DockerfileImageOwner:    "myorg",
		DockerfileImageRepo:     "myrepo",
		GitHubSHA:              "abc123def456",
	}

	addOCILabels(labels, "ghcr.io/myorg/abc123:latest", opts)

	assert.Equal(t, "ghcr.io/myorg/abc123:latest", labels["org.opencontainers.image.ref.name"])
	assert.Equal(t, "https://github.com/myorg/myrepo", labels["org.opencontainers.image.source"])
	assert.Equal(t, "abc123def456", labels["org.opencontainers.image.revision"])
	assert.NotEmpty(t, labels["org.opencontainers.image.created"])

	assert.Len(t, labels, 4)
}

func TestAddOCILabelsMinimal(t *testing.T) {
	labels := make(map[string]string)
	opts := options.ContainerOptions{}

	addOCILabels(labels, "some-image:latest", opts)

	assert.Equal(t, "some-image:latest", labels["org.opencontainers.image.ref.name"])
	assert.Empty(t, labels["org.opencontainers.image.source"])
	assert.Empty(t, labels["org.opencontainers.image.revision"])
	assert.NotEmpty(t, labels["org.opencontainers.image.created"])

	assert.Len(t, labels, 2)
}

func TestAddOCILabelsNoServerURL(t *testing.T) {
	labels := make(map[string]string)
	opts := options.ContainerOptions{
		DockerfileImageOwner: "myorg",
		DockerfileImageRepo:  "myrepo",
		GitHubSHA:           "sha1",
	}

	addOCILabels(labels, "img:latest", opts)

	assert.Equal(t, "sha1", labels["org.opencontainers.image.revision"])
	assert.Empty(t, labels["org.opencontainers.image.source"])
}

func TestAddOCILabelsNoRepo(t *testing.T) {
	labels := make(map[string]string)
	opts := options.ContainerOptions{
		GitHubServerURL:      "https://github.com",
		DockerfileImageOwner: "myorg",
		GitHubSHA:           "sha1",
	}

	addOCILabels(labels, "img:latest", opts)

	assert.Equal(t, "sha1", labels["org.opencontainers.image.revision"])
	assert.Empty(t, labels["org.opencontainers.image.source"])
}

func TestAddOCILabelsNoSHA(t *testing.T) {
	labels := make(map[string]string)
	opts := options.ContainerOptions{
		GitHubServerURL:      "https://github.com",
		DockerfileImageOwner: "myorg",
		DockerfileImageRepo:  "myrepo",
	}

	addOCILabels(labels, "img:latest", opts)

	assert.Equal(t, "https://github.com/myorg/myrepo", labels["org.opencontainers.image.source"])
	assert.Empty(t, labels["org.opencontainers.image.revision"])
}

func TestAddOCILabelsEnterpriseURL(t *testing.T) {
	labels := make(map[string]string)
	opts := options.ContainerOptions{
		GitHubServerURL:      "https://github.mycompany.com",
		DockerfileImageOwner: "myorg",
		DockerfileImageRepo:  "myrepo",
		GitHubSHA:           "def789",
	}

	addOCILabels(labels, "ghcr.io/myorg/hash:latest", opts)

	assert.Equal(t, "https://github.mycompany.com/myorg/myrepo", labels["org.opencontainers.image.source"])
}

func TestAddOCILabelsCreatedFormat(t *testing.T) {
	labels := make(map[string]string)
	addOCILabels(labels, "img:latest", options.ContainerOptions{})

	created := labels["org.opencontainers.image.created"]
	assert.NotEmpty(t, created)
	// RFC 3339 format: 2006-01-02T15:04:05Z07:00
	assert.True(t, strings.Contains(created, "T"), "should contain 'T' time separator")
	assert.True(t, strings.Contains(created, ":"), "should contain ':' time components")
}

func TestAddOCILabelsIdempotent(t *testing.T) {
	labels := map[string]string{
		"pre-existing": "value",
	}
	opts := options.ContainerOptions{
		GitHubServerURL:      "https://github.com",
		DockerfileImageOwner: "myorg",
		DockerfileImageRepo:  "myrepo",
		GitHubSHA:           "sha",
	}

	addOCILabels(labels, "img:latest", opts)

	assert.Equal(t, "value", labels["pre-existing"])
	assert.Equal(t, "sha", labels["org.opencontainers.image.revision"])
}

func TestAddOCILabelsOnlyOwnerNoRepo(t *testing.T) {
	labels := make(map[string]string)
	opts := options.ContainerOptions{
		GitHubServerURL:      "https://github.com",
		DockerfileImageOwner: "myorg",
		GitHubSHA:           "sha1",
	}

	addOCILabels(labels, "img:latest", opts)

	assert.Empty(t, labels["org.opencontainers.image.source"])
	assert.Equal(t, "sha1", labels["org.opencontainers.image.revision"])
}
