package main

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/launcher"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/savekit"
	"github.com/thedavidweng/any2jb/session"
)

type fakeToolAdapter struct {
	reasons  map[string]string
	state    retained.State
	selected string
	preview  *savekit.Preview
	prepared launcher.Request
}

func (f *fakeToolAdapter) Unavailable(tool, _ string) string { return f.reasons[tool] }
func (f *fakeToolAdapter) State() retained.State             { return f.state }
func (f *fakeToolAdapter) Select(titleID string) error       { f.selected = titleID; return nil }

func (f *fakeToolAdapter) PrepareLauncher(_ context.Context, request launcher.Request, _ savekit.Request) (string, savekit.Preview, error) {
	f.prepared = request
	if f.preview == nil {
		return "", savekit.Preview{}, errors.New("no prepared launcher package")
	}
	return "/tmp/any2jb-launcher-preview/save", *f.preview, nil
}

func retainedApp(t *testing.T, f *session.FakeEnvironment, adapter *fakeToolAdapter, tools []string) *app {
	t.Helper()
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	a := &app{manifest: m, tools: adapter,
		controller: session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned",
			RetainsRPC: true, Tools: tools})}
	a.toolsPanel.open = true
	return a
}

func TestGatedToolsStateTheirRecordedReasonInsteadOfAnAction(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	ounce, err := m.DistributionFor("ouncecake")
	if err != nil {
		t.Fatal(err)
	}
	research, err := m.DistributionFor("browser-research")
	if err != nil {
		t.Fatal(err)
	}
	adapter := &fakeToolAdapter{reasons: map[string]string{
		"ouncecake-webdav": ounce.Reason, "ouncecake-ftp": ounce.Reason}}
	f := session.NewFakeEnvironment()
	a := retainedApp(t, f, adapter, []string{"rpc-status", "ouncecake-webdav", "ouncecake-ftp", "sd-browser"})
	tt := ui.NewTester(a.advancedToolsView, 720, 1400)
	if !tt.HasText("These tools are read-only. This build has no console write path: no save, mod, launcher or filesystem write is possible here.") {
		t.Fatal(tt.Texts())
	}
	if !tt.HasText(ounce.Reason) || !tt.HasText(research.Reason) {
		t.Fatalf("missing recorded reasons: %q", tt.Texts())
	}
	for _, label := range []string{"Start read-only WebDAV export", "Start read-only FTP export on this Mac", "Check live RPC status"} {
		control, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok || !control.Disabled {
			t.Fatalf("%s is usable without a retained lease: %+v", label, control)
		}
	}
	// A tool outside the verified profile states that, not a distribution reason.
	if !tt.HasText("This host profile does not enable this tool.") {
		t.Fatal(tt.Texts())
	}
	if !tt.HasText("Advanced Tools need a verified session with a live retained RPC lease, or a connection to an existing one.") {
		t.Fatal(tt.Texts())
	}
}

func TestOwnedLeaseEnablesOnlyOneToolAtATime(t *testing.T) {
	f := session.NewFakeEnvironment()
	adapter := &fakeToolAdapter{}
	a := retainedApp(t, f, adapter, []string{"rpc-status", "system-code-dump", "sd-browser"})
	tt := ui.NewTester(a.advancedToolsView, 720, 1400)
	if _, err := a.controller.Inspect(); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	if _, err := a.controller.Start(); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.RetainedRPC)
	if !tt.HasText("This app owns the verified retained session.") {
		t.Fatal(tt.Texts())
	}
	status, ok := tt.AccessByRole(ui.RoleButton, "Check live RPC status")
	if !ok || status.Disabled {
		t.Fatalf("a verified lease did not enable the status check: %+v", status)
	}
	adapter.state = retained.State{Tool: "sd-browser", Endpoint: "ftp://192.0.2.7:2122/sd/"}
	tt.Frame()
	if !tt.HasText("Read-only endpoint: ftp://192.0.2.7:2122/sd/") {
		t.Fatal(tt.Texts())
	}
	for _, label := range []string{"Check live RPC status", "Dump system code"} {
		control, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok || !control.Disabled {
			t.Fatalf("%s started while another tool owns the session: %+v", label, control)
		}
	}
	if !tt.HasText("sd-browser owns the retained session. Stop it before starting another tool.") {
		t.Fatal(tt.Texts())
	}
	stop, ok := tt.AccessByRole(ui.RoleButton, "Stop the running tool")
	if !ok || stop.Disabled {
		t.Fatalf("a running tool cannot be stopped: %+v", stop)
	}
	adapter.state = retained.State{Lost: true}
	tt.Frame()
	if !tt.HasText("The retained session was lost. It is never reconnected automatically; restart the console before another attempt.") {
		t.Fatal(tt.Texts())
	}
}

func TestConnectingToAnExistingSessionIsNotALaunch(t *testing.T) {
	f := session.NewFakeEnvironment()
	f.RetainedSessions = []session.RetainedSession{{ID: "run-abc", Description: "Verified retained session run-abc"}}
	a := retainedApp(t, f, &fakeToolAdapter{}, []string{"rpc-status"})
	a.toolsPanel.sessions = f.RetainedSessions
	a.toolsPanel.chosen = "run-abc"
	tt := ui.NewTester(a.advancedToolsView, 720, 1400)
	if !tt.HasText("Connecting attaches to a session that is already verified. It does not retype commands or rebuild the exploit.") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("Connect to the existing session"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.RetainedRPC)
	if !tt.HasText("Connected to a verified session another app owns. Only that app can finish it.") {
		t.Fatal(tt.Texts())
	}
	trace := f.Trace()
	if slices.Contains(trace, "host.prepare") || slices.Contains(trace, "input.launch") {
		t.Fatalf("connecting rebuilt the exploit: %v", trace)
	}
	if !tt.HasText("Disconnecting releases this app's own resources. The original host keeps running on the console.") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("Disconnect without stopping the host"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Idle)
	if slices.Contains(f.Trace(), "host.cleanup") {
		t.Fatalf("disconnecting stopped the original host: %v", f.Trace())
	}
}

func TestDiscoveryRunsOffTheUIThread(t *testing.T) {
	f := session.NewFakeEnvironment()
	f.RetainedSessions = []session.RetainedSession{{ID: "run-abc"}}
	a := retainedApp(t, f, &fakeToolAdapter{}, []string{"rpc-status"})
	tt := ui.NewTester(a.advancedToolsView, 720, 1400)
	if find, ok := tt.AccessByRole(ui.RoleButton, "Find verified sessions on this Mac"); !ok || !find.Disabled {
		t.Fatalf("discovery ran without a main-thread publisher: %+v", find)
	}
	done := make(chan struct{})
	a.toolsPanel.update = func(apply func()) { apply(); close(done) }
	a.discoverRetained()
	<-done
	if len(a.toolsPanel.sessions) != 1 || a.toolsPanel.chosen != "run-abc" {
		t.Fatalf("discovery did not publish its result: %+v", a.toolsPanel)
	}
	tt.Frame()
	if !tt.HasText("run-abc") {
		t.Fatal(tt.Texts())
	}
}
