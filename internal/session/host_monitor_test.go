package session_test

import (
	"testing"

	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
)

func TestRetainedHostExitInvalidatesLease(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	exited := make(chan struct{})
	f.HostExitGate = exited
	c := ready(t, f, true)
	if _, err := c.Start(); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.RetainedRPC)
	close(exited)
	s := await(t, c, session.Indeterminate)
	if s.Recovery != session.RestartConsole {
		t.Fatal("host crash left a usable lease")
	}
	if _, err := c.RunTool("anything"); err == nil {
		t.Fatal("tool accepted after host exit")
	}
}

func TestHostLossClearsTheVerifiedOutcome(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	exited := make(chan struct{})
	f.HostExitGate = exited
	c := ready(t, f, true)
	if _, err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if s := await(t, c, session.RetainedRPC); !s.Outcome.Verified {
		t.Fatalf("held session was not verified: %+v", s.Outcome)
	}
	close(exited)
	s := await(t, c, session.Indeterminate)
	if s.Recovery != session.RestartConsole || s.Outcome.Verified {
		t.Fatalf("console loss kept a verified outcome: %+v", s)
	}
}
