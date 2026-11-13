package transport

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sync"

	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/session"
)

// DarwinOptions is explicit: packaged startup never supplies developer helpers.
// Bootstrap is shared with the manual adapter and returns the exact selected text.
// Confirm must wait for the user to observe the named console boundary.
type DarwinOptions struct {
	Runtime    engine.Options
	Helpers    *DeveloperHelpers
	Bootstrap  func(session.Run) (string, error)
	Controller string
	Timing     Timing
	Confirm    func(context.Context, session.Run, ConsoleStep) error
}

type ConsoleStep string

const (
	EditorFocus   ConsoleStep = "editor-focus"
	LoadCommand   ConsoleStep = "load"
	LoadPrompt    ConsoleStep = "load-prompt"
	RunCommand    ConsoleStep = "run"
	RemoveCommand ConsoleStep = "remove"
	StartCommand  ConsoleStep = "start"
	SaveCommand   ConsoleStep = "save"
)

// HelperCommand is a trusted-process fixture seam, not production authorization.
type HelperCommand func(string, ...string) *exec.Cmd

// USBKeyboard is the shared built-in USB HID keyboard surface implemented by
// Darwin (libusbd) and Linux (ConfigFS usb_f_hid).
type USBKeyboard interface {
	InputTransport
	session.KeyboardTransport
	Configure(controller string, timing Timing) error
	EnumeratedController() string
	OpenKeyboard(ctx context.Context, run session.Run, report session.Report) (*Keyboard, error)
	Check(ctx context.Context, run session.Run) error
}

type Darwin struct {
	options            DarwinOptions
	command            HelperCommand
	validateController func(string) error
	verifyHelper       func() error
	mu                 sync.Mutex
	active             *helperProcess
	trusted            bool
	configMu           sync.RWMutex
	controller         string
	timing             Timing
	enumerated         string
	stagedRoot         string
	workspaces         map[string]string
}

func NewDarwin(options DarwinOptions) *Darwin {
	d := &Darwin{options: options, controller: options.Controller, timing: options.Timing}
	d.command = func(name string, args ...string) *exec.Cmd {
		// Authorization is checked again in start/send before constructing a command.
		if d.options.Helpers != nil {
			return exec.Command(d.options.Helpers.path(name), args...)
		}
		d.configMu.RLock()
		root := d.stagedRoot
		d.configMu.RUnlock()
		if root == "" {
			return nil
		}
		return exec.Command(filepath.Join(root, "runtime", name), args...)
	}
	return d
}

// NewHelperDarwin bypasses binary authorization for trusted process fixtures only.
// The application must use NewDarwin.
func NewHelperDarwin(options DarwinOptions, command HelperCommand) *Darwin {
	return &Darwin{options: options, command: command, trusted: true, controller: options.Controller, timing: options.Timing}
}

func (d *Darwin) Configure(controller string, timing Timing) error {
	if err := timing.Validate(); err != nil {
		return err
	}
	if err := d.validateControllerName(controller); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.active != nil {
		return errors.New("release the active USB writer before changing settings")
	}
	d.configMu.Lock()
	defer d.configMu.Unlock()
	if d.controller != controller {
		d.enumerated = ""
	}
	d.controller, d.timing = controller, timing
	return nil
}

func (d *Darwin) EnumeratedController() string { d.mu.Lock(); defer d.mu.Unlock(); return d.enumerated }

func (d *Darwin) settings() (string, Timing) {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.controller, d.timing
}

func (d *Darwin) Send(ctx context.Context, run session.Run, action session.InputAction, host session.PreparedHost, report session.Report) error {
	if run.Edition != "full" && run.Edition != "demo" {
		return errors.New("keyboard workflow requires an explicit full or demo edition")
	}
	return d.send(ctx, run, action, host, report)
}

func (d *Darwin) authorize(run session.Run) (string, error) {
	if d.trusted {
		return "", nil
	}
	o := d.options.Runtime
	if o.Manifest == nil {
		return "", errors.New("missing USB runtime manifest")
	}
	p, err := o.Manifest.Profile(run.Profile.ID)
	if err != nil {
		return "", err
	}
	developerMode := o.DeveloperSourceRoot != "" || o.DeveloperProfileID != ""
	if developerMode {
		if o.DeveloperSourceRoot == "" || o.DeveloperProfileID != run.Profile.ID {
			return "", errors.New("USB input requires explicit developer staging for the selected profile")
		}
	} else if !p.Distribution.Available() {
		return "", fmt.Errorf("USB input unavailable for profile %s: %s", p.ID, p.Distribution.Reason)
	}
	if run.Profile.CivSwitch || p.Revision != run.Profile.Revision || p.SourceSHA256 != run.Profile.SourceSHA256 ||
		p.BootstrapSHA256 != run.Profile.BootstrapSHA256 || p.SaveName != run.Profile.SaveName ||
		p.StartArgumentCount != run.Profile.StartArgumentCount || p.StartPort != run.Profile.StartPort ||
		!slices.Contains(p.SessionProfile().Editions, run.Edition) {
		return "", errors.New("USB profile or edition identity mismatch")
	}
	root, err := engine.Stage(o)
	if err != nil {
		return "", err
	}
	if err := o.Manifest.VerifyProfile(root, p.ID); err != nil {
		return "", err
	}
	d.configMu.Lock()
	d.stagedRoot = root
	d.configMu.Unlock()
	return root, nil
}

// Check verifies bundled or developer provenance, bytes and settings without
// executing a helper or initializing USB. It is not enumeration or hardware evidence.
func (d *Darwin) Check(ctx context.Context, run session.Run) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := d.authorize(run)
	if err != nil {
		return err
	}
	if _, err := d.environment(); err != nil {
		return err
	}
	if d.verifyHelper != nil {
		return d.verifyHelper()
	}
	if d.options.Helpers != nil {
		return d.options.Helpers.verify()
	}
	if !d.trusted {
		if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
			return errors.New("Darwin USB helpers require Apple Silicon macOS")
		}
		if d.options.Runtime.DeveloperSourceRoot != "" {
			return errors.New("developer USB helpers require BuildDeveloperHelpers")
		}
		if err := engine.VerifyUSB(root, d.options.Runtime.Manifest); err != nil {
			return err
		}
		return d.options.Runtime.Manifest.Verify(root, "libusbd-dylib")
	}
	return nil
}

func (d *Darwin) Release(ctx context.Context, run session.Run) error {
	d.mu.Lock()
	p := d.active
	d.mu.Unlock()
	if p == nil {
		return d.releaseWorkspace(run)
	}
	if p.run.ID != run.ID {
		return errors.New("run does not own USB keyboard")
	}
	return errors.Join(p.close(ctx), d.releaseWorkspace(run))
}

var _ InputTransport = (*Darwin)(nil)
