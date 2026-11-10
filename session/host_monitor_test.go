package session_test

import (
	"testing"

	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
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
