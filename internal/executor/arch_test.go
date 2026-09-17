package executor

import (
	"runtime"
	"testing"

	"github.com/cirruslabs/cirrus-cli/internal/executor/build"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/container"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/freebsd"
	"github.com/cirruslabs/cirrus-cli/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func archPtr(arch api.Architecture) *api.Architecture {
	return &arch
}

func TestTaskArchContainer(t *testing.T) {
	arch, ok := taskArch(&build.Task{
		Instance: &container.Instance{Architecture: archPtr(api.Architecture_ARM64)},
	}, nil)
	require.True(t, ok)
	assert.Equal(t, api.Architecture_ARM64, arch)

	// Plain container: tasks default to the x64 pool.
	arch, ok = taskArch(&build.Task{
		Instance: &container.Instance{},
	}, nil)
	require.True(t, ok)
	assert.Equal(t, api.Architecture_AMD64, arch)
}

func TestTaskArchFreeBSD(t *testing.T) {
	inst, err := freebsd.New(freebsd.Config{}, nil)
	require.NoError(t, err)

	arch, ok := taskArch(&build.Task{Instance: inst}, nil)
	require.True(t, ok)
	assert.Equal(t, api.Architecture_AMD64, arch)
}

func TestTaskArchPrebuilt(t *testing.T) {
	builder := &build.Task{ID: 1, Instance: &instance.PrebuiltInstance{}}

	// No dependents: run.
	_, ok := taskArch(builder, []*build.Task{builder})
	assert.False(t, ok)

	// Single-arch dependents: inherit.
	armDependent := &build.Task{
		ID:          2,
		RequiredIDs: []int64{1},
		Instance:    &container.Instance{Architecture: archPtr(api.Architecture_ARM64)},
	}
	arch, ok := taskArch(builder, []*build.Task{builder, armDependent})
	require.True(t, ok)
	assert.Equal(t, api.Architecture_ARM64, arch)

	// Mixed dependents: run and let tags decide.
	amdDependent := &build.Task{
		ID:          3,
		RequiredIDs: []int64{1},
		Instance:    &container.Instance{Architecture: archPtr(api.Architecture_AMD64)},
	}
	_, ok = taskArch(builder, []*build.Task{builder, armDependent, amdDependent})
	assert.False(t, ok)
}

func TestHostSupportsNativeArch(t *testing.T) {
	var native api.Architecture
	if runtime.GOARCH == "arm64" {
		native = api.Architecture_ARM64
	} else {
		native = api.Architecture_AMD64
	}
	assert.True(t, hostSupportsArch(native))
}
