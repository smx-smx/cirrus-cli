package executor

import (
	"runtime"
	"testing"

	"github.com/cirruslabs/cirrus-cli/internal/executor/build"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/container"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/persistentworker/isolation/tart"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/persistentworker/isolation/vetu"
	"github.com/cirruslabs/cirrus-cli/internal/executor/platform"
	"github.com/cirruslabs/cirrus-cli/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskOSWindowsContainer(t *testing.T) {
	os, ok := taskOS(&build.Task{
		Instance: &container.Instance{Platform: platform.NewWindows("2019")},
	}, nil)
	require.True(t, ok)
	assert.Equal(t, "windows", os)
}

func TestTaskOSLinuxContainerUnconstrained(t *testing.T) {
	// Linux containers run anywhere Docker exists (e.g. Docker Desktop on macOS).
	_, ok := taskOS(&build.Task{
		Instance: &container.Instance{
			Platform:     platform.NewUnix(),
			Architecture: archPtr(api.Architecture_AMD64),
		},
	}, nil)
	assert.False(t, ok)
}

func TestTaskOSTart(t *testing.T) {
	tartInstance, err := tart.New("image", "user", "pass", 22, 4, 4096)
	require.NoError(t, err)

	os, ok := taskOS(&build.Task{Instance: tartInstance}, nil)
	require.True(t, ok)
	assert.Equal(t, "darwin", os)
}

func TestTaskOSVetu(t *testing.T) {
	vetuInstance, err := vetu.New("image", "user", "pass", 22, 4, 4096, nil, nil)
	require.NoError(t, err)

	os, ok := taskOS(&build.Task{Instance: vetuInstance}, nil)
	require.True(t, ok)
	assert.Equal(t, "linux", os)
}

func TestTaskOSPrebuilt(t *testing.T) {
	builder := &build.Task{ID: 1, Instance: &instance.PrebuiltInstance{}}

	// Windows-only dependents: pin to Windows.
	windowsDependent := &build.Task{
		ID:          2,
		RequiredIDs: []int64{1},
		Instance:    &container.Instance{Platform: platform.NewWindows("2019")},
	}
	os, ok := taskOS(builder, []*build.Task{builder, windowsDependent})
	require.True(t, ok)
	assert.Equal(t, "windows", os)

	// Linux dependents: unconstrained (Linux Dockerfiles build anywhere).
	linuxDependent := &build.Task{
		ID:          3,
		RequiredIDs: []int64{1},
		Instance:    &container.Instance{Platform: platform.NewUnix()},
	}
	_, ok = taskOS(builder, []*build.Task{builder, linuxDependent})
	assert.False(t, ok)

	// No dependents: run.
	_, ok = taskOS(builder, []*build.Task{builder})
	assert.False(t, ok)
}

func TestHostSupportsOS(t *testing.T) {
	assert.True(t, hostSupportsOS(runtime.GOOS))
	assert.False(t, hostSupportsOS("plan9"))
}
