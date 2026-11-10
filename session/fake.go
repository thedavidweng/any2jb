package session

import (
	"context"
	"errors"
	"sync"
)

// FakeEnvironment is only injected explicitly by tests/developer previews.
// Configure outcomes before constructing the controller; do not mutate while running.
type FakeEnvironment struct {
	mu              sync.Mutex
	trace           []string
	Prepared        PreparedHost
	PrepareProgress Progress
	InspectGate     <-chan struct{}
	Checks          []Check
	Outcome         Outcome
	FinishOutcome   Outcome
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
}

func NewFakeEnvironment() *FakeEnvironment {
	return &FakeEnvironment{
		Prepared: PreparedHost{Listening: true, PairingValid: true},
		Checks:   []Check{{ID: "simulated", State: CheckVerified, Message: "Simulated readiness, not a real hardware check."}},
		Outcome: Outcome{Verified: true, FSVerified: true, Restored: true, CleanedUp: true,
			RetainedReady: true, CleanupDeferred: true, DonorCleanedUp: true, RPCListening: true},
		FinishOutcome: Outcome{Verified: true, FSVerified: true, Restored: true, CleanedUp: true},
	}
}
func (f *FakeEnvironment) Environment() Environment {
	return Environment{Host: fakeHost{f}, Input: fakeInput{f}, Platform: fakePlatform{f}, Tools: fakeTools{f}, Simulated: true}
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

func (h fakeHost) Prepare(ctx context.Context, _ Run, report Report) (PreparedHost, error) {
	h.f.record("host.prepare")
	report(h.f.PrepareProgress)
	if err := gate(ctx, h.f.PrepareGate); err != nil {
		return PreparedHost{}, err
	}
	h.f.record("host.prepared")
	return h.f.Prepared, h.f.PrepareError
}
func (h fakeHost) Run(context.Context, Run, PreparedHost, Report) (Outcome, error) {
	h.f.record("host.run")
	return h.f.Outcome, h.f.RunError
}
func (h fakeHost) Wait(ctx context.Context, _ Run) error {
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
func (h fakeHost) Finish(context.Context, Run) (Outcome, error) {
	h.f.record("host.finish")
	return h.f.FinishOutcome, nil
}
func (h fakeHost) Cleanup(context.Context, Run) error {
	h.f.record("host.cleanup")
	return h.f.CleanupError
}

type fakeInput struct{ f *FakeEnvironment }

func (i fakeInput) Send(ctx context.Context, _ Run, action InputAction, _ PreparedHost, report Report) error {
	i.f.record("input." + string(action))
	report(Progress{SideEffect: true})
	if err := gate(ctx, i.f.InputGate); err != nil {
		return err
	}
	return i.f.InputError
}
func (i fakeInput) Release(context.Context, Run) error {
	i.f.record("input.release")
	return i.f.CleanupError
}

type fakePlatform struct{ f *FakeEnvironment }

func (p fakePlatform) Inspect(ctx context.Context, _ Run) ([]Check, error) {
	p.f.record("platform.inspect")
	if err := gate(ctx, p.f.InspectGate); err != nil {
		return nil, err
	}
	return p.f.Checks, p.f.InspectError
}
func (p fakePlatform) ExportDiagnostics(context.Context, Snapshot) (string, error) {
	return "simulated diagnostics; no real check performed", nil
}
func (p fakePlatform) Credentials() CredentialStore             { return fakeCredentials{} }
func (p fakePlatform) ReleaseInterrupted(context.Context) error { return nil }

type fakeCredentials struct{}

func (fakeCredentials) Get(context.Context, string) ([]byte, error) {
	return nil, errors.New("no simulated credential")
}
func (fakeCredentials) Set(context.Context, string, []byte) error {
	return errors.New("fake environment does not store secrets")
}
func (fakeCredentials) Delete(context.Context, string) error { return nil }

type fakeTools struct{ f *FakeEnvironment }

func (t fakeTools) Run(ctx context.Context, _ Run, _ string, _ Report) error {
	if err := gate(ctx, t.f.ToolGate); err != nil {
		return err
	}
	return t.f.ToolError
}
func (t fakeTools) Stop(context.Context, Run) error { return t.f.CleanupError }
