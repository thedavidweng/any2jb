package session

import (
	"context"
	"errors"
	"testing"
)

type recoveryPlatform struct {
	PlatformServices
	releaseError error
	calls        int
}

func (p *recoveryPlatform) ReleaseInterrupted(context.Context) error {
	p.calls++
	return p.releaseError
}

func TestConsoleRestartRequiresConfirmationAndLeaseRelease(t *testing.T) {
	for _, releaseError := range []error{nil, errors.New("session owner is still alive")} {
		env := NewFakeEnvironment().Environment()
		platform := &recoveryPlatform{PlatformServices: env.Platform, releaseError: releaseError}
		env.Platform = platform
		c := New(env, Profile{ID: "pinned"})
		c.MarkInterrupted()
		if _, err := c.ConfirmConsoleRestart(context.Background(), false); err == nil || platform.calls != 0 {
			t.Fatal("recovery without explicit console restart confirmation")
		}
		s, err := c.ConfirmConsoleRestart(context.Background(), true)
		if platform.calls != 1 {
			t.Fatal("stale lease release was skipped")
		}
		if releaseError != nil {
			if !errors.Is(err, releaseError) || s.Stage != Indeterminate || s.Recovery != RestartConsole {
				t.Fatalf("unsafe recovery: %+v %v", s, err)
			}
		} else if err != nil || s.Stage != Idle || s.SideEffects || s.Recovery != NoRecovery ||
			s.Outcome.Verified || len(s.Checks) != 0 || s.CanStart() {
			t.Fatalf("recovery reused stale readiness or verification: %+v %v", s, err)
		}
	}
}

func TestRelaunchNeverAdoptsStaleVerification(t *testing.T) {
	c := New(NewFakeEnvironment().Environment(), Profile{ID: "pinned"})
	s, err := c.MarkInterrupted()
	if err != nil {
		t.Fatal(err)
	}
	if s.Stage != Indeterminate || s.Recovery != RestartConsole || s.Outcome.Verified || s.CanStart() {
		t.Fatal(s)
	}
	if _, err := c.Start(); err == nil {
		t.Fatal("stale session launched")
	}
	if _, err := c.Inspect(); err == nil {
		t.Fatal("readiness bypassed console recovery")
	}
}
