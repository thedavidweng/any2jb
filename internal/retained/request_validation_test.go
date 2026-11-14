package retained_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/retained"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
)

func awaitSessionStage(t *testing.T, c *session.SessionController, want session.Stage) session.Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s := c.Snapshot()
		if !s.Busy && s.Stage == want {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; current=%s busy=%v", want, s.Stage, s.Busy)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func retainedValidatedController(t *testing.T, tools ...string) (*session.SessionController, *sessiontest.FakeEnvironment) {
	t.Helper()
	f := sessiontest.NewFakeEnvironment()
	f.ToolValidator = retained.ValidateToolRequest
	f.RetainedSessions = []session.RetainedSession{{ID: "session-1"}}
	c := session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned", RetainsRPC: true, Tools: tools})
	if _, err := c.AttachRetained("session-1"); err != nil {
		t.Fatal(err)
	}
	awaitSessionStage(t, c, session.RetainedRPC)
	return c, f
}

func writeTree(t *testing.T, root string, files ...string) string {
	t.Helper()
	for _, name := range files {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestModloaderNeedsTheProfileCapability(t *testing.T) {
	c, f := retainedValidatedController(t, "rpc-status")
	mods := writeTree(t, t.TempDir(), "contents/0100000000010000/romfs/data.bin")
	_, err := c.RunToolWith(session.ToolRequest{Tool: "modloader", ModsFolder: mods})
	if rejection, ok := err.(*session.Rejection); !ok || rejection.Reason != session.Unsupported {
		t.Fatalf("a profile without modloader capability ran it: %v", err)
	}
	if f.ToolRequest.Tool != "" || c.Snapshot().Stage != session.RetainedRPC {
		t.Fatalf("the refusal changed the lease: %+v %s", f.ToolRequest, c.Snapshot().Stage)
	}
}

func TestModloaderRefusesUnconfirmedCodeBeforeTransfer(t *testing.T) {
	c, f := retainedValidatedController(t, "modloader")
	mods := writeTree(t, t.TempDir(), "contents/0100000000010000/romfs/data.bin")
	_, err := c.RunToolWith(session.ToolRequest{Tool: "modloader", ModsFolder: mods})
	if rejection, ok := err.(*session.Rejection); !ok || rejection.Reason != session.InvalidConfiguration {
		t.Fatalf("unconfirmed code must be refused: %v", err)
	}
	if f.ToolRequest.Tool != "" || c.Snapshot().Stage != session.RetainedRPC {
		t.Fatal("unconfirmed mods reached the tool or changed the lease")
	}
}

func TestInvalidFoldersAreRejectedWithTheLeaseIntact(t *testing.T) {
	c, f := retainedValidatedController(t, "modloader", "custom-homebrew")
	link := t.TempDir()
	writeTree(t, link, "contents/0100000000010000/romfs/data.bin")
	symlinked := t.TempDir()
	if err := os.Symlink(filepath.Join(link, "contents"), filepath.Join(symlinked, "contents")); err != nil {
		t.Fatal(err)
	}
	bad := []session.ToolRequest{
		{Tool: "modloader", ModsFolder: "relative/mods", CodeConsent: true},
		{Tool: "modloader", ModsFolder: t.TempDir(), CodeConsent: true},
		{Tool: "modloader", ModsFolder: symlinked, CodeConsent: true},
		{Tool: "modloader", ModsFolder: writeTree(t, t.TempDir(), "contents/not-a-title/romfs/a"), CodeConsent: true},
		{Tool: "modloader", ModsFolder: writeTree(t, t.TempDir(), "contents/0100000000010000/cheats/a.txt.bin"), CodeConsent: true},
		{Tool: "modloader", ModsFolder: writeTree(t, t.TempDir(), "contents/0100000000010000/exefs/sub/main"), CodeConsent: true},
		{Tool: "modloader", ModsFolder: writeTree(t, t.TempDir(), "exefs_patches/set/notabuildid.ips"), CodeConsent: true},
		{Tool: "modloader", ModsFolder: writeTree(t, t.TempDir(), "contents/0100000000010000/romfs/a"), CustomHomebrewFolder: t.TempDir(), CodeConsent: true},
		{Tool: "custom-homebrew", CodeConsent: true},
		{Tool: "custom-homebrew", CustomHomebrewFolder: t.TempDir(), CodeConsent: true},
		{Tool: "custom-homebrew", CustomHomebrewFolder: writeTree(t, t.TempDir(), "module.json"), CodeConsent: true},
		{Tool: "custom-homebrew", ModsFolder: writeTree(t, t.TempDir(), "contents/0100000000010000/romfs/a"), CodeConsent: true},
	}
	for _, request := range bad {
		_, err := c.RunToolWith(request)
		rejection, ok := err.(*session.Rejection)
		if !ok || rejection.Reason != session.InvalidConfiguration {
			t.Fatalf("invalid request must state InvalidConfiguration: %+v %v", request, err)
		}
		if f.ToolRequest.Tool != "" || c.Snapshot().Stage != session.RetainedRPC {
			t.Fatalf("an invalid folder reached the tool or dropped the lease: %+v %s", request, c.Snapshot().Stage)
		}
	}
	mods := writeTree(t, t.TempDir(), "contents/0100000000010000/romfs/data/a.bin",
		"contents/0100000000010000/exefs/main", "contents/0100000000010000/README.md",
		"exefs_patches/fix/0123456789abcdef0123456789abcdef.ips", ".DS_Store")
	if _, err := c.RunToolWith(session.ToolRequest{Tool: "modloader", ModsFolder: mods, CodeConsent: true}); err != nil {
		t.Fatal(err)
	}
	awaitSessionStage(t, c, session.RetainedRPC)
	if f.ToolRequest.ModsFolder != mods {
		t.Fatalf("the validated folder did not reach the tool: %+v", f.ToolRequest)
	}
}

func TestCustomHomebrewAcceptsABuiltGPUApp(t *testing.T) {
	c, f := retainedValidatedController(t, "custom-homebrew")
	app := t.TempDir()
	if err := os.WriteFile(filepath.Join(app, "app.json"), []byte(`{"id":"hello-gpu","name":"Hello"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RunToolWith(session.ToolRequest{Tool: "custom-homebrew", CustomHomebrewFolder: app, CodeConsent: true}); err != nil {
		t.Fatal(err)
	}
	awaitSessionStage(t, c, session.RetainedRPC)
	if f.ToolRequest.CustomHomebrewFolder != app {
		t.Fatalf("unexpected request %+v", f.ToolRequest)
	}
}

func TestUserHomebrewRequiresPerActionCodeConsent(t *testing.T) {
	for _, tool := range []string{"custom-homebrew", "native-hblauncher"} {
		t.Run(tool, func(t *testing.T) {
			c, f := retainedValidatedController(t, tool)
			folder := t.TempDir()
			if err := os.WriteFile(filepath.Join(folder, "app.json"), []byte(`{"id":"hello","name":"Hello"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			request := session.ToolRequest{Tool: tool}
			if tool == "custom-homebrew" {
				request.CustomHomebrewFolder = folder
			} else {
				request.AppFolder = folder
			}
			if _, err := c.RunToolWith(request); err == nil {
				t.Fatal("unconfirmed user code was accepted")
			}
			if f.ToolRequest.Tool != "" {
				t.Fatal("unconfirmed user code reached the tool")
			}
			request.CodeConsent = true
			if _, err := c.RunToolWith(request); err != nil {
				t.Fatal(err)
			}
			awaitSessionStage(t, c, session.RetainedRPC)
			if !f.ToolRequest.CodeConsent {
				t.Fatal("consent was not carried to the retained runner")
			}
		})
	}
}

func TestHomebrewToolRequestsValidateTheirInput(t *testing.T) {
	c, _ := retainedValidatedController(t, "rpc-status", "mintbrew", "native-hblauncher", "modloader")
	bad := []session.ToolRequest{
		{Tool: "mintbrew", Modules: []string{"blitbun"}},
		{Tool: "mintbrew", StremioQuality: "4k"},
		{Tool: "mintbrew", StremioAddonURLs: []string{"ftp://example.com/manifest.json"}},
		{Tool: "mintbrew", StremioAddonURLs: []string{"https://example.com/not-a-manifest"}},
		{Tool: "mintbrew", AppFolder: t.TempDir(), CodeConsent: false},
		{Tool: "native-hblauncher", SDRomFolder: "sd/roms"},
		{Tool: "native-hblauncher", SDRomFolder: "/sd/roms/"},
		{Tool: "native-hblauncher", SDRomFolder: "/sd/../roms"},
		{Tool: "native-hblauncher", AppFolder: "relative/apps", CodeConsent: true},
		{Tool: "native-hblauncher", AppFolder: "/definitely/missing-any2jb-fixture", CodeConsent: true},
		{Tool: "mintbrew", SDRomFolder: "/sd/roms/nes"},
		{Tool: "modloader", CodeConsent: false},
	}
	for _, request := range bad {
		_, err := c.RunToolWith(request)
		rejection, ok := err.(*session.Rejection)
		if !ok || rejection.Reason != session.InvalidConfiguration {
			t.Fatalf("invalid request must state InvalidConfiguration: %+v %v", request, err)
		}
	}
	if _, err := c.RunToolWith(session.ToolRequest{Tool: "mintbrew",
		AppFolder: t.TempDir(), CodeConsent: true, StremioQuality: "1080p",
		StremioAddonURLs: []string{"https://v3-cinemeta.strem.io/manifest.json"}}); err != nil {
		t.Fatal(err)
	}
	awaitSessionStage(t, c, session.RetainedRPC)
	if _, err := c.RunToolWith(session.ToolRequest{Tool: "modloader", CodeConsent: true}); err != nil {
		t.Fatal(err)
	}
	awaitSessionStage(t, c, session.RetainedRPC)
	if _, err := c.RunToolWith(session.ToolRequest{Tool: "native-hblauncher",
		SDRomFolder: "/sd/roms/nes", AppFolder: t.TempDir(), CodeConsent: true}); err != nil {
		t.Fatal(err)
	}
	awaitSessionStage(t, c, session.RetainedRPC)
}
