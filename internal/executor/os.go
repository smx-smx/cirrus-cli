package executor

// Operating-system-aware task routing, the sibling of arch.go.
//
// On Cirrus Cloud, tasks land on hosts matching their OS: windows_container
// tasks on Windows, macos_instance (Tart) tasks on macOS. With plain
// `cirrus run` everything executes on one runner, so the CLI mirrors the
// scheduler instead: tasks whose OS the host cannot satisfy are Skipped
// (green) rather than failed.
//
// Deliberately conservative: Linux containers stay allowed everywhere Docker
// exists (so local MacBook flows via Docker Desktop keep working), and only
// positively-constrained instances (Windows platform, Tart/macOS, Vetu,
// Parallels) are gated.

import (
	"runtime"

	"github.com/cirruslabs/cirrus-cli/internal/executor/build"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/container"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/persistentworker/isolation/parallels"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/persistentworker/isolation/tart"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/persistentworker/isolation/vetu"
	"github.com/cirruslabs/cirrus-cli/internal/executor/platform"
)

// taskOS returns the host OS (runtime.GOOS form) a task requires, if it
// constrains one.
func taskOS(task *build.Task, allTasks []*build.Task) (string, bool) {
	switch inst := task.Instance.(type) {
	case *container.Instance:
		// Only Windows containers positively need a Windows host.
		if _, ok := inst.Platform.(*platform.WindowsPlatform); ok {
			return "windows", true
		}
		return "", false
	case *tart.Tart:
		// Covers both tart isolations and macos_instance tasks, which the
		// instance factory maps to Tart.
		return "darwin", true
	case *vetu.Vetu:
		return "linux", true
	case *parallels.Parallels:
		return "darwin", true
	case *instance.PrebuiltInstance:
		// Dockerfile builders inherit the OS of their dependents, but only
		// Windows constrains: Linux Dockerfiles build anywhere Docker runs.
		// Mixed or unknown dependents run (same policy as arch.go).
		_ = inst
		seenWindows := false
		seenOther := false
		for _, other := range allTasks {
			if !requiresTask(other, task.ID) {
				continue
			}
			if os, ok := containerOS(other); ok {
				if os == "windows" {
					seenWindows = true
				} else {
					seenOther = true
				}
			} else {
				seenOther = true
			}
		}
		if seenWindows && !seenOther {
			return "windows", true
		}
		return "", false
	default:
		return "", false
	}
}

// containerOS reports the OS pool of a container task, if it is one.
func containerOS(task *build.Task) (string, bool) {
	if inst, ok := task.Instance.(*container.Instance); ok {
		if _, ok := inst.Platform.(*platform.WindowsPlatform); ok {
			return "windows", true
		}
		return "linux", true
	}
	return "", false
}

// hostSupportsOS reports whether the current host satisfies a required OS.
func hostSupportsOS(os string) bool {
	return runtime.GOOS == os
}
