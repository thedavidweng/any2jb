package session_test

import (
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
)

func TestChangesWakeWindowAfterBackgroundCompletion(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	gate := make(chan struct{})
	f.InspectGate = gate
	c := session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"})
	c.Inspect()
	<-c.Changes()
	close(gate)
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		if s := c.Snapshot(); s.Stage == session.Ready && !s.Busy {
			return
		}
		select {
		case <-c.Changes():
		case <-timer.C:
			t.Fatal("background completion did not notify the window")
		}
	}
}
