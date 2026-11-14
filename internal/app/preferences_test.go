package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
)

func TestPreferencesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	a, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if a.Preferences.Profile != "kurumuz" || a.Preferences.Transport != "manual" {
		t.Fatal(a.Preferences)
	}
	a.Preferences = Preferences{Profile: "pepsipu", Edition: "full", LANIPv4: "192.168.1.9", Transport: "keyboard", Language: "zh-CN"}
	if err := a.ApplyPreferences(); err != nil {
		t.Fatal(err)
	}
	b, err := Load(path)
	if err != nil || b.Preferences != a.Preferences {
		t.Fatalf("reload: %+v %v", b, err)
	}
	if _, err := b.InitializeSession(sessiontest.NewFakeEnvironment().Environment()); err != nil {
		t.Fatal(err)
	}
	s := b.Controller.Snapshot()
	if s.Profile.ID != "pepsipu" || s.Profile.StartPort != 39380 || s.Profile.SaveName != "NS2" ||
		s.Edition != "full" || s.LANIPv4 != "192.168.1.9" || s.Transport.Mode != "keyboard" ||
		s.Stage != session.Idle || len(s.Checks) != 0 || s.BootstrapSaved {
		t.Fatalf("reload did not configure a fresh, unverified session: %+v", s)
	}
	b.Preferences.Language = "invalid-locale"
	if err := b.ApplyPreferences(); err == nil {
		t.Fatal("unsupported language accepted")
	}
	b.Preferences.Language = "system"
	if err := b.ApplyPreferences(); err != nil || b.Preferences.Language != "" {
		t.Fatalf("system language preference did not normalize to empty: %q, %v", b.Preferences.Language, err)
	}
	b.Preferences.Edition = "demo"
	if err := b.ApplyPreferences(); err == nil {
		t.Fatal("unsupported edition accepted")
	}
	b.Preferences.Edition = "full"
	b.Preferences.LANIPv4 = "::1"
	if err := b.ApplyPreferences(); err == nil {
		t.Fatal("IPv6 accepted")
	}
	if err := os.WriteFile(path, []byte(`{"profile":"missing","edition":"full","transport":"manual"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("invalid stored profile silently replaced")
	}
}
