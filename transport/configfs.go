package transport

import (
	"context"
	"errors"
	"os/exec"
	"runtime"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/internal/usbgadget"
	"github.com/thedavidweng/any2jb/session"
)

type LinuxOptions struct {
	Runtime    engine.Options
	Bootstrap  func(session.Run) (string, error)
	Controller string
	Timing     Timing
	Confirm    func(context.Context, session.Run, ConsoleStep) error
}

// Linux implements built-in USB HID keyboard transport over Linux ConfigFS
// usb_f_hid and /dev/hidgN via a narrowly scoped helper process.
type Linux struct {
	host *Darwin
}

func NewLinux(opts ...LinuxOptions) *Linux {
	var o LinuxOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.Timing == (Timing{}) {
		o.Timing = DefaultTiming()
	}
	host := &Darwin{
		options: DarwinOptions{
			Runtime:    o.Runtime,
			Bootstrap:  o.Bootstrap,
			Controller: o.Controller,
			Timing:     o.Timing,
			Confirm:    o.Confirm,
		},
		validateController: ValidateLinuxController,
		controller:         o.Controller,
		timing:             o.Timing,
	}
	host.verifyHelper = func() error {
		if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
			return errors.New("Linux ConfigFS HID requires linux/amd64 or linux/arm64")
		}
		_, err := usbgadget.VerifySelfExecutable()
		return err
	}
	host.command = func(_ string, _ ...string) *exec.Cmd {
		controller, timing := host.settings()
		cmd, err := usbgadget.KeyboardHelperCommand(controller, timing.EnumerationSeconds)
		if err != nil {
			return nil
		}
		return cmd
	}
	return &Linux{host: host}
}

// NewHelperLinux wires a trusted process fixture for unit and differential tests.
func NewHelperLinux(o LinuxOptions, command HelperCommand) *Linux {
	if o.Timing == (Timing{}) {
		o.Timing = DefaultTiming()
	}
	host := &Darwin{
		options: DarwinOptions{
			Runtime:    o.Runtime,
			Bootstrap:  o.Bootstrap,
			Controller: o.Controller,
			Timing:     o.Timing,
			Confirm:    o.Confirm,
		},
		command:            command,
		validateController: ValidateLinuxController,
		verifyHelper:       func() error { return nil },
		trusted:            true,
		controller:         o.Controller,
		timing:             o.Timing,
	}
	return &Linux{host: host}
}

func (l *Linux) Configure(controller string, timing Timing) error {
	return l.host.Configure(controller, timing)
}

func (l *Linux) EnumeratedController() string { return l.host.EnumeratedController() }

func (l *Linux) SelectedKeyboardController() string { return l.host.SelectedKeyboardController() }

func (l *Linux) Check(ctx context.Context, run session.Run) error { return l.host.Check(ctx, run) }

func (l *Linux) OpenKeyboard(ctx context.Context, run session.Run, report session.Report) (*Keyboard, error) {
	return l.host.OpenKeyboard(ctx, run, report)
}

func (l *Linux) OpenKeyboardSession(ctx context.Context, run session.Run, report session.Report) (session.Keyboard, error) {
	return l.host.OpenKeyboardSession(ctx, run, report)
}

func (l *Linux) Send(ctx context.Context, run session.Run, action session.InputAction, host session.PreparedHost, report session.Report) error {
	return l.host.Send(ctx, run, action, host, report)
}

func (l *Linux) Release(ctx context.Context, run session.Run) error {
	return l.host.Release(ctx, run)
}

var (
	_ InputTransport = (*Linux)(nil)
	_ USBKeyboard    = (*Linux)(nil)
	_ USBKeyboard    = (*Darwin)(nil)
)
