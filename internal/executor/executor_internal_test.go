package executor

import (
	"github.com/cirruslabs/cirrus-cli/internal/executor/build"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/container"
	"github.com/cirruslabs/cirrus-cli/internal/executor/options"
	"github.com/cirruslabs/cirrus-cli/pkg/api"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/anypb"
	"sort"
	"testing"
)

func TestDockerfileImageTemplate(t *testing.T) {
	anyInstance, err := anypb.New(&api.PrebuiltImageInstance{
		Repository: "cirrus-ci-community/d41d8cd98f00b204e9800998ecf8427e",
		Reference:  "latest",
	})
	if err != nil {
		t.Fatal(err)
	}

	tasks := []*api.Task{
		{
			Name:     "TestDockerfileImageTemplate",
			Instance: anyInstance,
		},
	}

	containerOpts := options.ContainerOptions{
		DockerfileImageTemplate: "gcr.io/cirrus-ci-community/%s:latest",
	}

	e, err := New(".", tasks, WithContainerOptions(containerOpts))
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "gcr.io/cirrus-ci-community/d41d8cd98f00b204e9800998ecf8427e:latest",
		e.build.GetTask(0).Instance.(*instance.PrebuiltInstance).Image)
}

func TestExtractImageHash(t *testing.T) {
	tests := []struct {
		image string
		want  string
	}{
		{"ghcr.io/owner/abc123:latest", "abc123"},
		{"ghcr.io/owner/def456:latest", "def456"},
		{"gcr.io/cirrus-ci-community/hash123:latest", "hash123"},
		{"gcr.io/cirrus-ci-community/hash-extra:latest", "hash-extra"},
		{"registry.io/org/repo/tag:latest", "repo/tag"},
		{"not-enough-parts", ""},
		{"", ""},
		{"nocolon:tag", ""},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			got := extractImageHash(tt.image)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSanitizeImageTagName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"windows_x64", "windows_x64"},
		{"WINDOWS_X64", "windows_x64"},
		{"linux-test", "linux-test"},
		{"test with spaces", "test-with-spaces"},
		{"special/chars!", "special-chars"},
		{"UPPERCase123", "uppercase123"},
		{"----trim-dashes----", "trim-dashes"},
		{"", "task"},
		{"...dots...", "dots"},
		{".leading", "leading"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeImageTagName(tt.name)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildExtraTags(t *testing.T) {
	e := &Executor{
		containerOptions: options.ContainerOptions{
			GitHubActionsMode:    true,
			GHCRRegistry:         "ghcr.io",
			DockerfileImageOwner: "myorg",
		},
	}

	prebuilt := &build.Task{
		ID:   10,
		Name: "Prebuild .ci/windows/Dockerfile",
		Instance: &instance.PrebuiltInstance{
			Image: "ghcr.io/myorg/abc123:latest",
		},
	}

	dependent1 := &build.Task{
		ID:          20,
		Name:        "windows_x64",
		RequiredIDs: []int64{10},
		Instance:    &container.Instance{Image: "ghcr.io/myorg/abc123:latest"},
	}

	dependent2 := &build.Task{
		ID:          30,
		Name:        "linux_test",
		RequiredIDs: []int64{10},
		Instance:    &container.Instance{Image: "ghcr.io/myorg/abc123:latest"},
	}

	unrelated := &build.Task{
		ID:          40,
		Name:        "other_task",
		RequiredIDs: []int64{99},
		Instance:    &container.Instance{Image: "debian:latest"},
	}

	allTasks := []*build.Task{prebuilt, dependent1, dependent2, unrelated}

	got := e.buildExtraTags(prebuilt, allTasks)
	sort.Strings(got)

	want := []string{
		"ghcr.io/myorg/abc123-linux_test:latest",
		"ghcr.io/myorg/abc123-windows_x64:latest",
		"ghcr.io/myorg/linux_test:latest",
		"ghcr.io/myorg/windows_x64:latest",
	}
	sort.Strings(want)

	assert.Equal(t, want, got)
}

func TestBuildExtraTagsNoDependents(t *testing.T) {
	e := &Executor{
		containerOptions: options.ContainerOptions{
			GitHubActionsMode:    true,
			GHCRRegistry:         "ghcr.io",
			DockerfileImageOwner: "myorg",
		},
	}

	prebuilt := &build.Task{
		ID:   10,
		Name: "Prebuild .ci/Dockerfile",
		Instance: &instance.PrebuiltInstance{
			Image: "ghcr.io/myorg/abc123:latest",
		},
	}

	allTasks := []*build.Task{prebuilt}

	got := e.buildExtraTags(prebuilt, allTasks)
	assert.Empty(t, got)
}

func TestBuildExtraTagsDuplicateDependents(t *testing.T) {
	e := &Executor{
		containerOptions: options.ContainerOptions{
			GitHubActionsMode:    true,
			GHCRRegistry:         "ghcr.io",
			DockerfileImageOwner: "myorg",
		},
	}

	prebuilt := &build.Task{
		ID:   10,
		Name: "Prebuild .ci/Dockerfile",
		Instance: &instance.PrebuiltInstance{
			Image: "ghcr.io/myorg/abc123:latest",
		},
	}

	// Two dependents with the same name (unlikely but possible via YAML aliases)
	dep1 := &build.Task{
		ID:          20,
		Name:        "test_task",
		RequiredIDs: []int64{10},
		Instance:    &container.Instance{Image: "ghcr.io/myorg/abc123:latest"},
	}
	dep2 := &build.Task{
		ID:          30,
		Name:        "test_task",
		RequiredIDs: []int64{10},
		Instance:    &container.Instance{Image: "ghcr.io/myorg/abc123:latest"},
	}

	allTasks := []*build.Task{prebuilt, dep1, dep2}

	got := e.buildExtraTags(prebuilt, allTasks)
	sort.Strings(got)

	// Duplicate-named dependents should only produce one set of tags
	want := []string{
		"ghcr.io/myorg/abc123-test_task:latest",
		"ghcr.io/myorg/test_task:latest",
	}
	sort.Strings(want)

	assert.Equal(t, want, got)
}
