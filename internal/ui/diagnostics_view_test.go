package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
	appstate "github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/diagnostics"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
	"github.com/thedavidweng/any2jb/locales"
)

func TestDiagnosticsSourcesFollowAndCleanupPreview(t *testing.T) {
	a := &app{
		State: appstate.State{
			Controller: session.New(sessiontest.NewFakeEnvironment().Environment(), session.Profile{ID: "test"}),
		},
		diagnostic: diagnosticUI{ready: true, logs: diagnostics.Logs{Host: "host failure", USB: "typing interrupted", Pipeline: "pipeline failure", Acquisition: "acquisition failed"}},
	}
	tt := ui.NewTester(a.diagnosticsView, 640, 720)
	for _, label := range []string{"Host", "USB and typing", "Pipeline", "Acquisition", "Follow logs", "Preview cleanup"} {
		if !tt.HasText(label) {
			t.Fatal(label, tt.Texts())
		}
	}
	if !tt.HasText("host failure") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("Pipeline"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("pipeline failure") || tt.HasText("host failure") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("Follow logs"); err != nil {
		t.Fatal(err)
	}
	if !a.diagnostic.follow {
		t.Fatal("follow toggle did not bind")
	}
	a.diagnostic.plan = []string{"past-runs/run-old/run.log"}
	a.diagnostic.preview = true
	tt.Frame()
	for _, label := range []string{"past-runs/run-old/run.log", "Delete listed temporary files", "Keep files"} {
		if !tt.HasText(label) {
			t.Fatal(label, tt.Texts())
		}
	}
	if err := tt.Click("Keep files"); err != nil {
		t.Fatal(err)
	}
	if a.diagnostic.preview {
		t.Fatal("preview remained open")
	}
}

// A corner view names itself once, and Settings aligns its values with the
// stages' label column.
func TestCornerViewsShareTheStageHierarchy(t *testing.T) {
	a := newTestApp(t)
	a.diagnostic.ready = true
	a.flow.diagnostics = true
	tt := ui.NewTester(a.view, 960, 900)
	names := 0
	for _, text := range tt.Texts() {
		if text == locales.Diagnostics() {
			names++
		}
	}
	// The corner button and the page heading.
	if names != 2 {
		t.Fatalf("Diagnostics is named %d times: %v", names, tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleRadioGroup, locales.LogSource()); !ok {
		t.Fatal("log sources are not a named choice")
	}
	if host, ok := tt.AccessByRole(ui.RoleRadio, locales.LogHost()); !ok || !host.Checked {
		t.Fatal("the shown log source is not announced as chosen")
	}
	a.flow.diagnostics, a.flow.settings = false, true
	tt.Frame()
	p, _ := a.Manifest.Profile(a.Preferences.Profile)
	if tt.HasText(locales.HostProfile()+": "+p.Label) || !tt.HasText(locales.HostProfile()) || !tt.HasText(p.Label) {
		t.Fatalf("host profile is not a label and value: %v", tt.Texts())
	}
}
