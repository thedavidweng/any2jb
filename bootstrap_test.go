package main

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func TestReloadedBootstrapStartsWithoutRepeatingSetupAfterFreshReadiness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	a, _ := loadApp(path)
	a.preferences.Transport = "keyboard"
	if err := a.savePreferences(); err != nil {
		t.Fatal(err)
	}
	a.initializeSession(sessiontest.NewFakeEnvironment().Environment())
	if err := a.confirmExistingBootstrap(); err != nil {
		t.Fatal(err)
	}
	b, _ := loadApp(path)
	f := sessiontest.NewFakeEnvironment()
	if err := b.initializeSession(f.Environment()); err != nil {
		t.Fatal(err)
	}
	c := b.controller
	if c.Snapshot().CanStart() {
		t.Fatal("saved record bypassed readiness")
	}
	c.Inspect()
	waitBootstrap(t, c)
	if !c.Snapshot().CanStart() {
		t.Fatal("matching record plus fresh verified readiness refused")
	}
	if _, err := c.Start(); err != nil {
		t.Fatal(err)
	}
	waitBootstrap(t, c)
	if c.Snapshot().Stage != session.RetainedRPC {
		t.Fatal(c.Snapshot())
	}
	trace := f.Trace()
	if slices.Contains(trace, "input.bootstrap") || slices.Contains(trace, "input.save") ||
		slices.Index(trace, "host.prepared") >= slices.Index(trace, "input.launch") {
		t.Fatal("saved launch repeated setup or input preceded listening and pairing", trace)
	}
	c.Finish()
	waitBootstrap(t, c)
}

func TestConfirmedBootstrapSurvivesReloadWithoutRestoringReadiness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	a, err := loadApp(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.initializeSession(sessiontest.NewFakeEnvironment().Environment()); err != nil {
		t.Fatal(err)
	}
	if err := a.confirmExistingBootstrap(); err != nil {
		t.Fatal(err)
	}
	record := a.controller.Snapshot().SavedBootstrap
	if !record.Confirmed || record.SavedAt.IsZero() || record.SaveName != "NS2" {
		t.Fatal(record)
	}
	b, err := loadApp(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.initializeSession(sessiontest.NewFakeEnvironment().Environment()); err != nil {
		t.Fatal(err)
	}
	if s := b.controller.Snapshot(); !s.BootstrapSaved || s.CanStart() || len(s.Checks) != 0 {
		t.Fatalf("saved identity or fresh readiness lost: %+v", s)
	}
	b.preferences.Profile = "pepsipu"
	if err := b.applyPreferences(); err != nil {
		t.Fatal(err)
	}
	if b.controller.Snapshot().BootstrapSaved {
		t.Fatal("profile change reused another host's identity")
	}
	c, err := loadApp(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.initializeSession(sessiontest.NewFakeEnvironment().Environment()); err != nil {
		t.Fatal(err)
	}
	if c.controller.Snapshot().BootstrapSaved {
		t.Fatal("relaunch reused mismatched identity")
	}
}

func TestOnlyCompletedSaveProducesBootstrapRecord(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.initializeSession(sessiontest.NewFakeEnvironment().Environment()); err != nil {
		t.Fatal(err)
	}
	c := a.controller
	c.Inspect()
	waitBootstrap(t, c)
	c.PrepareBootstrap()
	waitBootstrap(t, c)
	c.SaveBootstrap(false)
	waitBootstrap(t, c)
	if c.Snapshot().SavedBootstrap.Confirmed {
		t.Fatal("SAVE submission recorded success")
	}
	c.SaveBootstrap(true)
	waitBootstrap(t, c)
	record := c.Snapshot().SavedBootstrap
	if !record.Confirmed || record.SavedAt.IsZero() {
		t.Fatal("completed setup has no identity", record)
	}
	if err := saveBootstrapRecord(a.bootstrapPath(), record); err != nil {
		t.Fatal(err)
	}
	b, _ := loadApp(a.preferencesPath)
	if err := b.initializeSession(sessiontest.NewFakeEnvironment().Environment()); err != nil {
		t.Fatal(err)
	}
	if !b.controller.Snapshot().BootstrapSaved {
		t.Fatal("completed setup was not restored")
	}
}

func waitBootstrap(t *testing.T, c *session.SessionController) {
	t.Helper()
	timeout := time.After(time.Minute)
	for c.Snapshot().Busy {
		select {
		case <-c.Changes():
		case <-timeout:
			t.Fatal("bootstrap operation hung")
		}
	}
}
