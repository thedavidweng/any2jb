package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func TestDashboardReadinessEvidenceAndPrerequisites(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := sessiontest.NewFakeEnvironment()
	f.Checks = []session.Check{
		{ID: "network", State: session.CheckVerified, Message: "Assigned locally."},
		{ID: "usb-drd0", State: session.Available, GatesTransport: "usb-hid", Message: "Not enumerated."},
		{ID: "driver-loaded", State: session.RequiresAttention, GatesTransport: "usb-hid", Message: "Not loaded.", NextAction: "Review driver approval."},
	}
	if err := a.initializeSession(f.Environment()); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 720, 1800)
	if _, ok := tt.AccessByRole(ui.RoleTextField, "Preferred LAN IPv4 (optional)"); !ok {
		t.Fatal("LAN override hidden in Developer Options")
	}
	if err := tt.Click("Check readiness"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	for _, text := range []string{
		"Verified", "Available but not verified", "Requires attention", "Review driver approval.",
		"Reference prerequisites, not compatibility guarantees",
		"Switch 2 firmware 23.0.1 is the reference target. Newer firmware is not assumed compatible.",
		"Use SmileBASIC full 4.4.9, or demo 4.4.6 only with a demo-capable host. Keep the game on internal storage with more than 20 MiB free in its save.",
		"Connect the console by Ethernet to a LAN reachable from this host. Use a US English USB keyboard layout.",
		"Use a USB-C data cable, not a charge-only cable. The Mac must receive power from the console for the required USB data direction.",
	} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q: %v", text, tt.Texts())
		}
	}
	if path := os.Getenv("ANY2JB_UI_PNG"); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
}
