package session_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/thedavidweng/any2jb/internal/readiness"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
)

type observedPlatform struct {
	session.PlatformServices
	mu      sync.Mutex
	address string
}

type observedLAN struct {
	session.PlatformServices
	mu          sync.Mutex
	observation readiness.Observation
}

func (p *observedLAN) Inspect(ctx context.Context, run session.Run) ([]session.Check, error) {
	p.mu.Lock()
	observation := p.observation
	p.mu.Unlock()
	return (readiness.Inspector{Observe: func(context.Context) readiness.Observation {
		return observation
	}}).Inspect(ctx, run)
}

type recordingHost struct {
	session.HostRunner
	mu  sync.Mutex
	run session.Run
}

func (h *recordingHost) Prepare(ctx context.Context, run session.Run, report session.Report) (session.PreparedHost, error) {
	h.mu.Lock()
	h.run = run
	h.mu.Unlock()
	return h.HostRunner.Prepare(ctx, run, report)
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

func TestLaunchReobservesTheLANAddressItVerifies(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	env := f.Environment()
	platform := &observedLAN{PlatformServices: env.Platform, observation: readiness.Observation{
		Addresses: []readiness.Address{{Interface: "en0", IPv4: "192.168.1.10"}}, DefaultInterface: "en0",
	}}
	env.Platform = platform
	host := &recordingHost{HostRunner: env.Host}
	env.Host = host
	c := session.New(env, session.Profile{ID: "test", Revision: "pinned"})
	if _, err := c.Inspect(); err != nil {
		t.Fatal(err)
	}
	if s := await(t, c, session.Ready); s.LANIPv4 != "192.168.1.10" {
		t.Fatalf("first inspection selected %q", s.LANIPv4)
	}
	// The default interface moved before launch: the host must listen on the
	// address the pre-launch inspection verified, not the earlier selection.
	platform.mu.Lock()
	platform.observation = readiness.Observation{
		Addresses:        []readiness.Address{{Interface: "en0", IPv4: "192.168.1.10"}, {Interface: "en1", IPv4: "10.0.0.4"}},
		DefaultInterface: "en1",
	}
	platform.mu.Unlock()
	if _, err := c.Start(); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.Verified)
	host.mu.Lock()
	prepared := host.run.LANIPv4
	host.mu.Unlock()
	if prepared != "10.0.0.4" || s.LANIPv4 != "10.0.0.4" {
		t.Fatalf("launch used a stale address: prepared %q, snapshot %q", prepared, s.LANIPv4)
	}
}
