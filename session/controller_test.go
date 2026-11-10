package session_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func TestHostMustListenBeforeInputAndRunIdentityIsFresh(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	c := ready(t, f, false)
	c.Start()
	first := await(t, c, session.Verified)
	trace := f.Trace()
	if slices.Index(trace, "host.prepared") >= slices.Index(trace, "input.launch") {
		t.Fatalf("input preceded host readiness: %v", trace)
	}
	if _, err := c.Finish(); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.Finished)
	c.Inspect()
	await(t, c, session.Ready)
	c.Start()
	second := await(t, c, session.Verified)
	if first.RunID == "" || first.RunID == second.RunID {
		t.Fatal("run identity reused")
	}
	for _, e := range c.Events(0) {
		if e.Sequence == 0 || e.Elapsed < 0 {
			t.Fatal(e)
		}
	}
}

func TestUnreadyHostAndPairingPublicationHaveDifferentRecovery(t *testing.T) {
	t.Run("no input without validated listening host", func(t *testing.T) {
		f := sessiontest.NewFakeEnvironment()
		f.Prepared.Listening = false
		c := ready(t, f, false)
		c.Start()
		await(t, c, session.Failed)
		if slices.Contains(f.Trace(), "input.launch") {
			t.Fatal("input sent without listening host")
		}
	})
	t.Run("published pairing is already a side effect", func(t *testing.T) {
		f := sessiontest.NewFakeEnvironment()
		f.PrepareProgress = session.Progress{SideEffect: true, Message: "secret pairing token"}
		f.PrepareError = errors.New("secret pairing token")
		c := ready(t, f, false)
		c.Start()
		await(t, c, session.Indeterminate)
		for _, e := range c.Events(0) {
			if strings.Contains(e.Message, "secret") {
				t.Fatal("adapter detail leaked into event")
			}
		}
	})
	t.Run("cleanup failure cannot be called cancelled", func(t *testing.T) {
		f := sessiontest.NewFakeEnvironment()
		f.PrepareGate = make(chan struct{})
		f.CleanupError = errors.New("cleanup failed")
		c := ready(t, f, false)
		c.Start()
		c.Cancel()
		await(t, c, session.Indeterminate)
	})
}

func TestCancelInspectionDoesNotGrantLaunchEligibility(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	f.InspectGate = make(chan struct{})
	c := session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"})
	c.Inspect()
	if _, err := c.Cancel(); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.Ready)
	if _, err := c.Start(); err == nil {
		t.Fatal("cancelled inspection granted launch eligibility")
	}
}

func TestProfileAndTransportChangesInvalidateReadiness(t *testing.T) {
	c := ready(t, sessiontest.NewFakeEnvironment(), false)
	if _, err := c.ConfigureTransport(session.Transport{Mode: "keyboard", Capabilities: session.Capabilities{Keyboard: true}}, "not-an-ip"); err == nil {
		t.Fatal("invalid IPv4 accepted")
	}
	if _, err := c.ConfigureTransport(session.Transport{Mode: "keyboard", Capabilities: session.Capabilities{Keyboard: true}}, "192.0.2.10"); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().Stage != session.Idle || len(c.Snapshot().Checks) != 0 {
		t.Fatal("transport retained stale checks")
	}
	if _, err := c.SelectProfile(session.Profile{ID: "other", Revision: "pinned"}, "demo"); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().BootstrapSaved {
		t.Fatal("profile reused saved bootstrap")
	}
	text, err := c.ExportDiagnostics(context.Background())
	if err != nil || !strings.Contains(text, "simulated") {
		t.Fatalf("diagnostics lost simulation label: %q, %v", text, err)
	}
}

func await(t *testing.T, c *session.SessionController, stage session.Stage) session.Snapshot {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		s := c.Snapshot()
		if s.Stage == stage && !s.Busy {
			return s
		}
		select {
		case <-deadline:
			t.Fatalf("wanted %s, got %+v", stage, s)
		case <-time.After(time.Millisecond):
		}
	}
}

func active(t *testing.T, c *session.SessionController, stage session.Stage) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if c.Snapshot().Stage == stage {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("missing active stage %s: %+v", stage, c.Snapshot())
		case <-time.After(time.Millisecond):
		}
	}
}

func ready(t *testing.T, f *sessiontest.FakeEnvironment, held bool) *session.SessionController {
	t.Helper()
	c := session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned", RetainsRPC: held, Tools: []string{"inventory"}})
	if _, err := c.Inspect(); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.Ready)
	return c
}

func TestCancelStopAndFinishAreDifferent(t *testing.T) {
	t.Run("cancel before console side effects", func(t *testing.T) {
		f := sessiontest.NewFakeEnvironment()
		f.PrepareGate = make(chan struct{})
		c := ready(t, f, false)
		if _, err := c.Start(); err != nil {
			t.Fatal(err)
		}
		active(t, c, session.PreparingHost)
		if _, err := c.Stop(); err == nil {
			t.Fatal("Stop accepted before effects")
		}
		if _, err := c.Cancel(); err != nil {
			t.Fatal(err)
		}
		if s := await(t, c, session.Ready); s.SideEffects {
			t.Fatal("Cancel touched console")
		}
	})
	t.Run("stop during input requires console restart", func(t *testing.T) {
		f := sessiontest.NewFakeEnvironment()
		f.InputGate = make(chan struct{})
		c := ready(t, f, false)
		c.Start()
		active(t, c, session.WaitingForConsole)
		if _, err := c.Cancel(); err == nil {
			t.Fatal("Cancel accepted after effects")
		}
		if _, err := c.Finish(); err == nil {
			t.Fatal("Finish accepted before verification")
		}
		if _, err := c.Stop(); err != nil {
			t.Fatal(err)
		}
		s := await(t, c, session.Indeterminate)
		if s.Recovery != session.RestartConsole {
			t.Fatalf("wrong recovery: %+v", s)
		}
		if _, err := c.Start(); err == nil {
			t.Fatal("blind retry accepted")
		}
		if _, err := c.Inspect(); err == nil {
			t.Fatal("inspection erased indeterminate state")
		}
	})
	t.Run("finish releases verified run", func(t *testing.T) {
		c := ready(t, sessiontest.NewFakeEnvironment(), true)
		c.Start()
		s := await(t, c, session.RetainedRPC)
		if !s.Outcome.FSVerified {
			t.Fatal("unverified retained run")
		}
		if _, err := c.Stop(); err == nil {
			t.Fatal("Stop substituted for Finish")
		}
		if _, err := c.Finish(); err != nil {
			t.Fatal(err)
		}
		await(t, c, session.Finished)
	})
}

func TestIncompleteVerificationNeverProducesVerifiedEvent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		held       bool
		invalidate func(*session.Outcome)
	}{
		{"exploit", false, func(o *session.Outcome) { o.Verified = false }},
		{"filesystem", false, func(o *session.Outcome) { o.FSVerified = false }},
		{"restoration", false, func(o *session.Outcome) { o.Restored = false }},
		{"cleanup", false, func(o *session.Outcome) { o.CleanedUp = false }},
		{"held endpoint", true, func(o *session.Outcome) { o.RetainedReady = false }},
		{"held deferred cleanup", true, func(o *session.Outcome) { o.CleanupDeferred = false }},
		{"held donor cleanup", true, func(o *session.Outcome) { o.DonorCleanedUp = false }},
		{"held live RPC", true, func(o *session.Outcome) { o.RPCListening = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := sessiontest.NewFakeEnvironment()
			tc.invalidate(&f.Outcome)
			c := ready(t, f, tc.held)
			c.Start()
			await(t, c, session.Indeterminate)
			for _, e := range c.Events(0) {
				if e.Stage == session.Verified || e.Stage == session.RetainedRPC {
					t.Fatalf("false verification: %+v", e)
				}
			}
		})
	}
}

func TestToolsAreSingleFlightAndCapabilityGated(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	gate := make(chan struct{})
	f.ToolGate = gate
	c := ready(t, f, true)
	if _, err := c.RunTool("inventory"); err == nil {
		t.Fatal("tool accepted before verification")
	}
	c.Start()
	await(t, c, session.RetainedRPC)
	if _, err := c.RunTool("write-save"); err == nil {
		t.Fatal("unsupported tool accepted")
	}
	if _, err := c.RunTool("inventory"); err != nil {
		t.Fatal(err)
	}
	active(t, c, session.RunningTool)
	if _, err := c.RunTool("inventory"); err == nil {
		t.Fatal("concurrent tool accepted")
	}
	close(gate)
	await(t, c, session.RetainedRPC)
}

func TestBootstrapNeedsSubmissionThenConsoleConfirmation(t *testing.T) {
	c := ready(t, sessiontest.NewFakeEnvironment(), false)
	if _, err := c.SaveBootstrap(true); err == nil {
		t.Fatal("save without preparation")
	}
	c.PrepareBootstrap()
	await(t, c, session.BootstrapSetup)
	if _, err := c.SaveBootstrap(true); err == nil {
		t.Fatal("confirmation before SAVE submission")
	}
	if _, err := c.SaveBootstrap(false); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.BootstrapSetup)
	if s.BootstrapSaved {
		t.Fatal("SAVE submission treated as confirmed save")
	}
	if _, err := c.SaveBootstrap(true); err != nil {
		t.Fatal(err)
	}
	if !await(t, c, session.Ready).BootstrapSaved {
		t.Fatal("explicit save not recorded")
	}
}

func TestSnapshotsCannotMutateController(t *testing.T) {
	c := ready(t, sessiontest.NewFakeEnvironment(), false)
	s := c.Snapshot()
	s.Checks[0].State = session.RequiresAttention
	s.Profile.Tools[0] = "write-save"
	if c.Snapshot().Checks[0].State != session.CheckVerified || c.Snapshot().Profile.Tools[0] != "inventory" {
		t.Fatal("snapshot aliases authoritative state")
	}
}

func TestCompositeCapabilityAndReadinessBlockers(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	f.Checks[0].State = session.Available
	c := session.New(f.Environment(), session.Profile{ID: "civ", Revision: "pinned", CivSwitch: true, RequiredTransport: session.Capabilities{Keyboard: true, Gamepad: true}})
	c.Inspect()
	await(t, c, session.NeedsSetup)
	if _, err := c.Start(); err == nil {
		t.Fatal("unverified readiness accepted")
	}
	if _, err := c.PrepareBootstrap(); err == nil {
		t.Fatal("manual transport approximated composite HID")
	}
}

func TestReadinessAndRejectedStart(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	c := session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"})
	if _, err := c.Start(); err == nil {
		t.Fatal("start before inspection accepted")
	}
	if _, err := c.Inspect(); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.Ready)
	if len(s.Checks) == 0 {
		t.Fatalf("fake readiness must be explicit: %+v", s)
	}
	events := c.Events(0)
	if events[0].Reason != session.InvalidState {
		t.Fatalf("missing typed rejection: %+v", events)
	}
	for i, e := range events {
		if e.Sequence != uint64(i+1) {
			t.Fatal("nonmonotonic events")
		}
	}
}
