package transport

import (
	"context"
	"errors"
	"os/exec"
	"runtime"

	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/usbgadget"
)

type LinuxOptions struct {
	Runtime    engine.Options
	Bootstrap  func(session.Run) (string, error)
	Controller string
	Timing     Timing
	Confirm    func(context.Context, session.Run, ConsoleStep) error
}

// NewLinux constructs a built-in USB HID keyboard host over Linux ConfigFS
// usb_f_hid and /dev/hidgN via the narrowly scoped helper process.
func NewLinux(opts ...LinuxOptions) *Darwin {
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
	return host
}

// NewHelperLinux wires a trusted process fixture for unit and differential tests.
func NewHelperLinux(o LinuxOptions, command HelperCommand) *Darwin {
	if o.Timing == (Timing{}) {
		o.Timing = DefaultTiming()
	}
	return &Darwin{
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
}

var _ USBKeyboard = (*Darwin)(nil)
