package freebsd

// FreeBSD tasks run in a VM booted from the official FreeBSD BASIC-CI
// images under QEMU.
//
// This is what makes freebsd_instance work outside of Cirrus Cloud (and in
// particular on GitHub Actions, which has no FreeBSD runners): the CLI
// itself acts as the hypervisor, so no workflow changes are needed and the
// task's cache/artifact instructions keep working through the usual agent
// protocol (the gRPC endpoint reaches the guest over an SSH-forwarded port
// set up by remoteagent, exactly like with Tart and Vetu VMs).

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/abstract"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/persistentworker/projectdirsyncer"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/persistentworker/remoteagent"
	"github.com/cirruslabs/cirrus-cli/internal/executor/instance/runconfig"
	"github.com/cirruslabs/cirrus-cli/internal/executor/platform"
	"github.com/cirruslabs/cirrus-cli/internal/logger"
	"github.com/cirruslabs/cirrus-cli/pkg/parser/instance"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/crypto/ssh"
)

var ErrFailed = errors.New("freebsd instance failed")

var _ abstract.Instance = (*Instance)(nil)

// SSH credentials to try, in order.
//
// BASIC-CI images boot with sshd and DHCP enabled and permit root logins
// with an empty password; release VM images additionally carry the
// documented freebsd/freebsd and root/root accounts.
var sshCredentials = []struct {
	user     string
	password string
}{
	{"root", ""},
	{"freebsd", "freebsd"},
	{"root", "root"},
}

const (
	defaultCPU    = 2
	defaultMemory = 4096

	bootTimeout = 15 * time.Minute
)

type Config struct {
	ImageFamily string
	ImageName   string
	CPU         int
	Memory      uint32
}

// ConfigFromEnvironment picks up a freebsd_instance definition recorded by
// the parser (see pkg/parser/instance/freebsd.go). The second return value
// reports whether this task is a FreeBSD task at all.
func ConfigFromEnvironment(env map[string]string) (Config, bool) {
	family := env[instance.EnvFreeBSDImageFamily]
	name := env[instance.EnvFreeBSDImageName]

	if family == "" && name == "" {
		return Config{}, false
	}

	config := Config{
		ImageFamily: family,
		ImageName:   name,
		CPU:         defaultCPU,
		Memory:      defaultMemory,
	}

	if cpuRaw := env[instance.EnvFreeBSDCPU]; cpuRaw != "" {
		if cpuFloat, err := strconv.ParseFloat(cpuRaw, 32); err == nil && cpuFloat >= 1 {
			config.CPU = int(cpuFloat)
		}
	}

	if memoryRaw := env[instance.EnvFreeBSDMemory]; memoryRaw != "" {
		if memoryParsed, err := strconv.ParseUint(memoryRaw, 10, 32); err == nil && memoryParsed > 0 {
			config.Memory = uint32(memoryParsed)
		}
	}

	return config, true
}

type Instance struct {
	config Config
	logger logger.Lightweight

	workDir   string
	qemuCmd   *exec.Cmd
	qemuWait  chan error
	sshPort   int
	sshUser   string
	sshPass   string
	serialLog string
	stderrLog string
}

func New(config Config, log logger.Lightweight) (*Instance, error) {
	if log == nil {
		log = &logger.LightweightStub{}
	}

	return &Instance{
		config: config,
		logger: log,
	}, nil
}

func (inst *Instance) Attributes() []attribute.KeyValue {
	image := inst.config.ImageName
	if image == "" {
		image = inst.config.ImageFamily
	}

	return []attribute.KeyValue{
		attribute.String("image", image),
		attribute.String("instance_type", "freebsd"),
	}
}

func (inst *Instance) WorkingDirectory(projectDir string, dirtyMode bool) string {
	return platform.NewUnix().GenericWorkingDir()
}

func (inst *Instance) Run(ctx context.Context, config *runconfig.RunConfig) error {
	if config.DirtyMode {
		return fmt.Errorf("%w: dirty mode is not supported for FreeBSD tasks "+
			"(the project directory cannot be mounted into the QEMU guest)", ErrFailed)
	}

	if _, err := exec.LookPath("qemu-system-x86_64"); err != nil {
		return fmt.Errorf("%w: qemu-system-x86_64 not found in PATH "+
			"(e.g. sudo apt-get install -y qemu-system-x86): %v", ErrFailed, err)
	}

	rawImage, err := ensureImage(ctx, config.Logger(), inst.config.ImageFamily, inst.config.ImageName)
	if err != nil {
		return err
	}

	workDir, err := os.MkdirTemp("", "cirrus-freebsd-")
	if err != nil {
		return err
	}
	inst.workDir = workDir
	inst.serialLog = filepath.Join(workDir, "serial.log")
	inst.stderrLog = filepath.Join(workDir, "qemu-stderr.log")

	sshPort, err := freePort()
	if err != nil {
		return err
	}
	inst.sshPort = sshPort

	config.Logger().Infof("booting FreeBSD (%s, %s, %d CPU, %d MiB) under QEMU %s...",
		inst.imageDescription(), accelDescription(), inst.config.CPU, inst.config.Memory, qemuVersion())

	if err := inst.boot(ctx, rawImage); err != nil {
		return err
	}

	addr := fmt.Sprintf("127.0.0.1:%d", sshPort)

	sshUser, sshPass, err := inst.waitForSSH(ctx, addr, config)
	if err != nil {
		return err
	}
	inst.sshUser, inst.sshPass = sshUser, sshPass

	agentEnv := map[string]string{
		"CIRRUS_VM_ID": fmt.Sprintf("qemu-freebsd-%s", config.TaskID),
	}
	for key, value := range config.AdditionalEnvironment {
		agentEnv[key] = value
	}

	initializeHooks := remoteagent.WaitForAgentHooks{}
	if config.ProjectDir != "" {
		initializeHooks = append(initializeHooks, func(ctx context.Context, sshClient *ssh.Client) error {
			syncLogger := config.Logger().Scoped("syncing working directory")
			if err := projectdirsyncer.SyncProjectDir(config.ProjectDir, sshClient); err != nil {
				syncLogger.Finish(false)
				return fmt.Errorf("failed to sync project directory: %v", err)
			}

			syncLogger.Finish(true)
			return nil
		})
	}

	inst.logger.Debugf("FreeBSD VM is up (ssh %s@127.0.0.1:%d), running agent...", sshUser, sshPort)

	return remoteagent.WaitForAgent(ctx, inst.logger, addr, sshUser, sshPass,
		"freebsd", "amd64", config, true, initializeHooks, nil, "", agentEnv,
		config.LocalNetworkHelper)
}

func (inst *Instance) Close(ctx context.Context) error {
	if inst.qemuCmd != nil && inst.qemuCmd.Process != nil {
		// The waiter goroutine reaps the process; just signal it.
		_ = inst.qemuCmd.Process.Kill()
		inst.qemuCmd = nil
	}

	if inst.workDir != "" {
		_ = os.RemoveAll(inst.workDir)
		inst.workDir = ""
	}

	return nil
}

// boot starts QEMU with user-mode networking (host-forwarded SSH) and a
// throwaway snapshot overlay, so the cached image is never modified.
func (inst *Instance) boot(ctx context.Context, rawImage string) error {
	args := []string{
		"-m", strconv.FormatUint(uint64(inst.config.Memory), 10),
		"-smp", strconv.Itoa(inst.config.CPU),
		"-boot", "order=c",
		"-drive", fmt.Sprintf("file=%s,format=raw,if=virtio", rawImage),
		"-snapshot",
		"-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp:127.0.0.1:%d-:22", inst.sshPort),
		"-device", "virtio-net-pci,netdev=net0",
		"-display", "none",
		"-serial", fmt.Sprintf("file:%s", inst.serialLog),
	}

	if kvmAvailable() {
		args = append(args, "-accel", "kvm", "-cpu", "host")
	} else {
		// No usable KVM (absent, or present but not permitted, as on
		// GitHub-hosted runners where /dev/kvm exists yet opening it
		// fails) — fall back to multi-threaded TCG emulation.
		args = append(args, "-accel", "tcg,thread=multi")
	}

	inst.logger.Debugf("starting qemu-system-x86_64 with arguments: %v", args)

	stderrFile, err := os.Create(inst.stderrLog)
	if err != nil {
		return fmt.Errorf("%w: failed to create QEMU stderr log: %v", ErrFailed, err)
	}
	defer stderrFile.Close()

	//nolint:gosec // argument list is constructed above from validated config
	cmd := exec.CommandContext(ctx, "qemu-system-x86_64", args...)
	cmd.Stderr = stderrFile

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%w: failed to start QEMU: %v", ErrFailed, err)
	}

	inst.qemuCmd = cmd
	inst.qemuWait = make(chan error, 1)

	go func() {
		inst.qemuWait <- cmd.Wait()
	}()

	return nil
}

// waitForSSH cycles through the known BASIC-CI credentials until one opens
// an SSH session or the boot timeout expires.
//
// QEMU's own stderr and the guest serial log tails are attached to every
// failure, since both live in the work directory that is removed on Close
// and would otherwise be lost with the runner.
func (inst *Instance) waitForSSH(ctx context.Context, addr string, config *runconfig.RunConfig) (string, string, error) {
	bootCtx, bootCancel := context.WithTimeoutCause(ctx, bootTimeout,
		fmt.Errorf("timed out waiting for SSH on %s", addr))
	defer bootCancel()

	diag := func() string {
		return fmt.Sprintf("serial log tail:\n%s\nQEMU stderr tail:\n%s",
			tailFile(inst.serialLog, 80, 8192), tailFile(inst.stderrLog, 40, 4096))
	}

	for {
		select {
		case waitErr := <-inst.qemuWait:
			return "", "", fmt.Errorf("%w: QEMU exited before SSH came up (exit: %v):\n%s",
				ErrFailed, waitErr, diag())
		default:
		}

		for _, creds := range sshCredentials {
			attemptCtx, attemptCancel := context.WithTimeout(bootCtx, 20*time.Second)

			sshClient, err := remoteagent.WaitForSSH(attemptCtx, addr, creds.user, creds.password,
				config.LocalNetworkHelper, inst.logger)
			attemptCancel()

			if err == nil {
				_ = sshClient.Close()
				return creds.user, creds.password, nil
			}

			select {
			case waitErr := <-inst.qemuWait:
				return "", "", fmt.Errorf("%w: QEMU exited before SSH came up (exit: %v):\n%s",
					ErrFailed, waitErr, diag())
			case <-bootCtx.Done():
				return "", "", fmt.Errorf("%w: %v:\n%s",
					ErrFailed, context.Cause(bootCtx), diag())
			default:
			}
		}

		select {
		case waitErr := <-inst.qemuWait:
			return "", "", fmt.Errorf("%w: QEMU exited before SSH came up (exit: %v):\n%s",
				ErrFailed, waitErr, diag())
		case <-bootCtx.Done():
			return "", "", fmt.Errorf("%w: %v:\n%s",
				ErrFailed, context.Cause(bootCtx), diag())
		case <-time.After(3 * time.Second):
		}
	}
}

// tailFile returns the last lines of a log file (capped), for failure diagnostics.
func tailFile(path string, maxLines int, maxBytes int) string {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "(no log yet)"
	}

	if len(contents) > maxBytes {
		contents = contents[len(contents)-maxBytes:]
	}

	lines := strings.Split(string(contents), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	return strings.Join(lines, "\n")
}

func qemuVersion() string {
	out, err := exec.Command("qemu-system-x86_64", "--version").Output()
	if err != nil {
		return "unknown"
	}

	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(line)
}

func accelDescription() string {
	if kvmAvailable() {
		return "kvm"
	}
	return "tcg"
}

// kvmAvailable probes usability, not mere existence: on GitHub-hosted
// runners /dev/kvm exists but opening it fails with Permission denied.
func kvmAvailable() bool {
	file, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = file.Close()
	return true
}

func (inst *Instance) imageDescription() string {
	if inst.config.ImageName != "" {
		return inst.config.ImageName
	}
	return inst.config.ImageFamily
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port, nil
}
