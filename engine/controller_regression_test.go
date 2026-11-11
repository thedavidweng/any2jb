package engine_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

// These are simulated engine process tests, not upstream exploit evidence.
func TestSimulatedEngineControllerVerifiedFinish(t *testing.T) {
	for _, held := range []bool{false, true} {
		scenario := "verified"
		if held {
			scenario = "held-verified"
		}
		t.Run(scenario, func(t *testing.T) {
			c, input, support := simulatedController(t, scenario, held)
			if _, err := c.Start(); err != nil {
				t.Fatal(err)
			}
			want := session.Verified
			if held {
				want = session.RetainedRPC
			}
			s := awaitSnapshot(t, c, func(s session.Snapshot) bool { return s.Stage == want && !s.Busy })
			pairing := <-input.pairings
			if pairing.RunID != s.RunID || pairing.Command != "V=START(39380,192,0,2,1)" {
				t.Fatalf("input did not receive the fresh pairing: %+v", pairing)
			}
			if s.Current != 4 || s.Total != 4 ||
				!s.Outcome.Verified || !s.Outcome.FSVerified || !s.Outcome.Restored {
				t.Fatalf("incomplete simulated verification: %+v", s)
			}
			if held && (!s.Outcome.RPCListening || !s.Outcome.CleanupDeferred || s.Outcome.CleanedUp) {
				t.Fatalf("held lease was not proven live/deferred: %+v", s.Outcome)
			}
			if !held && !s.Outcome.CleanedUp {
				t.Fatal("cleanup missing from verified outcome")
			}
			if _, err := os.Stat(filepath.Join(support, "session.lock")); err != nil {
				t.Fatalf("active session lost its lease: %v", err)
			}
			if _, err := c.Finish(); err != nil {
				t.Fatal(err)
			}
			done := awaitSnapshot(t, c, func(s session.Snapshot) bool { return s.Stage == session.Finished && !s.Busy })
			if !done.Outcome.CleanedUp || done.SideEffects || len(done.Processes) != 0 {
				t.Fatalf("Finish did not release the run: %+v", done)
			}
			assertRunRemoved(t, support, pairing.Workspace)
			if !slices.Contains(input.environment.Trace(), "input.release") {
				t.Fatal("Finish omitted input release")
			}
		})
	}
}

func TestSimulatedEngineControllerIndeterminateCleanup(t *testing.T) {
	for _, scenario := range []string{"disconnect", "partial", "cleanup-error"} {
		t.Run(scenario, func(t *testing.T) {
			c, input, support := simulatedController(t, scenario, false)
			if _, err := c.Start(); err != nil {
				t.Fatal(err)
			}
			s := awaitSnapshot(t, c, func(s session.Snapshot) bool {
				return s.Stage == session.Indeterminate && !s.Busy
			})
			if s.Recovery != session.RestartConsole || !s.SideEffects || len(s.Processes) != 0 {
				t.Fatalf("partial run lost restart/cleanup requirements: %+v", s)
			}
			select {
			case pairing := <-input.pairings:
				assertRunRemoved(t, support, pairing.Workspace)
			default:
				t.Fatal("fixture did not pass pairing to input before disconnect")
			}
			for _, event := range c.Events(0) {
				if event.Stage == session.Verified || event.Stage == session.RetainedRPC {
					t.Fatal("partial result produced a verified event")
				}
				if strings.Contains(event.Message, "V=START") {
					t.Fatal("pairing leaked into public events")
				}
			}
			if _, err := c.Start(); err == nil {
				t.Fatal("indeterminate run allowed an automatic retry")
			}
			trace := input.environment.Trace()
			if slices.Index(trace, "input.launch") == -1 || slices.Index(trace, "input.release") == -1 {
				t.Fatalf("partial run omitted input/release: %v", trace)
			}
		})
	}
}

type pairingInput struct {
	session.InputTransport
	environment *sessiontest.FakeEnvironment
	pairings    chan engine.PairingDetails
}

func (i *pairingInput) Send(ctx context.Context, run session.Run, action session.InputAction, host session.PreparedHost, report session.Report) error {
	details, err := engine.Pairing(host)
	if err != nil {
		return err
	}
	if action != session.Launch || details.RunID != run.ID || details.Workspace == "" {
		return fmt.Errorf("input received invalid run-bound pairing")
	}
	i.pairings <- details
	return i.InputTransport.Send(ctx, run, action, host, report)
}

func simulatedController(t *testing.T, scenario string, held bool) (*session.SessionController, *pairingInput, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("simulated engine requires process-tree supervision; Windows launch is deliberately unsupported")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("simulated engine requires Node for engine/testdata/fake-engine.cjs")
	}
	fixture, err := filepath.Abs("testdata/fake-engine.cjs")
	if err != nil {
		t.Fatal(err)
	}
	support := t.TempDir()
	runner := engine.NewProtocolRunner(support, func(run session.Run, workspace string) *exec.Cmd {
		return exec.Command(node, fixture, scenario, run.ID, workspace)
	})
	fake := sessiontest.NewFakeEnvironment()
	env := fake.Environment()
	input := &pairingInput{InputTransport: env.Input, environment: fake, pairings: make(chan engine.PairingDetails, 1)}
	env.Host, env.Input = runner, input
	profile := testRun().Profile
	profile.Revision, profile.RetainsRPC = "simulated-engine", held
	c := session.New(env, profile)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := runner.Cleanup(ctx, session.Run{ID: c.Snapshot().RunID}); err != nil {
			t.Errorf("fixture teardown: %v", err)
		}
	})
	if _, err := c.ConfigureTransport(session.Transport{Mode: "manual", Capabilities: session.Capabilities{Manual: true}}, "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Inspect(); err != nil {
		t.Fatal(err)
	}
	awaitSnapshot(t, c, func(s session.Snapshot) bool { return s.Stage == session.Ready && !s.Busy })
	return c, input, support
}

func assertRunRemoved(t *testing.T, support, workspace string) {
	t.Helper()
	for _, name := range []string{workspace, filepath.Join(support, "session.lock")} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatalf("owned run artifact remains: %s (%v)", name, err)
		}
	}
}
