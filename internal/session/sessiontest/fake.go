// Package sessiontest provides the deterministic session test double. It is
// linked only into test binaries; production builds never import it.
package sessiontest

import (
	"context"
	"errors"
	"sync"

	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

// FakeEnvironment is only injected explicitly by tests.
// Configure outcomes before constructing the controller; do not mutate while running.
type FakeEnvironment struct {
	mu              sync.Mutex
	trace           []string
	Prepared        session.PreparedHost
	PrepareProgress session.Progress
	InspectGate     <-chan struct{}
	Checks          []session.Check
	Outcome         session.Outcome
	FinishOutcome   session.Outcome
	PrepareGate     <-chan struct{}
	InputGate       <-chan struct{}
	ToolGate        <-chan struct{}
	InspectError    error
	PrepareError    error
	InputError      error
	RunError        error
	CleanupError    error
	ToolError       error
	HostExitGate    <-chan struct{}

	RetainedSessions []session.RetainedSession
	AttachOutcome    session.Outcome
	AttachError      error
	DetachError      error
	RetainedLostGate <-chan struct{}
	ToolRequest      session.ToolRequest
	ToolValidator    func(session.ToolRequest) error
}

func NewFakeEnvironment() *FakeEnvironment {
	sim := locales.Key("dynFakeSimulatedReadiness")
	return &FakeEnvironment{
		Prepared: session.PreparedHost{Listening: true, PairingValid: true},
		Checks: []session.Check{
			{ID: "simulated", State: session.CheckVerified, Message: sim.English(), MessageText: sim},
			{ID: session.CheckRuntime, State: session.CheckVerified, Message: sim.English(), MessageText: sim},
			{ID: session.CheckEngine, State: session.CheckVerified, Message: sim.English(), MessageText: sim},
		},
		Outcome: session.Outcome{Verified: true, FSVerified: true, Restored: true, CleanedUp: true,
			RetainedReady: true, CleanupDeferred: true, DonorCleanedUp: true, RPCListening: true},
		FinishOutcome: session.Outcome{Verified: true, FSVerified: true, Restored: true, CleanedUp: true},
	}
}
func (f *FakeEnvironment) Environment() session.Environment {
	return session.Environment{Host: fakeHost{f}, Retained: fakeRetained{f}, Input: fakeInput{f},
		Platform: fakePlatform{f}, Tools: fakeTools{f}}
}
func (f *FakeEnvironment) record(action string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trace = append(f.trace, action)
}
func (f *FakeEnvironment) Trace() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.trace...)
}
func gate(ctx context.Context, ch <-chan struct{}) error {
	if ch == nil {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return ctx.Err()
	}
}

type fakeHost struct{ f *FakeEnvironment }

func (h fakeHost) Prepare(ctx context.Context, _ session.Run, report session.Report) (session.PreparedHost, error) {
	h.f.record("host.prepare")
	report(h.f.PrepareProgress)
	if err := gate(ctx, h.f.PrepareGate); err != nil {
		return session.PreparedHost{}, err
	}
	h.f.record("host.prepared")
	return h.f.Prepared, h.f.PrepareError
}
func (h fakeHost) Run(context.Context, session.Run, session.PreparedHost, session.Report) (session.Outcome, error) {
	h.f.record("host.run")
	return h.f.Outcome, h.f.RunError
}
func (h fakeHost) Wait(ctx context.Context, _ session.Run) error {
	if err := gate(ctx, h.f.HostExitGate); err != nil {
		return err
	}
	// A default fake host remains live until its watcher is cancelled.
	if h.f.HostExitGate == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	return errors.New("simulated host exited")
}
func (h fakeHost) Finish(context.Context, session.Run) (session.Outcome, error) {
	h.f.record("host.finish")
	return h.f.FinishOutcome, nil
}
func (h fakeHost) Cleanup(context.Context, session.Run) error {
	h.f.record("host.cleanup")
	return h.f.CleanupError
}

type fakeInput struct{ f *FakeEnvironment }

func (i fakeInput) Send(ctx context.Context, _ session.Run, action session.InputAction, _ session.PreparedHost, report session.Report) error {
	i.f.record("input." + string(action))
	report(session.Progress{SideEffect: true})
	if err := gate(ctx, i.f.InputGate); err != nil {
		return err
	}
	return i.f.InputError
}
func (i fakeInput) Release(context.Context, session.Run) error {
	i.f.record("input.release")
	return i.f.CleanupError
}

type fakePlatform struct{ f *FakeEnvironment }

func (p fakePlatform) Inspect(ctx context.Context, _ session.Run) ([]session.Check, error) {
	p.f.record("platform.inspect")
	if err := gate(ctx, p.f.InspectGate); err != nil {
		return nil, err
	}
	return p.f.Checks, p.f.InspectError
}
func (p fakePlatform) ExportDiagnostics(context.Context, session.Snapshot) (string, error) {
	return "simulated diagnostics; no real check performed", nil
}
func (p fakePlatform) ReleaseInterrupted(context.Context) error { return nil }

type fakeRetained struct{ f *FakeEnvironment }

func (r fakeRetained) Discover(context.Context) ([]session.RetainedSession, error) {
	r.f.record("retained.discover")
	return r.f.RetainedSessions, nil
}
func (r fakeRetained) Attach(_ context.Context, _ session.Run, id string) (session.Outcome, error) {
	r.f.record("retained.attach")
	for _, s := range r.f.RetainedSessions {
		if s.ID != id {
			continue
		}
		if r.f.AttachOutcome != (session.Outcome{}) {
			return r.f.AttachOutcome, r.f.AttachError
		}
		return r.f.Outcome, r.f.AttachError
	}
	return session.Outcome{}, errors.New("no simulated session with that identity")
}
func (r fakeRetained) Detach(context.Context, session.Run) error {
	r.f.record("retained.detach")
	return r.f.DetachError
}
func (r fakeRetained) Watch(ctx context.Context, _ session.Run) error {
	if err := gate(ctx, r.f.RetainedLostGate); err != nil {
		return err
	}
	if r.f.RetainedLostGate == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	return errors.New("simulated attachment lost")
}

type fakeTools struct{ f *FakeEnvironment }

func (t fakeTools) Validate(request session.ToolRequest) error {
	if t.f.ToolValidator != nil {
		return t.f.ToolValidator(request)
	}
	return nil
}
func (t fakeTools) Run(ctx context.Context, _ session.Run, _ string, _ session.Report) error {
	if err := gate(ctx, t.f.ToolGate); err != nil {
		return err
	}
	return t.f.ToolError
}
func (t fakeTools) RunWith(ctx context.Context, run session.Run, request session.ToolRequest, report session.Report) error {
	t.f.mu.Lock()
	t.f.trace = append(t.f.trace, "tool.run-with "+request.Tool)
	t.f.ToolRequest = request
	t.f.mu.Unlock()
	return t.Run(ctx, run, request.Tool, report)
}
func (t fakeTools) Stop(context.Context, session.Run) error { return t.f.CleanupError }
