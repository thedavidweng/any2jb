package transport_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/transport"
)

func TestDispatcherKeepsManualIndependentAndDoesNotSubstituteItForBlockedUSB(t *testing.T) {
	manual := transport.NewManual(func(session.Run) (string, error) { return "PRINT 1\n", nil })
	inspections := 0
	keyboard := transport.NewHelperDarwin(transport.DarwinOptions{Controller: "usb-drd0", Timing: transport.DefaultTiming()}, func(string, ...string) *exec.Cmd {
		t.Error("blocked USB reached a helper")
		return nil
	})
	input := transport.NewDispatcher(manual, keyboard, func(context.Context, session.Run) ([]session.Check, error) {
		inspections++
		return []session.Check{{ID: "driver-loaded", State: session.RequiresAttention, GatesTransport: "usb-hid"}, {ID: "usb-drd0", State: session.Available, GatesTransport: "usb-hid"}}, nil
	})
	if input.Manual() != manual || input.USB() != keyboard {
		t.Fatal("dispatcher lost adapter access")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	run := session.Run{ID: "manual", Edition: "full", Transport: session.Transport{Mode: "manual", Capabilities: session.Capabilities{Manual: true}}}
	done := make(chan error, 1)
	go func() {
		done <- input.Send(ctx, run, session.Bootstrap, session.PreparedHost{}, func(session.Progress) {})
	}()
	for _, kind := range []string{"slot", "bootstrap", "direct"} {
		for manual.Prompt().Kind != kind {
			select {
			case <-ctx.Done():
				t.Fatal("manual prompt not published")
			case <-time.After(time.Millisecond):
			}
		}
		p := manual.Prompt()
		if kind == "bootstrap" && p.Text != "PRINT 1\n" {
			t.Fatal("shared bootstrap loader changed text")
		}
		if !manual.Acknowledge(p.RunID, p.Sequence) {
			t.Fatal("manual step was not acknowledged")
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if inspections != 0 {
		t.Fatal("manual input was gated by USB readiness")
	}
	run.Transport = session.Transport{Mode: "keyboard", Controller: "usb-drd0", Capabilities: session.Capabilities{Keyboard: true}}
	if err := input.Send(ctx, run, session.Bootstrap, session.PreparedHost{}, func(session.Progress) { t.Error("blocked USB reported typing") }); err == nil {
		t.Fatal("blocked USB pretended delivery")
	}
	if inspections != 1 || manual.Prompt().Kind != "" {
		t.Fatal("blocked USB silently substituted a manual prompt")
	}
}
