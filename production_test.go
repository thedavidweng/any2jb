package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/session"
)

func TestProductionStartupUsesRealBlockedAdapters(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	env := productionEnvironment(engine.Options{Assets: t.TempDir(), Support: t.TempDir(), Version: "fixture", Manifest: a.manifest})
	if env.Simulated {
		t.Fatal("production injected a simulated environment")
	}
	if _, ok := env.Host.(*engine.HostRunner); !ok {
		t.Fatal("production did not use the real host runner")
	}
	if err := a.initializeSession(env); err != nil {
		t.Fatal(err)
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
	blocked, _ := a.manifest.DistributionFor("usbgadget-binary")
	if gate.DeveloperSource() || gate.Availability() == nil || gate.Availability().Error() != blocked.Reason {
		t.Fatal("packaged driver gate bypassed the canonical distribution blocker")
	}
}
