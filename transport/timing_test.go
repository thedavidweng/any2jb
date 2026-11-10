package transport_test

import (
	"context"
	"os/exec"
	"testing"

	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

func TestControllerAndTimingAreValidatedBeforeStartingAHelper(t *testing.T) {
	for _, controller := range []string{"auto", "usb-drd16", "usb-drd01", "usb-drd0 trailing", ""} {
		t.Run(controller, func(t *testing.T) {
			d := transport.NewHelperDarwin(transport.DarwinOptions{Controller: controller, Timing: transport.DefaultTiming()}, func(string, ...string) *exec.Cmd { t.Fatal("invalid controller launched"); return nil })
			if _, err := d.OpenKeyboard(context.Background(), session.Run{ID: "invalid"}, func(session.Progress) {}); err == nil {
				t.Fatal("accepted invalid controller")
			}
		})
	}
	for _, change := range []func(*transport.Timing){
		func(t *transport.Timing) { t.GapMS = 0 }, func(t *transport.Timing) { t.GapMS = 2001 },
		func(t *transport.Timing) { t.HoldMS = -1 }, func(t *transport.Timing) { t.HoldMS = 2001 },
		func(t *transport.Timing) { t.EnterMS = -1 }, func(t *transport.Timing) { t.EnterMS = 2001 },
		func(t *transport.Timing) { t.Interval = 0 }, func(t *transport.Timing) { t.Interval = 17 },
		func(t *transport.Timing) { t.EnumerationSeconds = 0 }, func(t *transport.Timing) { t.EnumerationSeconds = 121 },
		func(t *transport.Timing) { t.LoadMS = -1 }, func(t *transport.Timing) { t.LoadMS = 120001 },
		func(t *transport.Timing) { t.RunMS = -1 }, func(t *transport.Timing) { t.RunMS = 120001 },
		func(t *transport.Timing) { t.RemoveMS = -1 }, func(t *transport.Timing) { t.RemoveMS = 120001 },
	} {
		timing := transport.DefaultTiming()
		change(&timing)
		if err := timing.Validate(); err == nil {
			t.Fatalf("accepted invalid timing %+v", timing)
		}
	}
	if err := transport.DefaultTiming().Validate(); err != nil {
		t.Fatal(err)
	}
}
