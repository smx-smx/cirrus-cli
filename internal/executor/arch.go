package executor

// Architecture-aware task routing.
//
// On Cirrus Cloud, tasks land on hosts matching their pool: container tasks
// on x64, arm_container tasks on Graviton-class ARM. On GitHub Actions the
// CLI runs every task on one runner (plain `cirrus run` has no filter), so
// it mirrors the scheduler instead: tasks whose architecture the host cannot
// satisfy are Skipped (green) rather than failed.
//
// This keeps dual-arch .cirrus.yml files working with a bare `cirrus run`
// on any single runner, and makes a matrix over ubuntu-24.04 /
// ubuntu-24.04-arm do the right thing on each leg automatically.

import (
	"os"
	"runtime"

	"github.com/cirruslabs/cirrus-cli/internal/executor/build"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/container"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/freebsd"
	"github.com/cirruslabs/cirrus-cli/pkg/api"
)

// taskArch returns the CPU architecture a task requires, if it constrains one.
func taskArch(task *build.Task, allTasks []*build.Task) (api.Architecture, bool) {
	switch inst := task.Instance.(type) {
	case *container.Instance:
		// arm_container tasks carry ARM64; plain container: tasks default to
		// the x64 pool (a multi-arch image manifest does not change pools).
		if inst.Architecture != nil {
			return *inst.Architecture, true
		}
		return api.Architecture_AMD64, true
	case *freebsd.Instance:
		// The official BASIC-CI images we boot are amd64-only.
		return api.Architecture_AMD64, true
	case *instance.PrebuiltInstance:
		// Dockerfile builder tasks carry no arch themselves; inherit the
		// arch of the tasks depending on them. Mixed or unknown dependents
		// (e.g. a shared Dockerfile across arches) run and let the push
		// tags decide — see buildExtraTags.
		var arch api.Architecture
		found := false
		for _, other := range allTasks {
			if !requiresTask(other, task.ID) {
				continue
			}
			dependentArch, ok := containerArch(other)
			if !ok {
				return api.Architecture_AMD64, false
			}
			if !found {
				arch, found = dependentArch, true
			} else if dependentArch != arch {
				return api.Architecture_AMD64, false
			}
		}
		if !found {
			return api.Architecture_AMD64, false
		}
		return arch, true
	default:
		return api.Architecture_AMD64, false
	}
}

// containerArch reports the pool arch of a container task, if it is one.
func containerArch(task *build.Task) (api.Architecture, bool) {
	if inst, ok := task.Instance.(*container.Instance); ok {
		if inst.Architecture != nil {
			return *inst.Architecture, true
		}
		return api.Architecture_AMD64, true
	}
	return api.Architecture_AMD64, false
}

func requiresTask(task *build.Task, id int64) bool {
	for _, requiredID := range task.RequiredIDs {
		if requiredID == id {
			return true
		}
	}
	return false
}

// hostSupportsArch reports whether the current host can run an arch:
// natively, or emulated via binfmt_misc (e.g. qemu-aarch64 registered by
// tonistiigi/binfmt on Linux, which Docker then uses transparently).
func hostSupportsArch(arch api.Architecture) bool {
	var goarch string
	switch arch {
	case api.Architecture_ARM64:
		goarch = "arm64"
	default:
		goarch = "amd64"
	}

	if runtime.GOARCH == goarch {
		return true
	}

	if runtime.GOOS == "linux" {
		binfmt := map[string]string{
			"arm64": "qemu-aarch64",
			"amd64": "qemu-x86_64",
		}[goarch]

		if _, err := os.Stat("/proc/sys/fs/binfmt_misc/" + binfmt); err == nil {
			return true
		}
	}

	return false
}
