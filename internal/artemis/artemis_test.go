package artemis_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/artemis"
	"github.com/thedavidweng/any2jb/internal/manifest"
)

// referenceTree is the pinned mac2jb v0.8.2 source the on-demand plugins are
// built from. Plugin bytes are never bundled with any2jb, so the parity tests
// that run them skip when the reference is not staged; CI sets
// ANY2JB_REFERENCE_ROOT.
func referenceTree(t *testing.T) string {
	t.Helper()
	directory := os.Getenv("ANY2JB_REFERENCE_ROOT")
	if directory == "" {
		directory = "/tmp/mac2jb-v082-ref"
	}
	if _, err := os.Stat(filepath.Join(directory, "vendor/ns2-savekit/NOTICE")); err != nil {
		t.Skip("the pinned mac2jb v0.8.2 reference tree is not staged")
	}
	return directory
}

func nodeBinary(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required to run the pinned Artemis tools")
	}
	return node
}

// savekitFiles and artemisFSFiles are the pinned plugin files these tools read.
// The civswitch and ns2-savekit overlays are what prepare-artemis-fs.cjs joins
// into its stage, so a plugin root without them cannot prepare a host.
var (
	savekitFiles = []string{
		"vendor/ns2-savekit/NOTICE",
		"vendor/ns2-savekit/research/catalog.json",
		"vendor/ns2-savekit/research/current/config/titles.json",
		"vendor/ns2-savekit/research/current/tools/artemis-http.cjs",
		"vendor/ns2-savekit/research/current/tools/transports/artemis-http.cjs",
	}
	artemisFSFiles = []string{
		"integrations/artemis-fs",
		"integrations/civswitch/fs-kit/port/engine/shared/ipc.cjs",
		"integrations/civswitch/fs-kit/port/engine/switch2/ro/ncm.cjs",
		"integrations/civswitch/fs-kit/port/engine/switch2/memory/fs-memory-control.cjs",
		"vendor/ns2-savekit/overlay/engine/shared/ipc.cjs",
		"vendor/ns2-savekit/overlay/engine/switch2/ro/ncm.cjs",
		"scripts/prepare-artemis-fs.cjs",
		"scripts/prepare-civswitch-savekit.cjs",
		"scripts/gui-dump.cjs",
	}
)

type fixture struct {
	manifest *manifest.Manifest
	support  string
	toolRoot string
}

// newFixture stages both plugins exactly as retained.StageTools does: files at
// 0400 inside directories at 0500.
func newFixture(t *testing.T) fixture {
	t.Helper()
	reference := referenceTree(t)
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	support := canonical(t, filepath.Join(t.TempDir(), "support"))
	toolRoot := filepath.Join(support, "tools", "0.1.0")
	for _, plugin := range []struct {
		id    string
		paths []string
	}{
		{artemis.SaveKitPlugin, savekitFiles},
		{artemis.ArtemisFSPlugin, artemisFSFiles},
	} {
		root := filepath.Join(toolRoot, "tools", plugin.id)
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		stageFixture(t, reference, root, plugin.paths)
		if err := m.Verify(root, plugin.id); err != nil {
			t.Fatalf("the staged %s fixture does not verify: %v", plugin.id, err)
		}
	}
	return fixture{manifest: m, support: support, toolRoot: toolRoot}
}

// stageFixture copies pinned reference paths into a plugin root at the read-only
// modes the production stage uses.
func stageFixture(t *testing.T, reference, root string, paths []string) {
	t.Helper()
	for _, rel := range paths {
		source := filepath.Join(reference, filepath.FromSlash(rel))
		info, err := os.Lstat(source)
		if err != nil {
			t.Fatalf("the reference tree is missing %s: %v", rel, err)
		}
		if !info.IsDir() {
			if err := copyPinned(source, filepath.Join(root, filepath.FromSlash(rel))); err != nil {
				t.Fatal(err)
			}
			continue
		}
		err = filepath.WalkDir(source, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(reference, name)
			if err != nil {
				return err
			}
			target := filepath.Join(root, relative)
			if entry.IsDir() {
				return os.MkdirAll(target, 0o700)
			}
			return copyPinned(name, target)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	freeze(t, root)
}

func copyPinned(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o400)
}

// freeze closes the stage the way retained.StageTools leaves it. Directories go
// last: a 0500 directory no longer accepts files.
func freeze(t *testing.T, root string) {
	t.Helper()
	var directories []string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			directories = append(directories, name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range directories {
		if err := os.Chmod(directory, 0o500); err != nil {
			t.Fatal(err)
		}
	}
	// A 0500 directory would stop t.TempDir's own cleanup.
	t.Cleanup(func() { _ = thaw(root) })
}

func thaw(root string) error {
	return filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return nil
		}
		return os.Chmod(name, 0o700)
	})
}

func canonical(t *testing.T, directory string) string {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

// mockLauncher is a local Artemis HTTP server. The pinned tool talks to it over
// the real protocol, so the transport, its read-back verification and the
// explicit-file backups are exercised rather than stubbed.
type mockLauncher struct {
	server *httptest.Server
	// bridge, when set, holds the Seedsow bridge POST open until it is closed or
	// the request is aborted, so a test can stop a live host.
	bridge chan struct{}

	mu    sync.Mutex
	files map[string][]byte
}

func newMockLauncher(t *testing.T) *mockLauncher {
	t.Helper()
	m := &mockLauncher{files: map[string][]byte{"existing.dat": []byte("0123456789")}}
	m.server = httptest.NewServer(m)
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockLauncher) address(t *testing.T) (string, int) {
	t.Helper()
	parsed, err := url.Parse(m.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Hostname(), port
}

func (m *mockLauncher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/status":
		io.WriteString(w, "n2h HTTP server (seedsow)\nArtemis launcher v2\n")
	case r.Method == http.MethodPost && r.URL.Path == "/eval":
		code, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if reply, ok := m.reply(string(code)); ok {
			io.WriteString(w, reply)
			return
		}
		if m.bridge != nil {
			select {
			case <-m.bridge:
			case <-r.Context().Done():
				return
			}
		}
		io.WriteString(w, "status: ok\nresult: 42\noutput:\nhello\n")
	case r.Method == http.MethodPut && r.URL.Path == "/save":
		body, err := io.ReadAll(io.LimitReader(r.Body, artemis.BodyLimit+1024))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		name := strings.TrimPrefix(r.URL.Query().Get("path"), "save:/")
		m.mu.Lock()
		m.files[name] = body
		m.mu.Unlock()
		fmt.Fprintf(w, "wrote %d bytes\n", len(body))
	case r.Method == http.MethodDelete && r.URL.Path == "/delete":
		name := strings.TrimPrefix(r.URL.Query().Get("path"), "save:/")
		m.mu.Lock()
		_, present := m.files[name]
		delete(m.files, name)
		m.mu.Unlock()
		if !present {
			http.Error(w, "missing", http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, "deleted save:/%s\n", name)
	default:
		http.NotFound(w, r)
	}
}

// reply models the launcher's Lua for the pinned scripts the tool sends.
func (m *mockLauncher) reply(code string) (string, bool) {
	switch {
	case strings.Contains(code, "mac2jb-profile"):
		return "status: ok\n" + seedsowProfile + "\n", true
	case strings.Contains(code, "-- ns2http:stat "):
		var request struct {
			Path string `json:"path"`
		}
		json.Unmarshal([]byte(evalPayload(code, "stat")), &request)
		m.mu.Lock()
		data, present := m.files[request.Path]
		m.mu.Unlock()
		if !present {
			return "status: ok\noutput:\nmissing\n", true
		}
		return fmt.Sprintf("status: ok\noutput:\nsize %d\n", len(data)), true
	case strings.Contains(code, "-- ns2http:read "):
		var request struct {
			Offset int `json:"offset"`
			Length int `json:"length"`
			Path   string
		}
		json.Unmarshal([]byte(evalPayload(code, "read")), &request)
		m.mu.Lock()
		data := m.files[request.Path]
		m.mu.Unlock()
		var chunk []byte
		if request.Offset < len(data) {
			end := request.Offset + request.Length
			if end > len(data) {
				end = len(data)
			}
			chunk = data[request.Offset:end]
		}
		return fmt.Sprintf("status: ok\noutput:\n%d\n%s\n", len(chunk), hex.EncodeToString(chunk)), true
	case strings.Contains(code, "-- ns2http:append "):
		var request struct {
			Path string `json:"path"`
		}
		json.Unmarshal([]byte(evalPayload(code, "append")), &request)
		match := hexLine.FindStringSubmatch(code)
		if match == nil {
			return "", false
		}
		appended, err := hex.DecodeString(match[1])
		if err != nil {
			return "", false
		}
		m.mu.Lock()
		m.files[request.Path] = append(m.files[request.Path], appended...)
		m.mu.Unlock()
		return "status: ok\noutput:\nappended\n", true
	case strings.Contains(code, "-- ns2http:rename "):
		var request struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		json.Unmarshal([]byte(evalPayload(code, "rename")), &request)
		m.mu.Lock()
		data, present := m.files[request.From]
		if present {
			m.files[request.To] = data
			delete(m.files, request.From)
		}
		m.mu.Unlock()
		if !present {
			return "status: ok\noutput:\nfalse\nrename failed\n", true
		}
		return "status: ok\noutput:\ntrue\n", true
	case strings.Contains(code, "print(_VERSION)"):
		return "status: ok\noutput:\nLua 5.4\nOunce\ntable\n", true
	}
	return "", false
}

var hexLine = regexp.MustCompile(`local h="([0-9a-f]*)"`)

func evalPayload(code, name string) string {
	prefix := "-- ns2http:" + name + " "
	for _, line := range strings.Split(code, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
}

// seedsowProfile is the runtime profile the pinned compatibility check requires:
// the Seedsow game, a Switch 2 platform and its exact SDK offsets.
const seedsowProfile = "mac2jb-profile\tseedsow\tOunce\t8000000\t9000000\t" +
	"function\tfunction\tfunction\tfunction\tfunction\tfunction\tfunction\t" +
	"14d4c0\t14d3b0\t14d440\t14d380\t14d300\t14d350\t14d510\t14d560\t168b54\t168ca4\t3d9e54"

func TestPluginRootRequiresAnInstalledPlugin(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	support := canonical(t, filepath.Join(t.TempDir(), "support"))
	toolRoot := filepath.Join(support, "tools", "0.1.0")
	for _, id := range []string{artemis.SaveKitPlugin, artemis.ArtemisFSPlugin} {
		_, err := artemis.PluginRoot(toolRoot, m, id)
		var missing *artemis.PluginMissingError
		if !errors.As(err, &missing) || missing.ID != id {
			t.Fatalf("a missing %s plugin must be reported for installation: %v", id, err)
		}
	}
	if _, err := artemis.LoadCatalog(toolRoot, m); !errors.As(err, new(*artemis.PluginMissingError)) {
		t.Fatalf("the catalog needs the savekit plugin: %v", err)
	}
	if _, err := artemis.PluginRoot("relative/tools", m, artemis.SaveKitPlugin); err == nil {
		t.Fatal("an unverified tool root must be refused")
	}
	if _, err := artemis.PluginRoot(toolRoot, nil, artemis.SaveKitPlugin); err == nil {
		t.Fatal("plugin verification needs the canonical manifest")
	}
	if _, err := artemis.PluginRoot(toolRoot, m, "homebrew"); err == nil {
		t.Fatal("an unsupported plugin must be refused")
	}
}

func TestConsoleAddressStaysOnThePrivateLAN(t *testing.T) {
	for _, test := range []struct {
		address string
		allowed bool
	}{
		{"192.168.1.55", true},
		{"10.0.0.2", true},
		{"172.16.4.4", true},
		{"172.31.255.254", true},
		{"172.32.0.1", false},
		{"8.8.8.8", false},
		{"169.254.1.1", false},
		{"255.255.255.255", false},
		{"0.0.0.0", false},
		{"192.168.001.55", false},
		{"switch.local", false},
		{"", false},
	} {
		if err := artemis.ConsoleAddress(test.address, false); (err == nil) != test.allowed {
			t.Fatalf("ConsoleAddress(%q) = %v, want allowed=%t", test.address, err, test.allowed)
		}
	}
	if err := artemis.ConsoleAddress("127.0.0.1", true); err != nil {
		t.Fatalf("a loopback fixture must be selectable: %v", err)
	}
	if err := artemis.ConsoleAddress("127.0.0.1", false); !errors.Is(err, artemis.ErrAddress) {
		t.Fatalf("loopback is never the console the user confirmed: %v", err)
	}
}

func TestSavePathProtectsTheLauncher(t *testing.T) {
	for _, test := range []struct {
		path, want string
		write      bool
		refused    bool
	}{
		{path: "save:/file.bin", want: "save:/file.bin", write: true},
		{path: "  plain.dat ", want: "save:/plain.dat", write: true},
		{path: "save:/core.lua", refused: true, write: true},
		{path: "save:/core.lua", want: "save:/core.lua"},
		{path: "save:/inject2.lua", want: "save:/inject2.lua"},
		{path: "save:/nested/file.bin", refused: true, write: true},
		{path: "/absolute.bin", refused: true, write: true},
		{path: "save:/../escape.dat", refused: true, write: true},
		{path: "save:/sp ace.bin", refused: true, write: true},
		{path: "save:/" + strings.Repeat("a", 200), refused: true, write: true},
		{path: "", refused: true, write: true},
	} {
		var got string
		var err error
		if test.write {
			got, err = artemis.SavePath(test.path)
		} else {
			got, err = artemis.ReadPath(test.path)
		}
		if test.refused {
			if !errors.Is(err, artemis.ErrSavePath) {
				t.Fatalf("SavePath(%q) = %q, %v; want a refusal", test.path, got, err)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Fatalf("SavePath(%q) = %q, %v; want %q", test.path, got, err, test.want)
		}
	}
}

func TestConsentGatesRefuseBeforeNode(t *testing.T) {
	client := artemis.Client{Node: artemis.NodeBinary(filepath.Join(t.TempDir(), "absent-node"))}
	ctx := context.Background()
	if _, err := client.Eval(ctx, artemis.EvalRequest{Host: "192.168.1.55", Code: "print(1)"}); !errors.Is(err, artemis.ErrConsent) {
		t.Fatalf("arbitrary Lua needs explicit consent: %v", err)
	}
	if _, err := client.Put(ctx, artemis.PutRequest{Host: "192.168.1.55", Path: "save:/a.bin", In: "a.bin"}); !errors.Is(err, artemis.ErrConsent) {
		t.Fatalf("an upload needs explicit consent: %v", err)
	}
	if _, err := client.Remove(ctx, artemis.RemoveRequest{Host: "192.168.1.55", Path: "save:/a.bin"}); !errors.Is(err, artemis.ErrConsent) {
		t.Fatalf("a delete needs explicit consent: %v", err)
	}
	// Consent comes first: a refused mutation never reaches the plugin or Node.
	if _, err := client.Eval(ctx, artemis.EvalRequest{Host: "not-an-ip", Code: "print(1)"}); !errors.Is(err, artemis.ErrConsent) {
		t.Fatalf("consent is checked before the address: %v", err)
	}
}

func TestLoadCatalogVerifiesThePinnedInventory(t *testing.T) {
	f := newFixture(t)
	catalog, err := artemis.LoadCatalog(f.toolRoot, f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Revision) != 40 || len(catalog.LegacyRevision) != 40 {
		t.Fatalf("the catalog revisions are not pinned: %q %q", catalog.Revision, catalog.LegacyRevision)
	}
	if !slices.ContainsFunc(catalog.Tools, func(tool artemis.InventoryTool) bool {
		return tool.Name == "artemis-http.cjs" && tool.Path == "current/tools/artemis-http.cjs" && tool.Execution == "node"
	}) {
		t.Fatalf("the catalog lost the Artemis HTTP tool: %+v", catalog.Tools)
	}
	for _, test := range []struct {
		id, name, engine string
		demo             bool
	}{
		{artemis.SeedsowDemoTitleID, "Seedsow Lullaby (demo)", "seedsow", true},
		{artemis.SeedsowFullTitleID, "Seedsow Lullaby (full)", "seedsow", false},
	} {
		index := slices.IndexFunc(catalog.Games, func(game artemis.ArtemisGame) bool { return game.TitleID == test.id })
		if index < 0 {
			t.Fatalf("the catalog lost %s: %+v", test.id, catalog.Games)
		}
		game := catalog.Games[index]
		if game.Name != test.name || game.Engine != test.engine || game.Status != "fs-verified" || game.Demo != test.demo {
			t.Fatalf("game %s = %+v", test.id, game)
		}
	}
	// Content the manifest did not record must never define a game.
	catalogPath := filepath.Join(f.toolRoot, "tools", artemis.SaveKitPlugin, "vendor/ns2-savekit/research/catalog.json")
	if err := os.Chmod(filepath.Dir(catalogPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(catalogPath, 0o600); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, append(body, ' '), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := artemis.LoadCatalog(f.toolRoot, f.manifest); err == nil ||
		!strings.Contains(err.Error(), "recorded digest") {
		t.Fatalf("a changed catalog was accepted: %v", err)
	}
}

func TestArtemisHTTPRoundTripsAgainstTheLauncherFixture(t *testing.T) {
	f := newFixture(t)
	launcher := newMockLauncher(t)
	host, port := launcher.address(t)
	client := artemis.Client{Node: artemis.NodeBinary(nodeBinary(t)), ToolRoot: f.toolRoot,
		Manifest: f.manifest, Support: f.support, Port: port, AllowLoopback: true}
	ctx := context.Background()

	status, err := client.Status(ctx, host)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.Banner, "n2h HTTP server (seedsow)") {
		t.Fatalf("banner: %q", status.Banner)
	}
	health, err := client.Health(ctx, host)
	if err != nil {
		t.Fatal(err)
	}
	if health.Lua != "Lua 5.4" || health.Platform != "Ounce" || health.Engine != "table" {
		t.Fatalf("health: %+v", health)
	}
	stat, err := client.Stat(ctx, host, "save:/existing.dat")
	if err != nil || !stat.Exists || stat.Size != 10 {
		t.Fatalf("stat: %+v %v", stat, err)
	}
	// Protected launcher files may be inspected, never replaced.
	if protected, err := client.Stat(ctx, host, "save:/core.lua"); err != nil || protected.Exists {
		t.Fatalf("reading a protected file must stay possible: %+v %v", protected, err)
	}

	folder := t.TempDir()
	if _, err := client.Get(ctx, artemis.GetRequest{Host: host, Path: "save:/absent.dat",
		Out: filepath.Join(folder, "absent.dat")}); !errors.Is(err, artemis.ErrRefused) {
		t.Fatalf("a missing save must be a refused transfer: %v", err)
	}
	destination := filepath.Join(folder, "download.dat")
	download, err := client.Get(ctx, artemis.GetRequest{Host: host, Path: "save:/existing.dat", Out: destination})
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256([]byte("0123456789"))
	if download.Size != 10 || download.SHA256 != hex.EncodeToString(expected[:]) {
		t.Fatalf("download: %+v", download)
	}
	if body, err := os.ReadFile(destination); err != nil || string(body) != "0123456789" {
		t.Fatalf("downloaded bytes: %q %v", body, err)
	}

	probe, err := client.Probe(ctx, host)
	if err != nil || !probe.Rename.OK || probe.ServerBodyCap != artemis.BodyLimit {
		t.Fatalf("probe: %+v %v", probe, err)
	}

	if _, err := client.Eval(ctx, artemis.EvalRequest{Host: host, Code: "print(1)"}); !errors.Is(err, artemis.ErrConsent) {
		t.Fatalf("eval without consent: %v", err)
	}
	evaluated, err := client.Eval(ctx, artemis.EvalRequest{Host: host, Code: "print(1)", ConfirmedArbitraryCode: true})
	if err != nil {
		t.Fatal(err)
	}
	if evaluated.Result != "42" || !slices.Contains(evaluated.Output, "hello") {
		t.Fatalf("eval: %+v", evaluated)
	}
	if _, err := client.Eval(ctx, artemis.EvalRequest{Host: host, Code: strings.Repeat("x", artemis.BodyLimit+1),
		ConfirmedArbitraryCode: true}); !errors.Is(err, artemis.ErrTooLarge) {
		t.Fatalf("an oversized Lua request must be refused locally: %v", err)
	}

	source := filepath.Join(folder, "upload.bin")
	if err := os.WriteFile(source, []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Put(ctx, artemis.PutRequest{Host: host, Path: "save:/new.bin", In: source}); !errors.Is(err, artemis.ErrConsent) {
		t.Fatalf("upload without consent: %v", err)
	}
	upload, err := client.Put(ctx, artemis.PutRequest{Host: host, Path: "save:/new.bin", In: source, ConfirmedMutation: true})
	if err != nil {
		t.Fatal(err)
	}
	if !upload.Verified || upload.Size != 11 || upload.SHA256 != sha256Hex("hello world") {
		t.Fatalf("upload: %+v", upload)
	}
	if !strings.HasPrefix(upload.Backup, filepath.Join(f.support, "artemis", "backups")+string(filepath.Separator)) {
		t.Fatalf("the pre-upload backup is not private: %q", upload.Backup)
	}
	if mode(t, upload.Backup) != 0o700 {
		t.Fatalf("the backup folder mode is %v", mode(t, upload.Backup))
	}
	if _, err := client.Put(ctx, artemis.PutRequest{Host: host, Path: "save:/core.lua", In: source,
		ConfirmedMutation: true}); !errors.Is(err, artemis.ErrSavePath) {
		t.Fatalf("a protected launcher file must never be written: %v", err)
	}

	backup, err := client.Backup(ctx, artemis.BackupRequest{Host: host, Files: []string{"save:/new.bin"}})
	if err != nil {
		t.Fatal(err)
	}
	if backup.Manifest.Scope != "explicit-files" || backup.Manifest.Complete || backup.Manifest.Files != 1 ||
		len(backup.Manifest.Entries) != 1 || backup.Manifest.Entries[0].SHA256 != sha256Hex("hello world") {
		t.Fatalf("backup: %+v", backup)
	}

	deletion, err := client.Remove(ctx, artemis.RemoveRequest{Host: host, Path: "save:/new.bin", ConfirmedMutation: true})
	if err != nil {
		t.Fatal(err)
	}
	if !deletion.Deleted {
		t.Fatalf("deletion: %+v", deletion)
	}
	restored, err := os.ReadFile(filepath.Join(deletion.Backup, "new.bin"))
	if err != nil || string(restored) != "hello world" {
		t.Fatalf("the pre-delete backup does not hold the deleted bytes: %q %v", restored, err)
	}
	if gone, err := client.Stat(ctx, host, "save:/new.bin"); err != nil || gone.Exists {
		t.Fatalf("the deleted file is still reported: %+v %v", gone, err)
	}
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func TestStartSeedsowFSGatesBeforeSpawn(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	support := canonical(t, filepath.Join(t.TempDir(), "support"))
	toolRoot := filepath.Join(support, "tools", "0.1.0")
	absent := artemis.NodeBinary(filepath.Join(t.TempDir(), "absent-node"))
	if _, err := artemis.StartSeedsowFS(context.Background(), artemis.SeedsowFSRequest{}, nil); err == nil {
		t.Fatal("a host needs a verified Node binary")
	}
	if _, err := artemis.StartSeedsowFS(context.Background(), artemis.SeedsowFSRequest{Node: absent,
		ConsoleIP: "127.0.0.1", HostIP: "192.168.1.2", Support: support, ToolRoot: toolRoot,
		Manifest: m}, nil); !errors.Is(err, artemis.ErrAddress) {
		t.Fatalf("a loopback console needs the fixture option: %v", err)
	}
	if _, err := artemis.StartSeedsowFS(context.Background(), artemis.SeedsowFSRequest{Node: absent,
		ConsoleIP: "192.168.1.55", HostIP: "127.0.0.1", Support: support, ToolRoot: toolRoot,
		Manifest: m}, nil); !errors.Is(err, artemis.ErrAddress) {
		t.Fatalf("a loopback computer address needs the fixture option: %v", err)
	}
	// The pinned host stages only beneath an absolute canonical root.
	real := canonical(t, filepath.Join(t.TempDir(), "real"))
	link := filepath.Join(filepath.Dir(real), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := artemis.StartSeedsowFS(context.Background(), artemis.SeedsowFSRequest{Node: absent,
		ConsoleIP: "127.0.0.1", HostIP: "127.0.0.1", Support: link, ToolRoot: toolRoot,
		Manifest: m, AllowLoopback: true}, nil); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("a symlinked support root must be refused: %v", err)
	}
	if _, err := artemis.StartSeedsowFS(context.Background(), artemis.SeedsowFSRequest{Node: absent,
		ConsoleIP: "127.0.0.1", HostIP: "127.0.0.1", Port: 70000, Support: support,
		ToolRoot: toolRoot, Manifest: m, AllowLoopback: true}, nil); err == nil {
		t.Fatal("an impossible port must be refused")
	}
	if _, err := artemis.StartSeedsowFS(context.Background(), artemis.SeedsowFSRequest{Node: absent,
		ConsoleIP: "127.0.0.1", HostIP: "127.0.0.1", Support: support, ToolRoot: toolRoot,
		Manifest: m, AllowLoopback: true}, nil); !errors.As(err, new(*artemis.PluginMissingError)) {
		t.Fatalf("the host needs the artemis-fs plugin: %v", err)
	}
}

func TestStartSeedsowFSStagesTheReadOnlyPluginCopy(t *testing.T) {
	f := newFixture(t)
	launcher := newMockLauncher(t)
	host, port := launcher.address(t)
	var mu sync.Mutex
	var events []artemis.SeedsowFSEvent
	session, err := artemis.StartSeedsowFS(context.Background(), artemis.SeedsowFSRequest{
		Node: artemis.NodeBinary(nodeBinary(t)), ToolRoot: f.toolRoot, Manifest: f.manifest, Support: f.support,
		ConsoleIP: host, HostIP: host, Port: port, AllowLoopback: true,
	}, func(event artemis.SeedsowFSEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	// No console answers the held RPC, so the host must report a failure rather
	// than a session; reaching that failure proves the stage prepared.
	if err := session.Wait(); !errors.Is(err, artemis.ErrHostFailed) {
		t.Fatalf("a fixture host must not report a verified session: %v", err)
	}
	if session.Held() || session.RunPath() != "" {
		t.Fatalf("the host reported a held session it never verified: %q %t", session.RunPath(), session.Held())
	}
	mu.Lock()
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Type)
	}
	messages := make([]string, 0, len(events))
	for _, event := range events {
		messages = append(messages, event.Message)
	}
	mu.Unlock()
	if !slices.Contains(kinds, "host-starting") || !slices.Contains(kinds, "preparing") || !slices.Contains(kinds, "failed") {
		t.Fatalf("frames: %v", kinds)
	}
	if slices.Contains(kinds, "held-ready") {
		t.Fatalf("the fixture host reported a held session: %v", messages)
	}

	runs := filepath.Join(f.support, "runs")
	hosts, err := os.ReadDir(runs)
	if err != nil || len(hosts) != 1 || !strings.HasPrefix(hosts[0].Name(), "artemis-fs-") {
		t.Fatalf("the host did not leave one private stage: %v %v", hosts, err)
	}
	portDir := filepath.Join(runs, hosts[0].Name(), "port")
	// The 0400/0500 plugin stage cannot take the SaveKit overlays or the engine's
	// own reports, so the copy must be widened and the plugin left untouched.
	if got := mode(t, portDir); got != 0o700 {
		t.Fatalf("the staged port directory is %v", got)
	}
	if got := mode(t, filepath.Join(portDir, "engine", "shared", "ipc.cjs")); got != 0o600 {
		t.Fatalf("the staged overlay file is %v", got)
	}
	if got := mode(t, filepath.Join(portDir, "engine", "run.cjs")); got != 0o600 {
		t.Fatalf("the staged engine file is %v", got)
	}
	marker, err := os.ReadFile(filepath.Join(portDir, ".mac2jb-artemis-fs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var prepared struct {
		Format       string `json:"format"`
		Target       string `json:"target"`
		SourceSha256 string `json:"sourceSha256"`
	}
	if err := json.Unmarshal(marker, &prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.Format != "mac2jb-artemis-fs-stage-v1" || prepared.Target != "artemis-seedsow" || len(prepared.SourceSha256) != 64 {
		t.Fatalf("the stage was not prepared by the pinned script: %s", marker)
	}
	pluginRoot := filepath.Join(f.toolRoot, "tools", artemis.ArtemisFSPlugin)
	runtime := filepath.Join(pluginRoot, "integrations", "artemis-fs", "runtime-host.cjs")
	if got := mode(t, runtime); got != 0o400 {
		t.Fatalf("the plugin file mode changed to %v", got)
	}
	if got := mode(t, filepath.Join(pluginRoot, "integrations", "artemis-fs", "kit", "port")); got != 0o500 {
		t.Fatalf("the plugin directory mode changed to %v", got)
	}
	body, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	component, err := f.manifest.ToolComponent(artemis.ArtemisFSPlugin)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(sum[:]) != component.Source.Embedded["integrations/artemis-fs/runtime-host.cjs"] {
		t.Fatal("the pinned runtime host no longer matches its recorded digest")
	}
	preload := filepath.Join(f.support, "artemis", ".cp-preload.cjs")
	if got := mode(t, preload); got != 0o600 {
		t.Fatalf("the private preload mode is %v", got)
	}
	if err := session.Stop(context.Background()); err != nil {
		t.Fatalf("stopping a finished host failed: %v", err)
	}
}

func TestStopEndsTheSeedsowHost(t *testing.T) {
	f := newFixture(t)
	launcher := newMockLauncher(t)
	launcher.bridge = make(chan struct{})
	t.Cleanup(func() { close(launcher.bridge) })
	host, port := launcher.address(t)
	var mu sync.Mutex
	var events []artemis.SeedsowFSEvent
	session, err := artemis.StartSeedsowFS(context.Background(), artemis.SeedsowFSRequest{
		Node: artemis.NodeBinary(nodeBinary(t)), ToolRoot: f.toolRoot, Manifest: f.manifest, Support: f.support,
		ConsoleIP: host, HostIP: host, Port: port, AllowLoopback: true,
	}, func(event artemis.SeedsowFSEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	// Wait until the host holds its bridge open, so Stop lands on a live host
	// instead of the startup window that stops cleanly.
	awaitMessage(t, &mu, &events, "Sending Seedsow bridge")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := session.Stop(ctx); err != nil {
		t.Fatalf("stopping the host failed: %v", err)
	}
	if err := session.Wait(); !errors.Is(err, artemis.ErrHostFailed) {
		t.Fatalf("a stop before verification must not report a session: %v", err)
	}
	if err := session.Stop(ctx); err != nil {
		t.Fatalf("stopping a finished host failed: %v", err)
	}
}

func awaitMessage(t *testing.T, mu *sync.Mutex, events *[]artemis.SeedsowFSEvent, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		mu.Lock()
		found := slices.ContainsFunc(*events, func(event artemis.SeedsowFSEvent) bool {
			return strings.Contains(event.Message, want)
		})
		mu.Unlock()
		if found {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the host never reported %q", want)
}
