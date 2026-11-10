package session_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/thedavidweng/any2jb/readiness"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

type observedPlatform struct {
	session.PlatformServices
	mu      sync.Mutex
	address string
}

func (p *observedPlatform) Inspect(ctx context.Context, run session.Run) ([]session.Check, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return (readiness.Inspector{Observe: func(context.Context) readiness.Observation {
		return readiness.Observation{Addresses: []readiness.Address{{Interface: "en0", IPv4: p.address}}}
	}}).Inspect(ctx, run)
}

func TestObservedLANSelectionAndTransportGates(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	env := f.Environment()
	p := &observedPlatform{PlatformServices: env.Platform, address: "192.168.1.10"}
	env.Platform = p
	c := session.New(env, session.Profile{ID: "test", Revision: "pinned"})
	c.Inspect()
	s := await(t, c, session.Ready)
	if s.LANIPv4 != "192.168.1.10" || s.LANOverride != "" || !s.CanStart() {
		t.Fatalf("%+v", s)
	}
	// A discovered address is not a persisted override. Refresh may choose a new assignment.
	p.mu.Lock()
	p.address = "10.0.0.4"
	p.mu.Unlock()
	c.Inspect()
	if s := await(t, c, session.Ready); s.LANIPv4 != "10.0.0.4" {
		t.Fatal(s)
	}
	// A stale assignment must not reach the host or console.
	p.mu.Lock()
	p.address = ""
	p.mu.Unlock()
	c.Start()
	await(t, c, session.Failed)
	if slices.Contains(f.Trace(), "host.prepare") {
		t.Fatal("host launched with stale IPv4")
	}
	// USB facts are required by built-in HID, not by guided manual input.
	p.mu.Lock()
	p.address = "192.168.1.10"
	p.mu.Unlock()
	c.ConfigureTransport(session.Transport{Mode: "keyboard", Capabilities: session.Capabilities{Keyboard: true}}, "")
	c.Inspect()
	if s := await(t, c, session.NeedsSetup); s.CanStart() {
		t.Fatal("unsupported USB launched")
	}
}
