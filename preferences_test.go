package main

import (
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
	"os"
	"path/filepath"
	"testing"
)

func TestPreferencesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	a, err := loadApp(path)
	if err != nil {
		t.Fatal(err)
	}
	if a.preferences.Profile != "kurumuz" || a.preferences.Transport != "manual" {
		t.Fatal(a.preferences)
	}
	a.preferences = preferences{Profile: "izmysh", Edition: "full", LANIPv4: "192.168.1.9", Transport: "keyboard"}
	if err := a.applyPreferences(); err != nil {
		t.Fatal(err)
	}
	b, err := loadApp(path)
	if err != nil || b.preferences != a.preferences {
		t.Fatalf("reload: %+v %v", b, err)
	}
	if err := b.initializeSession(sessiontest.NewFakeEnvironment().Environment()); err != nil {
		t.Fatal(err)
	}
	s := b.controller.Snapshot()
	if s.Profile.ID != "izmysh" || s.Profile.StartPort != 39395 || s.Profile.SaveName != "NS2I" ||
		s.Edition != "full" || s.LANIPv4 != "192.168.1.9" || s.Transport.Mode != "keyboard" ||
		s.Stage != session.Idle || len(s.Checks) != 0 || s.BootstrapSaved {
		t.Fatalf("reload did not configure a fresh, unverified session: %+v", s)
	}
	b.preferences.Edition = "demo"
	if err := b.applyPreferences(); err == nil {
		t.Fatal("unsupported edition accepted")
	}
	b.preferences.Edition = "full"
	b.preferences.LANIPv4 = "::1"
	if err := b.applyPreferences(); err == nil {
		t.Fatal("IPv6 accepted")
	}
	if err := os.WriteFile(path, []byte(`{"profile":"missing","edition":"full","transport":"manual"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadApp(path); err == nil {
		t.Fatal("invalid stored profile silently replaced")
	}
}
