package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/thedavidweng/any2jb/civswitch"
	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

func TestProductionStartupUsesRealBlockedAdapters(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	env := productionEnvironmentWithCivSwitch(
		engine.Options{Assets: t.TempDir(), Support: t.TempDir(), Version: "fixture", Manifest: a.manifest},
		transport.DarwinOptions{Timing: transport.DefaultTiming()}, civswitch.Options{})
	if _, ok := env.Host.(*engine.HostRunner); !ok {
		t.Fatal("production did not use the real host runner")
	}
	inputs, ok := env.Input.(*transport.Dispatcher)
	if !ok || inputs.Manual() == nil || inputs.USB() == nil {
		t.Fatal("production did not compose both input adapters")
	}
	if err := a.initializeSession(env); err != nil {
		t.Fatal(err)
	}
	if a.manual != inputs.Manual() || a.keyboard.input != inputs.USB() {
		t.Fatal("native panels lost access to the composed adapters")
	}
	checks, err := env.Platform.Inspect(context.Background(), session.Run{Profile: a.controller.Snapshot().Profile})
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]session.Check{}
	for _, check := range checks {
		found[check.ID] = check
		if (check.ID == "runtime" || check.ID == "engine" || check.ID == "input-adapter") && check.State != session.RequiresAttention {
			t.Fatalf("unavailable prerequisite %s marked ready", check.ID)
		}
	}
	if _, ok := found["network"]; !ok {
		t.Fatal("real readiness adapter was not composed")
	}
	if usb, ok := found["usb-runtime"]; !ok || usb.GatesTransport != "usb-hid" ||
		usb.Blocks(session.Transport{Capabilities: session.Capabilities{Manual: true}}) ||
		!usb.Blocks(session.Transport{Capabilities: session.Capabilities{Keyboard: true}}) {
		t.Fatal("USB helper gate blocked manual input or allowed missing USB helpers")
	}
	if a.controller.Snapshot().CanStart() {
		t.Fatal("startup restored readiness without observations")
	}
	gate := productionDriverGate(engine.Options{Assets: t.TempDir(), Manifest: a.manifest})
	if gate.DeveloperSource() || gate.Availability() == nil {
		t.Fatal("packaged driver gate accepted missing driver files")
	}
}

func TestProductionStartupWithBundledResourcesIsReadyOffline(t *testing.T) {
	assets := os.Getenv("ANY2JB_RUNTIME_ASSETS")
	if assets == "" {
		if abs, err := filepath.Abs("resources"); err == nil {
			if _, statErr := os.Stat(filepath.Join(abs, "runtime", "bridge.cjs")); statErr == nil {
				assets = abs
			}
		}
	}
	if assets == "" {
		t.Skip("packaged resources not staged; run cmd/prepare-runtime or set ANY2JB_RUNTIME_ASSETS")
	}
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	support := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(support, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(p, 0o700)
			}
			return nil
		})
	})
	opts := engine.Options{Assets: assets, Support: support, Version: "0.1.0", Manifest: a.manifest}
	if _, err := engine.Stage(opts); err != nil {
		t.Fatalf("engine.Stage failed against %s: %v", assets, err)
	}
	env := productionEnvironmentWithCivSwitch(
		opts,
		transport.DarwinOptions{Timing: transport.DefaultTiming()}, civswitch.Options{})
	if err := a.initializeSession(env); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"kurumuz", "cellenseres", "astral", "pepsipu", "BermTheSequel"} {
		p, err := a.manifest.Profile(id)
		if err != nil {
			t.Fatal(err)
		}
		checks, err := env.Platform.Inspect(context.Background(), session.Run{
			Profile:   p.SessionProfile(),
			Transport: session.Transport{Capabilities: session.Capabilities{Manual: true}},
		})
		if err != nil {
			t.Fatal(err)
		}
		byID := map[string]session.Check{}
		for _, check := range checks {
			byID[check.ID] = check
		}
		for _, want := range []string{"runtime", "engine", "input-adapter"} {
			if byID[want].State != session.CheckVerified {
				t.Fatalf("profile %s check %s not verified offline: %+v", id, want, byID[want])
			}
		}
		if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" && byID["usb-runtime"].State != session.CheckVerified {
			t.Fatalf("profile %s usb-runtime not verified on darwin/arm64: %+v", id, byID["usb-runtime"])
		}
	}
	iz, err := a.manifest.Profile("izmysh")
	if err != nil {
		t.Fatal(err)
	}
	izChecks, err := env.Platform.Inspect(context.Background(), session.Run{Profile: iz.SessionProfile()})
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range izChecks {
		if check.ID == "engine" && check.State != session.RequiresAttention {
			t.Fatalf("izmysh unexpectedly marked ready: %+v", check)
		}
	}
	mgr, ok := env.Tools.(*retained.Manager)
	if !ok {
		t.Fatal("expected *retained.Manager")
	}
	for _, bundledTool := range []string{
		"rpc-status", "system-code-dump", "ouncecake-webdav", "ouncecake-ftp",
		"sd-browser", "savekit-read", "savekit", "modloader", "launcher-seedsow-demo",
	} {
		if reason := mgr.Unavailable(bundledTool, "kurumuz"); reason != "" {
			t.Fatalf("bundled tool %s unavailable offline: %q", bundledTool, reason)
		}
	}
	if reason := mgr.Unavailable("mintbrew", "kurumuz"); reason == "" {
		t.Fatal("optional mintbrew should not be staged before on-demand install")
	}
	if !mgr.HomebrewInstallable(a.controller.Snapshot().Profile) {
		t.Fatal("expected optional homebrew to be installable on demand")
	}
	if reason := mgr.Unavailable("browser-research", "kurumuz"); reason == "" {
		t.Fatal("excluded browser-research must remain unavailable")
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		if reason := a.controller.CivSwitchUnavailable(); reason != "" {
			t.Fatalf("packaged CivSwitch unavailable on darwin/arm64: %s", reason)
		}
		gate := productionDriverGate(engine.Options{Assets: assets, Manifest: a.manifest})
		if err := gate.Availability(); err != nil {
			t.Fatalf("packaged driver gate unavailable on darwin/arm64: %v", err)
		}
	}
}
