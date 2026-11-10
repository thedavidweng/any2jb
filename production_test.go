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
	for _, check := range checks {
		if check.State != session.RequiresAttention {
			t.Fatalf("unavailable prerequisite marked ready: %+v", check)
		}
	}
	if a.controller.Snapshot().CanStart() {
		t.Fatal("startup restored readiness without observations")
	}
}
