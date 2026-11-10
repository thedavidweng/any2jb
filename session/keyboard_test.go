package session_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

type keyboardFixture struct {
	mu      sync.Mutex
	reports [][8]byte
	closed  bool
	run     context.Context
	// cancelledFirst records a run cancelled before the neutral release.
	cancelledFirst bool
}

func (*keyboardFixture) Send(context.Context, session.Run, session.InputAction, session.PreparedHost, session.Report) error {
	return nil
}
func (*keyboardFixture) Release(context.Context, session.Run) error { return nil }
func (*keyboardFixture) SelectedKeyboardController() string         { return "usb-drd0" }
func (k *keyboardFixture) OpenKeyboardSession(ctx context.Context, _ session.Run, _ session.Report) (session.Keyboard, error) {
	k.mu.Lock()
	k.run = ctx
	k.mu.Unlock()
	return keyboardLease{k}, nil
}

type keyboardLease struct{ k *keyboardFixture }

func (k keyboardLease) Send(_ context.Context, r [8]byte) error {
	k.k.mu.Lock()
	defer k.k.mu.Unlock()
	k.k.reports = append(k.k.reports, r)
	return nil
}
func (k keyboardLease) Close(context.Context) error {
	k.k.mu.Lock()
	defer k.k.mu.Unlock()
	if !k.k.closed {
		k.k.cancelledFirst = k.k.run.Err() != nil
		k.k.reports = append(k.k.reports, [8]byte{})
		k.k.closed = true
	}
	return nil
}

func TestLiveKeyboardIsControllerOwnedAndPartialCloseRequiresInspection(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	f.Checks = append(f.Checks, session.Check{ID: "usb-drd0", State: session.Available, GatesTransport: "usb-hid"})
	env := f.Environment()
	input := &keyboardFixture{}
	env.Input = input
	c := session.New(env, session.Profile{ID: "fixture", Revision: "pinned"})
	c.ConfigureTransport(session.Transport{Mode: "keyboard", Capabilities: session.Capabilities{Keyboard: true}}, "192.0.2.1")
	c.Inspect()
	await(t, c, session.NeedsSetup)
	if _, err := c.StartKeyboard(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c.Snapshot().Busy {
			c.CloseKeyboard()
			await(t, c, session.Indeterminate)
		}
	})
	deadline := time.After(3 * time.Second)
	for !c.Snapshot().KeyboardOpen {
		select {
		case <-deadline:
			t.Fatal("keyboard did not open")
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := c.StartKeyboard(); err == nil {
		t.Fatal("duplicate keyboard started")
	}
	if _, err := c.ForwardKeyboardReport([8]byte{0, 0, 4}); err != nil {
		t.Fatal(err)
	}
	for {
		input.mu.Lock()
		count := len(input.reports)
		input.mu.Unlock()
		if count == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("key was not forwarded")
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := c.CloseKeyboard(); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.Indeterminate)
	input.mu.Lock()
	defer input.mu.Unlock()
	if !input.closed || len(input.reports) != 2 || input.reports[1] != [8]byte{} {
		t.Fatal("controller reported inspection state before neutral close")
	}
	if input.cancelledFirst {
		t.Fatal("run was cancelled before the neutral release")
	}
}
