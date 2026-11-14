package transport_test

import (
	"context"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/session"
	guided "github.com/thedavidweng/any2jb/internal/transport"
)

func prompt(t *testing.T, m *guided.Manual) guided.Prompt {
	t.Helper()
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		if p := m.Prompt(); p.Kind != "" {
			return p
		}
		select {
		case <-m.Changes():
		case <-timer.C:
			t.Fatal("manual prompt did not arrive")
		}
	}
}

func TestManualSetupRequiresEachConsoleAcknowledgement(t *testing.T) {
	m := guided.NewManual(func(session.Run) (string, error) { return "PRINT 1\n", nil })
	run := session.Run{
		ID: "setup", Edition: "full", Profile: session.Profile{SaveName: "NS2"},
		Transport: session.Transport{Mode: "manual", Capabilities: session.Capabilities{Manual: true}},
	}
	done := make(chan error, 1)
	go func() {
		done <- m.Send(context.Background(), run, session.Bootstrap, session.PreparedHost{}, func(session.Progress) {})
	}()
	for _, want := range []struct{ kind, text string }{{"slot", ""}, {"bootstrap", "PRINT 1\n"}, {"direct", ""}} {
		p := prompt(t, m)
		if p.Kind != want.kind || p.Text != want.text {
			t.Fatalf("unexpected prompt: %+v", p)
		}
		if m.Acknowledge("stale-run", p.Sequence) {
			t.Fatal("stale acknowledgement accepted")
		}
		if !m.Acknowledge(run.ID, p.Sequence) {
			t.Fatal("current acknowledgement rejected")
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	go func() {
		done <- m.Send(context.Background(), run, session.Save, session.PreparedHost{}, func(session.Progress) {})
	}()
	p := prompt(t, m)
	if p.Text != `SAVE "NS2"` {
		t.Fatalf("wrong upstream full SAVE: %q", p.Text)
	}
	m.Acknowledge(run.ID, p.Sequence)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if m.Prompt().Kind != "" {
		t.Fatal("completed prompt retained")
	}
}
