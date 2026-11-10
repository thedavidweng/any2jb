package transport_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
	"github.com/thedavidweng/any2jb/transport"
)

func TestBootstrapCanCancelBeforePublicationWithoutStartingAHelper(t *testing.T) {
	const text = "PRINT 1\n"
	sum := sha256.Sum256([]byte(text))
	focus := make(chan struct{}, 1)
	d := transport.NewHelperDarwin(transport.DarwinOptions{Controller: "usb-drd0", Timing: transport.DefaultTiming(),
		Bootstrap: func(session.Run) (string, error) { return text, nil },
		Confirm: func(ctx context.Context, _ session.Run, step transport.ConsoleStep) error {
			if step != transport.EditorFocus {
				t.Error("wrong console boundary")
			}
			focus <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		}}, func(string, ...string) *exec.Cmd { t.Error("helper launched before focus confirmation"); return nil })
	f := sessiontest.NewFakeEnvironment()
	f.Checks = append(f.Checks, session.Check{ID: "usb-drd0", Controller: "usb-drd0", State: session.Available, GatesTransport: "usb-hid"})
	env := f.Environment()
	env.Input = d
	c := session.New(env, session.Profile{ID: "fixture", Revision: "pinned", BootstrapSHA256: hex.EncodeToString(sum[:])})
	c.ConfigureTransport(session.Transport{Mode: "keyboard", Controller: "usb-drd0", Capabilities: session.Capabilities{Keyboard: true}}, "192.0.2.1")
	c.Inspect()
	awaitTransportStage(t, c, session.NeedsSetup)
	if _, err := c.PrepareBootstrap(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-focus:
	case <-time.After(3 * time.Second):
		t.Fatal("focus boundary not reached")
	}
	if c.Snapshot().SideEffects {
		t.Fatal("waiting for editor focus was treated as partial typing")
	}
	if _, err := c.Cancel(); err != nil {
		t.Fatal(err)
	}
	awaitTransportStage(t, c, session.Ready)
	if c.Snapshot().SideEffects {
		t.Fatal("Cancel touched console state")
	}
}
