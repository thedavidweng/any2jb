package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
)

type civAdapter struct {
	transfers, probes, releases int
	probeErr                    error
	transfer                    func(context.Context, session.Report) error
}

func (a *civAdapter) Availability() error                            { return nil }
func (a *civAdapter) Writer() session.CivSwitchWriter                { return nil }
func (a *civAdapter) Prepare(context.Context) (time.Duration, error) { return 23 * time.Minute, nil }

func (a *civAdapter) Transfer(ctx context.Context, report session.Report) error {
	a.transfers++
	if a.transfer != nil {
		return a.transfer(ctx, report)
	}
	report(session.Progress{Current: 87, Total: 87, SideEffect: true})
	return nil
}
func (a *civAdapter) Probe(context.Context, string, bool) error { a.probes++; return a.probeErr }
func (a *civAdapter) Release(context.Context) error             { a.releases++; return nil }

func civController(a *civAdapter) *session.SessionController {
	f := sessiontest.NewFakeEnvironment()
	env := f.Environment()
	env.CivSwitch = a
	return session.New(env, session.Profile{ID: "test", Revision: "pinned"})
}

func civPrerequisites() session.CivSwitchPrerequisites {
	return session.CivSwitchPrerequisites{PhysicalSwitch1Cartridge100: true, EnglishUS: true, SameReachableNetwork: true, LobbyChatFocused: true, ConsoleIPv4: "192.0.2.2"}
}

func TestCivSwitchRefusesMissingPrerequisites(t *testing.T) {
	for _, missing := range []string{"cartridge", "language", "network", "chat", "address"} {
		t.Run(missing, func(t *testing.T) {
			a := &civAdapter{}
			c := civController(a)
			p := civPrerequisites()
			switch missing {
			case "cartridge":
				p.PhysicalSwitch1Cartridge100 = false
			case "language":
				p.EnglishUS = false
			case "network":
				p.SameReachableNetwork = false
			case "chat":
				p.LobbyChatFocused = false
			case "address":
				p.ConsoleIPv4 = "not an IP"
			}
			if _, err := c.StartCivSwitch(p); err == nil {
				t.Fatal("accepted incomplete CivSwitch prerequisites")
			}
			if a.transfers != 0 || a.probes != 0 {
				t.Fatal("touched console before prerequisite confirmation")
			}
		})
	}
}

func TestCivSwitchRequiresCompositeAndAvailableAdapter(t *testing.T) {
	a := &civAdapter{}
	c := civController(a)
	c.ConfigureTransport(session.Transport{Mode: "keyboard", Capabilities: session.Capabilities{Keyboard: true}}, "")
	if _, err := c.StartCivSwitch(civPrerequisites()); err == nil {
		t.Fatal("accepted keyboard-only transport")
	}
	if a.transfers != 0 {
		t.Fatal("transferred without gamepad")
	}
	c = session.New(sessiontest.NewFakeEnvironment().Environment(), session.Profile{ID: "test", Revision: "pinned"})
	c.ConfigureTransport(session.Transport{Mode: "composite", Capabilities: session.Capabilities{Keyboard: true, Gamepad: true}}, "")
	if _, err := c.StartCivSwitch(civPrerequisites()); err == nil {
		t.Fatal("missing adapter accepted")
	}
	if _, err := c.ConnectCivSwitch("192.0.2.2"); err == nil {
		t.Fatal("missing probe adapter accepted")
	}
}

func TestCivSwitchStopReleasesCompositeAndBlocksReplay(t *testing.T) {
	started := make(chan struct{})
	a := &civAdapter{transfer: func(ctx context.Context, report session.Report) error {
		report(session.Progress{Current: 1, Total: 87, SideEffect: true})
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	c := civController(a)
	c.ConfigureTransport(session.Transport{Mode: "composite", Capabilities: session.Capabilities{Keyboard: true, Gamepad: true}}, "")
	if _, err := c.StartCivSwitch(civPrerequisites()); err != nil {
		t.Fatal(err)
	}
	<-started
	if s := c.Snapshot(); s.Current != 1 || s.EstimatedDuration != 23*time.Minute {
		t.Fatalf("progress: %+v", s)
	}
	if _, err := c.Cancel(); err == nil {
		t.Fatal("partial transfer was retry-safe")
	}
	if _, err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.Indeterminate)
	if a.releases != 1 || a.probes != 0 || s.Recovery != session.RestartConsole {
		t.Fatalf("cleanup %+v: %+v", a, s)
	}
	if _, err := c.StartCivSwitch(civPrerequisites()); err == nil {
		t.Fatal("partial transfer replayed")
	}
}

func TestCivSwitchLoaderFailureRechecksWithoutRetyping(t *testing.T) {
	a := &civAdapter{probeErr: errors.New("wrong service")}
	c := civController(a)
	c.ConfigureTransport(session.Transport{Mode: "composite", Capabilities: session.Capabilities{Keyboard: true, Gamepad: true}}, "")
	if _, err := c.StartCivSwitch(civPrerequisites()); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.Indeterminate)
	if s.LoaderVerified || a.transfers != 1 {
		t.Fatal(s)
	}
	a.probeErr = nil
	if _, err := c.RecheckCivSwitch(); err != nil {
		t.Fatal(err)
	}
	s = await(t, c, session.Verified)
	if !s.LoaderVerified || s.Outcome.Verified || a.transfers != 1 {
		t.Fatal(s)
	}
	if _, err := c.RunTool("inventory"); err == nil {
		t.Fatal("CivSwitch granted SmileBASIC RPC")
	}
}

func TestConnectCivSwitchDoesNotTypeOrRequireHID(t *testing.T) {
	a := &civAdapter{}
	c := civController(a)
	if _, err := c.ConnectCivSwitch("192.0.2.2"); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.Verified)
	if !s.CivSwitch || !s.LoaderVerified || s.Outcome.Verified || a.transfers != 0 || a.probes != 1 {
		t.Fatalf("%+v %+v", a, s)
	}
	if _, err := c.Finish(); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.Finished)
	if a.releases != 1 {
		t.Fatal("CivSwitch resources not released")
	}
}

func TestCivSwitchRestartNeedsExplicitConfirmation(t *testing.T) {
	a := &civAdapter{probeErr: errors.New("probe failed")}
	c := civController(a)
	c.ConfigureTransport(session.Transport{Mode: "composite", Capabilities: session.Capabilities{Keyboard: true, Gamepad: true}}, "")
	c.StartCivSwitch(civPrerequisites())
	await(t, c, session.Indeterminate)
	if _, err := c.ConfirmConsoleRestart(context.Background(), false); err == nil {
		t.Fatal("reset without console confirmation")
	}
	if _, err := c.StartCivSwitch(civPrerequisites()); err == nil {
		t.Fatal("unsafe replay")
	}
	if _, err := c.ConfirmConsoleRestart(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if s := c.Snapshot(); s.Stage != session.Idle || s.LoaderVerified || s.SideEffects {
		t.Fatal(s)
	}
}
