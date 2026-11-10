package main

import (
	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/diagnostics"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
	"testing"
)

func TestDiagnosticsSourcesFollowAndCleanupPreview(t *testing.T) {
	a := &app{controller: session.New(sessiontest.NewFakeEnvironment().Environment(), session.Profile{ID: "test"}), diagnostic: diagnosticUI{ready: true, logs: diagnostics.Logs{Host: "host failure", USB: "typing interrupted", Pipeline: "pipeline failure", Acquisition: "acquisition failed"}}}
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
