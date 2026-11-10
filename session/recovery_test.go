package session

import "testing"

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
