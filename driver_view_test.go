package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/driver"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func TestDriverPanelBlockedAndDeclinePreservesSession(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := sessiontest.NewFakeEnvironment()
	f.Checks = []session.Check{{ID: "driver-loaded", State: session.RequiresAttention, GatesTransport: "usb-hid", NextAction: "Review driver approval."}}
	if err := a.initializeSession(f.Environment()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.controller.Inspect(); err != nil {
		t.Fatal(err)
	}
	a.driverPanel.gate = driver.New(driver.Options{Manifest: a.manifest})
	tt := ui.NewTester(a.view, 900, 2200)
	uiStage(t, a, tt, session.Ready)
	before := a.controller.Snapshot()
	if err := tt.Click("Review USB driver setup"); err != nil {
		t.Fatal(err)
	}
	d, _ := a.manifest.DistributionFor("usbgadget-binary")
	for _, message := range []string{d.Reason, "USB driver and startup security", "This driver is unsigned. It requires reduced startup security and user-approved kernel extensions. Re-enabling full protection with this driver has not been verified."} {
		if !tt.HasText(message) {
			t.Fatalf("missing disclosure %q: %v", message, tt.Texts())
		}
	}
	if button, ok := tt.AccessByRole(ui.RoleButton, "Approve driver installation"); !ok || !button.Disabled {
		t.Fatal("blocked driver approval must be disabled")
	}
	if err := tt.Click("Decline driver installation"); err != nil {
		t.Fatal(err)
	}
	if a.driverPanel.open || !reflect.DeepEqual(before, a.controller.Snapshot()) {
		t.Fatal("declining changed session state")
	}
	a.preferences.Transport = "keyboard"
	if err := a.applyPreferences(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.controller.Inspect(); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.NeedsSetup)
	if err := tt.Click("Review USB driver setup"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Use Guided manual launch instead"); err != nil {
		t.Fatal(err)
	}
	if a.driverPanel.open || !a.controller.Snapshot().Transport.Capabilities.Manual {
		t.Fatal("manual launch path is unavailable after declining the driver")
	}
}

func TestDriverPanelRequiresSecondConfirmationBeforeAdministrator(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for i := range a.manifest.Components {
		component := &a.manifest.Components[i]
		switch component.ID {
		case "usbgadget-binary", "usbgadget-info", "usbgadget-checksums", "driver-installer":
			component.SHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
			name := filepath.Join(root, filepath.FromSlash(component.Path))
			if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(name, []byte("hello"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	called := make(chan string, 1)
	release := make(chan struct{})
	a.driverPanel = driverPanel{open: true, gate: driver.New(driver.Options{Manifest: a.manifest, DeveloperSourceRoot: root,
		Administrator: func(_ context.Context, installer string) error { called <- installer; <-release; return nil },
	})}
	tt := ui.NewTester(a.view, 900, 2200)
	if err := tt.Click("Approve driver installation"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
		t.Fatal("first approval skipped confirmation")
	default:
	}
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Approve driver installation"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Install with administrator approval"); err != nil {
		t.Fatal(err)
	}
	select {
	case installer := <-called:
		if installer != filepath.Join(root, "scripts/install-driver.sh") {
			t.Fatalf("unexpected administrator action: %q", installer)
		}
	case <-time.After(time.Minute):
		t.Fatal("fake administrator was not reached")
	}
	// Receive the worker's result deterministically, then let the view consume it.
	close(release)
	select {
	case done := <-a.driverPanel.completion:
		a.driverPanel.completion <- done
	case <-time.After(time.Minute):
		t.Fatal("fake installation did not complete")
	}
	tt.Frame()
	if !a.driverPanel.result.RestartRequired || !a.driverPanel.result.DeveloperSource {
		t.Fatal("missing restart/developer evidence")
	}
}
