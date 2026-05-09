package options

import (
	"context"
	"errors"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/containerbackend"
)

type ContainerOptions struct {
	LazyPull       bool
	NoPullImages   []string
	NoCleanup      bool
	IgnoreGitignore bool

	DockerfileImageTemplate string
	DockerfileImagePush     bool

	GitHubActionsMode bool
	GHCRRegistry      string
	GHCRUsername      string
	GitHubToken       string
}

func (copts ContainerOptions) ShouldPullImage(
	ctx context.Context,
	backend containerbackend.ContainerBackend,
	image string,
) bool {
	for _, noPullImage := range copts.NoPullImages {
		if noPullImage == image {
			return false
		}
	}

	if !copts.LazyPull {
		return true
	}

	return errors.Is(backend.ImageInspect(ctx, image), containerbackend.ErrNotFound)
}
