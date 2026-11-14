package civswitch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/internal/manifest"
)

// stagedPlugins is the v0.8.2 layout: one plugin stages the USB payload and one
// stages the persistent trigger components.
var stagedPlugins = map[string]string{
	payloadComponent:       "civswitch-usb",
	installerComponent:     "civswitch-trigger",
	coreComponent:          "civswitch-trigger",
	saveTemplateComponent:  "civswitch-trigger",
	backupPayloadComponent: "civswitch-trigger",
}

func stagedPluginFixture(t *testing.T) (*manifest.Manifest, string) {
	t.Helper()
	toolRoot := t.TempDir()
	paths := map[string]string{
		payloadComponent:       "integrations/civswitch/payload.txt",
		installerComponent:     "vendor/CivSwitch/installer-creator/install.py",
		coreComponent:          "vendor/CivSwitch/core.js",
		saveTemplateComponent:  "vendor/CivSwitch/installer-creator/save_template.bin",
		backupPayloadComponent: "integrations/civswitch/backup-single.js",
	}
	m := &manifest.Manifest{}
	for id, plugin := range stagedPlugins {
		body := "fixture " + id
		target := filepath.Join(toolRoot, "tools", plugin, filepath.FromSlash(paths[id]))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		// StageTools writes every staged file read-only.
		if err := os.WriteFile(target, []byte(body), 0o400); err != nil {
			t.Fatal(err)
		}
		m.Components = append(m.Components, manifest.Component{
			ID: id, Path: paths[id],
			SHA256:       fmt.Sprintf("%x", sha256Sum([]byte(body))),
			Distribution: manifest.Distribution{Decision: "may-ship", Bundled: false},
		})
	}
	return m, toolRoot
}

func TestStagedPluginRootsFollowTheInstalledPlugins(t *testing.T) {
	m, toolRoot := stagedPluginFixture(t)
	adapter := New(Options{Manifest: m, ToolRoot: toolRoot, Support: t.TempDir()})
	if err := adapter.Availability(); err != nil {
		t.Fatalf("staged plugins were not available: %v", err)
	}
	if got := adapter.payload(); got != filepath.Join(toolRoot, "tools/civswitch-usb/integrations/civswitch/payload.txt") {
		t.Fatalf("payload path: %s", got)
	}
	for id, want := range map[string]string{
		installerComponent:     "tools/civswitch-trigger/vendor/CivSwitch/installer-creator/install.py",
		coreComponent:          "tools/civswitch-trigger/vendor/CivSwitch/core.js",
		saveTemplateComponent:  "tools/civswitch-trigger/vendor/CivSwitch/installer-creator/save_template.bin",
		backupPayloadComponent: "tools/civswitch-trigger/integrations/civswitch/backup-single.js",
	} {
		got, err := adapter.componentPath(id)
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Join(toolRoot, filepath.FromSlash(want)) {
			t.Fatalf("%s path: %s", id, got)
		}
	}
	// The staged copies keep their read-only modes; nothing widened the plugin.
	info, err := os.Stat(adapter.payload())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o400 {
		t.Fatalf("the plugin stage was modified: %v", info.Mode())
	}
}

func TestStagedPluginLayoutReportsAMissingInstallBeforeUse(t *testing.T) {
	m, _ := stagedPluginFixture(t)
	empty := t.TempDir()
	adapter := New(Options{Manifest: m, ToolRoot: empty, Support: t.TempDir()})
	err := adapter.Availability()
	if err == nil {
		t.Fatal("an uninstalled civswitch-usb plugin was reported as available")
	}
	if !errors.Is(err, os.ErrNotExist) && !strings.Contains(err.Error(), "not staged") {
		t.Fatalf("missing plugin error: %v", err)
	}
	if _, err := adapter.dependentLoader(t.Context(), "192.0.2.2", payloadComponent); err == nil {
		t.Fatal("a dependent action ran without the plugin")
	}
	if adapter.payload() != filepath.Join(empty, "tools/civswitch-usb/integrations/civswitch/payload.txt") {
		t.Fatal("the payload path did not follow the stage root")
	}
}

func TestChangedStagedPayloadIsAnIntegrityFailure(t *testing.T) {
	m, toolRoot := stagedPluginFixture(t)
	payload := filepath.Join(toolRoot, "tools/civswitch-usb/integrations/civswitch/payload.txt")
	if err := os.Chmod(payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payload, []byte("tampered"), 0o400); err != nil {
		t.Fatal(err)
	}
	err := New(Options{Manifest: m, ToolRoot: toolRoot}).Availability()
	if err == nil {
		t.Fatal("a changed staged payload was reported as available")
	}
	if strings.Contains(err.Error(), "not staged") {
		t.Fatalf("a changed payload was reported as missing: %v", err)
	}
	var integrity *manifest.IntegrityError
	if !errors.As(err, &integrity) {
		t.Fatalf("changed payload error: %v", err)
	}
}

func TestRelativeStageRootIsRefused(t *testing.T) {
	m, _ := stagedPluginFixture(t)
	if err := New(Options{Manifest: m, ToolRoot: "tools/version"}).Availability(); err == nil {
		t.Fatal("accepted a relative plugin stage root")
	}
}
