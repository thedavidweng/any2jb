package transport

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/thedavidweng/any2jb/internal/usbgadget"
)

// Timing preserves upstream stroke and command waits. The native keyboard's
// descriptor interval remains its fixed stock value of 10.
type Timing struct {
	GapMS, HoldMS, EnterMS, EnumerationSeconds int
	LoadMS, RunMS, RemoveMS                    int
}

func DefaultTiming() Timing {
	return Timing{GapMS: 10, EnumerationSeconds: 4, LoadMS: 1000}
}

func (t Timing) Validate() error {
	if t.GapMS < 1 || t.GapMS > 2000 || t.HoldMS < 0 || t.HoldMS > 2000 ||
		t.EnterMS < 0 || t.EnterMS > 2000 ||
		t.EnumerationSeconds < 1 || t.EnumerationSeconds > 120 ||
		t.LoadMS < 0 || t.LoadMS > 120000 || t.RunMS < 0 || t.RunMS > 120000 ||
		t.RemoveMS < 0 || t.RemoveMS > 120000 {
		return errors.New("USB timing outside upstream bounds")
	}
	return nil
}

func (d *Darwin) validateControllerName(c string) error {
	if d.validateController != nil {
		return d.validateController(c)
	}
	return ValidateController(c)
}

func (d *Darwin) environment() ([]string, error) {
	c, t := d.settings()
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if err := d.validateControllerName(c); err != nil {
		return nil, err
	}
	return []string{"MAC2JB_CONTROLLER=" + c,
		"MAC2JB_ENUMERATION_TIMEOUT_SECONDS=" + strconv.Itoa(t.EnumerationSeconds)}, nil
}

func ValidateController(c string) error {
	var n int
	if _, err := fmt.Sscanf(c, "usb-drd%d", &n); err != nil || n < 0 || n > 15 || c != "usb-drd"+strconv.Itoa(n) {
		return errors.New("select an observed usb-drd0..15 controller")
	}
	return nil
}

func ValidateLinuxController(c string) error {
	if !usbgadget.ValidUDCName(c) || strings.HasPrefix(c, "usb-drd") {
		return errors.New("select an observed Linux UDC controller from /sys/class/udc")
	}
	return nil
}

func ValidateAnyController(c string) error {
	if ValidateController(c) == nil || ValidateLinuxController(c) == nil {
		return nil
	}
	return errors.New("select an observed USB device controller")
}
