package civswitch

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/readiness"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/usbgadget"
	"github.com/thedavidweng/any2jb/locales"
)

type Options struct {
	Manifest *manifest.Manifest
	// Assets is the packaged or prepared resource root containing bundled
	// CivSwitch payloads and notices when DeveloperSourceRoot is empty.
	Assets string
	// StagedRuntimeRoot is the verified immutable runtime directory containing
	// runtime/civswitch-hid and runtime/vendor/libusbd/libusbd.dylib on Darwin.
	StagedRuntimeRoot string
	// Explicit local source root for developer mode.
	DeveloperSourceRoot string
	// Explicit digest of the developer-built binary, not redistribution clearance.
	DeveloperHelperSHA256 string
	// Absolute path from the shared integrity-checked local helper build.
	DeveloperHelperPath string
	// Support is the app support directory. Verified save backups and read-only
	// save exports are retained there, outside every session workspace.
	Support string
	// DeveloperInstallerPython runs the pinned upstream trigger installer when
	// explicitly supplied; otherwise any2jb runs its verified pure-Go installer.
	DeveloperInstallerPython string
	Inspector                *readiness.Inspector
	// LinuxHelperCommand is a test seam for trusted Linux ConfigFS helper fixtures.
	LinuxHelperCommand func(payload, controller string, dryRun bool) (*exec.Cmd, error)
}

type Adapter struct {
	options      Options
	releaseError error
	// loader replaces the derived console endpoint in loopback fixtures, which
	// is how the gated actions are exercised without a console.
	loader string
}

func New(options Options) *Adapter {
	if options.Inspector == nil {
		inspector := readiness.New()
		options.Inspector = &inspector
	}
	return &Adapter{options: options}
}

func (a *Adapter) sourceRoot() string {
	if a.options.DeveloperSourceRoot != "" {
		return a.options.DeveloperSourceRoot
	}
	return a.options.Assets
}

func (a *Adapter) Writer() session.CivSwitchWriter { return a }

func (a *Adapter) Availability() error {
	if a.options.Manifest == nil {
		return locales.Key("dynCivManifestUnavailable")
	}
	d, err := a.options.Manifest.DistributionFor("civswitch-payload")
	if err != nil {
		return err
	}
	if a.options.DeveloperSourceRoot != "" {
		if d.Decision == "excluded" {
			return locales.Format("dynCivUnavailable", map[string]any{
				"reason": locales.DistributionReason("civswitch-payload", d.Reason),
			})
		}
		if !filepath.IsAbs(a.options.DeveloperSourceRoot) {
			return locales.Key("dynCivRootAbsolute")
		}
		return nil
	}
	if !d.Available() || a.options.Assets == "" {
		return locales.Format("dynCivUnavailable", map[string]any{
			"reason": locales.DistributionReason("civswitch-payload", d.Reason),
		})
	}
	if !filepath.IsAbs(a.options.Assets) {
		return locales.Key("dynCivRootAbsolute")
	}
	return nil
}

func (a *Adapter) verifySource() error {
	if err := a.Availability(); err != nil {
		return err
	}
	root := a.sourceRoot()
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return locales.Key("dynCivRootNotDirectory")
	}
	if err := a.options.Manifest.Verify(root, "civswitch-payload"); err != nil {
		return err
	}
	if a.options.DeveloperSourceRoot != "" {
		if err := a.options.Manifest.Verify(root, "civswitch-hid.c"); err != nil {
			return err
		}
	}
	return nil
}

// Check verifies the staged helper and payload without running a helper or opening USB.
func (a *Adapter) Check() error { return a.verify() }

func (a *Adapter) useLinuxHelper() bool {
	if a.options.LinuxHelperCommand != nil {
		return true
	}
	return runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") &&
		a.options.DeveloperHelperPath == "" && a.options.DeveloperHelperSHA256 == ""
}

func (a *Adapter) verify() error {
	if err := a.verifySource(); err != nil {
		return err
	}
	if a.useLinuxHelper() {
		if a.options.LinuxHelperCommand != nil {
			return nil
		}
		_, err := usbgadget.VerifySelfExecutable()
		return err
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return locales.Key("dynCivRequiresAppleSilicon")
	}
	if a.options.DeveloperSourceRoot == "" {
		if !filepath.IsAbs(a.options.StagedRuntimeRoot) {
			return locales.Key("dynCivStagedRootAbsolute")
		}
		if err := a.options.Manifest.Verify(a.options.StagedRuntimeRoot, "civswitch-hid"); err != nil {
			return err
		}
		return a.options.Manifest.Verify(a.options.StagedRuntimeRoot, "libusbd-dylib")
	}
	// A locally built helper has no release digest. Require the developer's
	// explicit digest, then use the same symlink-safe verifier as other assets.
	path := a.helperPath()
	if !filepath.IsAbs(path) {
		return errors.New("CivSwitch developer helper path must be absolute")
	}
	local := manifest.Manifest{Components: []manifest.Component{{ID: "developer-civswitch-helper", Path: filepath.Base(path), SHA256: a.options.DeveloperHelperSHA256}}}
	return local.Verify(filepath.Dir(path), "developer-civswitch-helper")
}

func (a *Adapter) helperPath() string {
	if a.options.DeveloperSourceRoot == "" {
		return filepath.Join(a.options.StagedRuntimeRoot, "runtime/civswitch-hid")
	}
	if a.options.DeveloperHelperPath != "" {
		return a.options.DeveloperHelperPath
	}
	return filepath.Join(a.options.DeveloperSourceRoot, "mac2jb-civswitch-hid")
}

func (a *Adapter) payload() string {
	for _, c := range a.options.Manifest.Components {
		if c.ID == "civswitch-payload" {
			return filepath.Join(a.sourceRoot(), c.Path)
		}
	}
	return ""
}

func (a *Adapter) Prepare(ctx context.Context) (time.Duration, error) {
	if err := a.verify(); err != nil {
		return 0, err
	}
	a.releaseError = nil // no writer is running during preparation
	var estimate time.Duration
	err := a.helper(ctx, true, func(e helperEvent) error {
		if e.Type != "dry-run" || e.Total != 87 || e.EstimatedMS <= 0 {
			return errors.New("invalid CivSwitch dry-run result")
		}
		estimate = time.Duration(e.EstimatedMS) * time.Millisecond
		return nil
	})
	if err == nil && estimate == 0 {
		err = errors.New("CivSwitch helper did not report a duration estimate")
	}
	return estimate, err
}

func (a *Adapter) Transfer(ctx context.Context, report session.Report) error {
	if err := a.verify(); err != nil {
		return err
	}
	a.releaseError = errors.New("composite keyboard and gamepad release was not confirmed")
	current, complete := 0, false
	err := a.helper(ctx, false, func(e helperEvent) error {
		switch e.Type {
		case "ready", "line-start":
			// Ready means both interfaces enumerated, not completed input.
		case "line-complete", "complete":
			if e.Total != 87 || e.Completed < current || e.Completed > 87 {
				return errors.New("invalid CivSwitch transfer progress")
			}
			if e.Type == "complete" && e.Completed != 87 {
				return errors.New("incomplete CivSwitch transfer")
			}
			current = e.Completed
			report(session.Progress{Current: current, Total: 87, SideEffect: true})
			if e.Type == "complete" {
				complete = true
				a.releaseError = nil
			}
		case "cancelled":
			a.releaseError = nil // pinned helper releases both interfaces first
		case "failed":
			return errors.New("CivSwitch composite helper failed")
		default:
			return errors.New("unexpected CivSwitch helper event")
		}
		return nil
	})
	if err == nil && !complete {
		err = errors.New("CivSwitch helper exited before complete")
	}
	return err
}

func (a *Adapter) Probe(ctx context.Context, address string, once bool) error {
	if err := a.verifySource(); err != nil {
		return err
	}
	if !consoleAddress(address) {
		return errors.New("CivSwitch requires the console LAN IPv4 address")
	}
	// This action deliberately needs neither a USB helper nor chat transfer.
	return probe(ctx, address, once)
}

// Transfer waits for the helper to exit before the controller calls Release.
// A killed or silent helper cannot establish neutral reports.
func (a *Adapter) Release(context.Context) error { return a.releaseError }

type helperEvent struct {
	Type        string `json:"type"`
	Completed   int    `json:"completed"`
	Total       int    `json:"total"`
	EstimatedMS int64  `json:"estimatedMs"`
}

func (a *Adapter) helper(ctx context.Context, dryRun bool, receive func(helperEvent) error) error {
	checks, err := a.options.Inspector.Inspect(ctx, session.Run{})
	if err != nil {
		return err
	}
	controller := ""
	found := 0
	for _, check := range checks {
		if check.State == session.RequiresAttention {
			return fmt.Errorf("CivSwitch prerequisite: %s %s", check.Message, check.NextAction)
		}
		switch check.ID {
		case "usb-drd0", "usb-drd1", "usb-drd2", "usb-drd3":
			controller = check.ID
			found++
		default:
			if a.useLinuxHelper() && check.Controller != "" {
				controller = check.Controller
				found++
			}
		}
	}
	if found != 1 || controller == "" {
		return errors.New("CivSwitch requires one discovered USB device controller")
	}
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var cmd *exec.Cmd
	var stdinCloser func()
	if a.useLinuxHelper() {
		builder := usbgadget.CivSwitchHelperCommand
		if a.options.LinuxHelperCommand != nil {
			builder = a.options.LinuxHelperCommand
		}
		cmd, err = builder(a.payload(), controller, dryRun)
		if err != nil {
			return err
		}
		stdinPipe, pipeErr := cmd.StdinPipe()
		if pipeErr != nil {
			return pipeErr
		}
		stdinCloser = func() { _ = stdinPipe.Close() }
		defer stdinCloser()
		stopCancel := context.AfterFunc(childCtx, func() {
			stdinCloser()
			if cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
		})
		defer stopCancel()
	} else {
		// Avoid the upstream auto parent, which can kill a worker before neutral
		// reports yet still emit cancelled. Own the single writer directly.
		args := []string{"--payload", a.payload(), "--controller", controller}
		if dryRun {
			args = append(args, "--dry-run")
		}
		cmd = exec.CommandContext(childCtx, a.helperPath(), args...)
		cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
		cmd.WaitDelay = 5 * time.Second
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 4096)
	var receiveErr error
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "MAC2JB_CIVSWITCH ") {
			continue
		}
		var e helperEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "MAC2JB_CIVSWITCH ")), &e); err != nil {
			receiveErr = err
			cancel()
			break
		}
		if err := receive(e); err != nil {
			receiveErr = err
			cancel()
			break
		}
	}
	if scanner.Err() != nil {
		cancel()
	}
	return errors.Join(receiveErr, scanner.Err(), cmd.Wait())
}

var _ session.CivSwitchAdapter = (*Adapter)(nil)
