package transport

import (
	"errors"
	"fmt"
	"strconv"
)

// Timing preserves the upstream stock values. Interval is a USB descriptor
// exponent, not milliseconds. No invalid preference is silently replaced.
type Timing struct {
	GapMS, HoldMS, EnterMS, Interval, EnumerationSeconds int
	LoadMS, RunMS, RemoveMS                              int
}

func DefaultTiming() Timing {
	return Timing{GapMS: 10, Interval: 10, EnumerationSeconds: 4, LoadMS: 1000}
}

func (t Timing) Validate() error {
	if t.GapMS < 1 || t.GapMS > 2000 || t.HoldMS < 0 || t.HoldMS > 2000 ||
		t.EnterMS < 0 || t.EnterMS > 2000 || t.Interval < 1 || t.Interval > 16 ||
		t.EnumerationSeconds < 1 || t.EnumerationSeconds > 120 ||
		t.LoadMS < 0 || t.LoadMS > 120000 || t.RunMS < 0 || t.RunMS > 120000 ||
		t.RemoveMS < 0 || t.RemoveMS > 120000 {
		return errors.New("USB timing outside upstream bounds")
	}
	return nil
}

func (d *Darwin) environment() ([]string, error) {
	c, t := d.settings()
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if err := ValidateController(c); err != nil {
		return nil, err
	}
	return []string{"MAC2JB_CONTROLLER=" + c, "MAC2JB_AUTO_WORKER=1",
		"MAC2JB_KEY_HOLD_MS=" + strconv.Itoa(t.HoldMS),
		"MAC2JB_ENTER_SETTLE_MS=" + strconv.Itoa(t.EnterMS),
		"MAC2JB_POLL_INTERVAL=" + strconv.Itoa(t.Interval),
		"MAC2JB_ENUMERATION_TIMEOUT_SECONDS=" + strconv.Itoa(t.EnumerationSeconds)}, nil
}

func ValidateController(c string) error {
	var n int
	if _, err := fmt.Sscanf(c, "usb-drd%d", &n); err != nil || n < 0 || n > 15 || c != "usb-drd"+strconv.Itoa(n) {
		return errors.New("select an observed usb-drd0..15 controller")
	}
	return nil
}
