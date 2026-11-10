package session_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/session"
)

func retainedModloader(t *testing.T, tools ...string) (*session.SessionController, *session.FakeEnvironment) {
	t.Helper()
	f := session.NewFakeEnvironment()
	f.RetainedSessions = []session.RetainedSession{{ID: "session-1"}}
	c := session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned", RetainsRPC: true, Tools: tools})
	if _, err := c.AttachRetained("session-1"); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.RetainedRPC)
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
	c, f := retainedModloader(t, "rpc-status")
	mods := writeTree(t, t.TempDir(), "contents/0100000000010000/romfs/data.bin")
	_, err := c.RunToolWith(session.ToolRequest{Tool: "modloader", ModsFolder: mods})
	if rejection, ok := err.(*session.Rejection); !ok || rejection.Reason != session.Unsupported {
		t.Fatalf("a profile without modloader capability ran it: %v", err)
	}
	if f.ToolRequest.Tool != "" || c.Snapshot().Stage != session.RetainedRPC {
		t.Fatalf("the refusal changed the lease: %+v %s", f.ToolRequest, c.Snapshot().Stage)
	}
}

func TestInvalidFoldersAreRejectedWithTheLeaseIntact(t *testing.T) {
	c, f := retainedModloader(t, "modloader", "custom-homebrew")
	link := t.TempDir()
	writeTree(t, link, "contents/0100000000010000/romfs/data.bin")
	symlinked := t.TempDir()
	if err := os.Symlink(filepath.Join(link, "contents"), filepath.Join(symlinked, "contents")); err != nil {
		t.Fatal(err)
	}
	bad := []session.ToolRequest{
		{Tool: "modloader"},
		{Tool: "modloader", ModsFolder: "relative/mods"},
		{Tool: "modloader", ModsFolder: t.TempDir()},
		{Tool: "modloader", ModsFolder: symlinked},
		{Tool: "modloader", ModsFolder: writeTree(t, t.TempDir(), "contents/not-a-title/romfs/a")},
		{Tool: "modloader", ModsFolder: writeTree(t, t.TempDir(), "contents/0100000000010000/cheats/a.txt.bin")},
		{Tool: "modloader", ModsFolder: writeTree(t, t.TempDir(), "contents/0100000000010000/exefs/sub/main")},
		{Tool: "modloader", ModsFolder: writeTree(t, t.TempDir(), "exefs_patches/set/notabuildid.ips")},
		{Tool: "modloader", ModsFolder: writeTree(t, t.TempDir(), "contents/0100000000010000/romfs/a"), CustomHomebrewFolder: t.TempDir()},
		{Tool: "custom-homebrew"},
		{Tool: "custom-homebrew", CustomHomebrewFolder: t.TempDir()},
		{Tool: "custom-homebrew", CustomHomebrewFolder: writeTree(t, t.TempDir(), "module.json")},
		{Tool: "custom-homebrew", ModsFolder: writeTree(t, t.TempDir(), "contents/0100000000010000/romfs/a")},
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
	if _, err := c.RunToolWith(session.ToolRequest{Tool: "modloader", ModsFolder: mods}); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.RetainedRPC)
	if f.ToolRequest.ModsFolder != mods {
		t.Fatalf("the validated folder did not reach the tool: %+v", f.ToolRequest)
	}
}

func TestCustomHomebrewAcceptsABuiltGPUApp(t *testing.T) {
	c, f := retainedModloader(t, "custom-homebrew")
	app := t.TempDir()
	if err := os.WriteFile(filepath.Join(app, "app.json"), []byte(`{"id":"hello-gpu","name":"Hello"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RunToolWith(session.ToolRequest{Tool: "custom-homebrew", CustomHomebrewFolder: app}); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.RetainedRPC)
	if f.ToolRequest.CustomHomebrewFolder != app {
		t.Fatalf("unexpected request %+v", f.ToolRequest)
	}
}
