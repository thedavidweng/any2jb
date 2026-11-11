package engine_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func protocolRunner(t *testing.T, scenario string) *engine.HostRunner {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for structured bridge fixture")
	}
	fixture, err := filepath.Abs("testdata/fake-engine.cjs")
	if err != nil {
		t.Fatal(err)
	}
	return engine.NewProtocolRunner(t.TempDir(), func(run session.Run, workspace string) *exec.Cmd {
		return exec.Command(node, fixture, scenario, run.ID, workspace)
	})
}

func testRun() session.Run {
	return session.Run{ID: "fixture-run", LANIPv4: "192.0.2.1",
		Profile: session.Profile{ID: "fixture", StartPort: 39380, StartArgumentCount: 5}}
}

func TestCancelDuringAuthorizationDoesNotLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := engine.NewProtocolRunner(t.TempDir(), func(session.Run, string) *exec.Cmd {
		cancel()
		return exec.Command("must-not-launch")
	})
	if _, err := runner.Prepare(ctx, testRun(), func(session.Progress) {}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled authorization still attempted process launch")
	}
}

func TestProtocolRunnerVerified(t *testing.T) {
	runner, run := protocolRunner(t, "verified"), testRun()
	defer runner.Cleanup(context.Background(), run)
	host, err := runner.Prepare(context.Background(), run, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	if !host.Listening || !host.PairingValid {
		t.Fatal("input permitted without host readiness")
	}
	outcome, err := runner.Run(context.Background(), run, host, func(session.Progress) {})
	if err != nil || !outcome.Verified || !outcome.FSVerified || !outcome.Restored || !outcome.CleanedUp {
		t.Fatalf("verification refused: %+v %v", outcome, err)
	}
}

func TestProtocolRunnerRefusesIncompleteEvidence(t *testing.T) {
	for _, scenario := range []string{"partial", "crash", "disconnect", "protocol-error", "truncated", "out-of-order", "dirty-exit", "cleanup-error"} {
		t.Run(scenario, func(t *testing.T) {
			runner, run := protocolRunner(t, scenario), testRun()
			defer runner.Cleanup(context.Background(), run)
			host, err := runner.Prepare(context.Background(), run, func(session.Progress) {})
			if err == nil {
				_, err = runner.Run(context.Background(), run, host, func(session.Progress) {})
			}
			if err == nil {
				t.Fatal("invalid verification accepted")
			}
		})
	}
}

func TestProtocolRunnerRefusesInvalidHeldLease(t *testing.T) {
	for _, scenario := range []string{"held-dead-rpc", "held-wrong-session", "held-public-artifact"} {
		t.Run(scenario, func(t *testing.T) {
			runner, run := protocolRunner(t, scenario), testRun()
			run.Profile.RetainsRPC = true
			defer runner.Cleanup(context.Background(), run)
			host, err := runner.Prepare(context.Background(), run, func(session.Progress) {})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runner.Run(context.Background(), run, host, func(session.Progress) {}); err == nil {
				t.Fatal("invalid held lease accepted")
			}
		})
	}
}

func TestProtocolRunnerBindFailureIsBeforeSideEffects(t *testing.T) {
	runner, run := protocolRunner(t, "bind-failure"), testRun()
	effect := false
	if _, err := runner.Prepare(context.Background(), run, func(p session.Progress) {
		effect = effect || p.SideEffect
	}); err == nil {
		t.Fatal("bind failure accepted")
	}
	if effect {
		t.Fatal("bind failure reported console effects")
	}
	// Failed Prepare owns its terminal cleanup, even without a controller.
	if err := runner.Cleanup(context.Background(), run); err != nil {
		t.Fatal(err)
	}
}

func TestProtocolRunnerCancelAndStopThroughController(t *testing.T) {
	for _, afterEffect := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "stop"}[afterEffect], func(t *testing.T) {
			scenario := "waiting"
			if afterEffect {
				scenario = "ready-wait"
			}
			runner := protocolRunner(t, scenario)
			f := sessiontest.NewFakeEnvironment()
			f.InputGate = make(chan struct{})
			env := f.Environment()
			env.Host = runner
			profile := testRun().Profile
			profile.Revision = "fixture"
			c := session.New(env, profile)
			c.ConfigureTransport(session.Transport{Mode: "manual", Capabilities: session.Capabilities{Manual: true}}, "192.0.2.1")
			c.Inspect()
			awaitSnapshot(t, c, func(s session.Snapshot) bool { return s.Stage == session.Ready && !s.Busy })
			if _, err := c.Start(); err != nil {
				t.Fatal(err)
			}
			awaitSnapshot(t, c, func(s session.Snapshot) bool {
				if afterEffect {
					return s.Stage == session.WaitingForConsole
				}
				return len(s.Processes) > 0
			})
			want := session.Ready
			if afterEffect {
				want = session.Indeterminate
				if _, err := c.Stop(); err != nil {
					t.Fatal(err)
				}
			} else if _, err := c.Cancel(); err != nil {
				t.Fatal(err)
			}
			s := awaitSnapshot(t, c, func(s session.Snapshot) bool { return s.Stage == want && !s.Busy })
			if len(s.Processes) != 0 {
				t.Fatal("owned processes retained after teardown")
			}
			if afterEffect && s.Recovery != session.RestartConsole {
				t.Fatal("Stop allowed automatic replay")
			}
		})
	}
}

func awaitSnapshot(t *testing.T, c *session.SessionController, predicate func(session.Snapshot) bool) session.Snapshot {
	t.Helper()
	timeout := time.NewTimer(time.Minute)
	defer timeout.Stop()
	for {
		s := c.Snapshot()
		if predicate(s) {
			return s
		}
		select {
		case <-timeout.C:
			t.Fatalf("controller did not reach expected state: %+v", s)
		default:
			runtime.Gosched()
		}
	}
}

func TestProductionRunnerRefusesPackagedEngine(t *testing.T) {
	o := fixture(t)
	runner := engine.NewHostRunner(o)
	if _, err := runner.Prepare(context.Background(), testRun(), func(session.Progress) {}); err == nil {
		t.Fatal("packaged engine bypassed developer-stage requirement")
	}
	if _, err := os.Stat(filepath.Join(o.Support, "session.lock")); !os.IsNotExist(err) {
		t.Fatal("failed launch retained session lock")
	}
}

func TestProtocolRunnerHeldFinish(t *testing.T) {
	runner, run := protocolRunner(t, "held-verified"), testRun()
	run.Profile.RetainsRPC = true
	defer runner.Cleanup(context.Background(), run)
	host, err := runner.Prepare(context.Background(), run, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := runner.Run(context.Background(), run, host, func(session.Progress) {})
	if err != nil || !outcome.CleanupDeferred || !outcome.RPCListening || outcome.CleanedUp {
		t.Fatalf("held proof: %+v %v", outcome, err)
	}
	outcome, err = runner.Finish(context.Background(), run)
	if err != nil || !outcome.CleanedUp {
		t.Fatalf("finish: %+v %v", outcome, err)
	}
}

func TestProtocolRunnerLeaseAndOwner(t *testing.T) {
	runner, run := protocolRunner(t, "waiting"), testRun()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runner.Prepare(ctx, run, func(session.Progress) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-start cancel: %v", err)
	}
	runner = protocolRunner(t, "verified")
	host, err := runner.Prepare(context.Background(), run, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	details, _ := engine.Pairing(host)
	if _, err := os.Stat(details.Workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Prepare(context.Background(), run, func(session.Progress) {}); err == nil {
		t.Fatal("concurrent session accepted")
	}
	support := filepath.Dir(filepath.Dir(details.Workspace))
	second := engine.NewProtocolRunner(support, func(session.Run, string) *exec.Cmd {
		t.Fatal("independent runner bypassed the active disk lease")
		return nil
	})
	if _, err := second.Prepare(context.Background(), run, func(session.Progress) {}); err == nil {
		t.Fatal("independent runner accepted an active lease")
	}
	if err := second.Cleanup(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if owner, err := os.ReadFile(filepath.Join(support, "session.lock")); err != nil || string(owner) != run.ID {
		t.Fatal("non-owning runner removed the active lease")
	}
	other := run
	other.ID = "not-owner"
	if err := runner.Cleanup(context.Background(), other); err == nil {
		t.Fatal("non-owner cleanup accepted")
	}
	if err := runner.Cleanup(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(details.Workspace); !os.IsNotExist(err) {
		t.Fatal("workspace retained")
	}
}
