package instance

import (
	"archive/tar"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/containerbackend"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/runconfig"
	"github.com/cirruslabs/cirrus-cli/internal/executor/options"
	"go.opentelemetry.io/otel/attribute"
)

type PrebuiltInstance struct {
	Image      string
	Dockerfile string
	Arguments  map[string]string
	ExtraTags  []string

	containerBackend containerbackend.ContainerBackend
}

func CreateTempArchive(dir string) (string, error) {
	tmpFile, err := os.CreateTemp("", "cirrus-prebuilt-archive-")
	if err != nil {
		return "", err
	}

	archive := tar.NewWriter(tmpFile)

	if err := filepath.Walk(dir, func(path string, fileInfo os.FileInfo, err error) error {
		// Handle possible error that occurred when reading this directory entry information
		if err != nil {
			return err
		}

		// We clearly don't want any directories here (because tar)
		// and probably not interested in special files for now
		if !fileInfo.Mode().IsRegular() {
			return nil
		}

		header, err := tar.FileInfoHeader(fileInfo, fileInfo.Name())
		if err != nil {
			return err
		}

		// Since os.FileInfo doesn't contain the full path to a file
		// we need to manually update the Name field in the header
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		header.Name = relPath

		// Write file header
		if err := archive.WriteHeader(header); err != nil {
			return err
		}

		// Write file contents
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		if _, err := io.Copy(archive, file); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return "", err
	}

	if err := archive.Close(); err != nil {
		return "", err
	}

	if err := tmpFile.Close(); err != nil {
		return "", err
	}

	return tmpFile.Name(), nil
}

func (prebuilt *PrebuiltInstance) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("image", prebuilt.Image),
		attribute.String("instance_type", "prebuilt"),
	}
}

func (prebuilt *PrebuiltInstance) Run(ctx context.Context, config *runconfig.RunConfig) error {
	logger := config.Logger()
	backend, err := config.GetContainerBackend()
	if err != nil {
		return err
	}
	prebuilt.containerBackend = backend

	containerOpts := config.ContainerOptions

	// In GitHub Actions mode, login to ghcr.io first to enable remote cache check
	if containerOpts.GitHubActionsMode && containerOpts.GitHubToken != "" {
		logger.Infof("Logging into %s...", containerOpts.GHCRRegistry)
		if err := backend.RegistryLogin(ctx, containerOpts.GHCRRegistry, containerOpts.GHCRUsername, containerOpts.GitHubToken); err != nil {
			return fmt.Errorf("failed to login to %s: %w", containerOpts.GHCRRegistry, err)
		}
	}

	// Check if the image we're about to build is available locally
	if err = backend.ImageInspect(ctx, prebuilt.Image); err == nil {
		logger.Infof("Re-using local image %s...", prebuilt.Image)
		return nil
	}

	// The image is not available locally, try to pull it
	logger.Infof("Pulling image %s...", prebuilt.Image)
	if err := backend.ImagePull(ctx, prebuilt.Image, nil); err == nil {
		logger.Infof("Using pulled image %s...", prebuilt.Image)
		return nil
	}

	logger.Infof("Image %s is not available locally nor remotely, building it...", prebuilt.Image)

	// Create an archive with the build context
	archivePath, err := CreateTempArchive(config.ProjectDir)
	if err != nil {
		return err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() {
		// Don't bother with catching the error since the file may be already closed by a container backend
		_ = file.Close()

		if err := os.Remove(archivePath); err != nil {
			logger.Warnf("while removing temporary archive file: %v", err)
		}
	}()

	// Build the image
	buildLabels := make(map[string]string)
	if containerOpts.GitHubActionsMode {
		addOCILabels(buildLabels, prebuilt.Image, config.ContainerOptions)
	}
	logChan, errChan := backend.ImageBuild(ctx, file, &containerbackend.ImageBuildInput{
		Tags:       []string{prebuilt.Image},
		Dockerfile: prebuilt.Dockerfile,
		BuildArgs:  prebuilt.Arguments,
		Pull:       !config.ContainerOptions.LazyPull,
		ExtraTags:  prebuilt.ExtraTags,
		Labels:     buildLabels,
	})

Outer:
	for {
		select {
		case line := <-logChan:
			logger.Infof("%s", line)
		case err := <-errChan:
			if errors.Is(err, containerbackend.ErrDone) {
				break Outer
			}

			return err
		}
	}

	// Push the image (if needed)
	if config.ContainerOptions.DockerfileImagePush {
		var auth string
		if config.ContainerOptions.GitHubActionsMode && config.ContainerOptions.GitHubToken != "" {
			auth = constructAuth(config.ContainerOptions.GHCRUsername, config.ContainerOptions.GitHubToken)
		}

		allTags := append([]string{prebuilt.Image}, prebuilt.ExtraTags...)
		for _, tag := range allTags {
			if err := backend.ImagePush(ctx, tag, auth); err != nil {
				if tag == prebuilt.Image {
					return err
				}
				logger.Warnf("failed to push extra tag %s: %v", tag, err)
			}
		}
	}

	return nil
}

func constructAuth(username, password string) string {
	authConfig := map[string]string{
		"username": username,
		"password": password,
	}
	authConfigJSON, _ := json.Marshal(authConfig)
	return base64.URLEncoding.EncodeToString(authConfigJSON)
}

func addOCILabels(labels map[string]string, image string, opts options.ContainerOptions) {
	labels["org.opencontainers.image.created"] = time.Now().UTC().Format(time.RFC3339)
	labels["org.opencontainers.image.ref.name"] = image
	if opts.GitHubServerURL != "" && opts.DockerfileImageRepo != "" {
		labels["org.opencontainers.image.source"] = fmt.Sprintf("%s/%s/%s",
			opts.GitHubServerURL, opts.DockerfileImageOwner, opts.DockerfileImageRepo)
	}
	if opts.GitHubSHA != "" {
		labels["org.opencontainers.image.revision"] = opts.GitHubSHA
	}
}

func (prebuilt *PrebuiltInstance) WorkingDirectory(projectDir string, dirtyMode bool) string {
	return ""
}

func (prebuilt *PrebuiltInstance) Close(context.Context) error {
	if prebuilt.containerBackend != nil {
		return prebuilt.containerBackend.Close()
	}

	return nil
}
