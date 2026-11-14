package devtools_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/devtools"
	"github.com/thedavidweng/any2jb/internal/manifest"
)

// referenceEnv names the mac2jb v0.8.2 tree the integration tests stage from.
const referenceEnv = "ANY2JB_MAC2JB_REF"

func referenceRoot(t *testing.T) string {
	t.Helper()
	root := os.Getenv(referenceEnv)
	if root == "" {
		root = "/tmp/mac2jb-v082-ref"
	}
	if _, err := os.Stat(filepath.Join(root, "scripts", "gui-dnswitch.cjs")); err != nil {
		t.Skipf("the mac2jb v0.8.2 reference tree is not available at %s; set %s to run these tests", root, referenceEnv)
	}
	return root
}

func nodePath(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for the Node utility tests")
	}
	return node
}

func pythonPath(t *testing.T) string {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for the Python utility tests")
	}
	return python
}

// fixture stages plugin files into a private tool root the way the app does.
type fixture struct {
	ref      string
	toolRoot string
	support  string
	manifest *manifest.Manifest
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{ref: referenceRoot(t), toolRoot: t.TempDir(), support: t.TempDir(), manifest: m}
}

func (f *fixture) config() devtools.Config {
	cfg := devtools.Config{ToolRoot: f.toolRoot, Support: f.support, Manifest: f.manifest}
	if node, err := exec.LookPath("node"); err == nil {
		cfg.Node = node
	}
	return cfg
}

// stage copies reference files to <toolRoot>/tools/<id> at 0400, the read-only
// mode the app stages plugin trees with.
func (f *fixture) stage(t *testing.T, id string, rels ...string) string {
	t.Helper()
	dir := filepath.Join(f.toolRoot, "tools", id)
	for _, rel := range rels {
		f.write(t, filepath.Join(dir, filepath.FromSlash(rel)), filepath.Join(f.ref, filepath.FromSlash(rel)), 0o400)
	}
	return dir
}

// stageTree copies every file below one reference directory.
func (f *fixture) stageTree(t *testing.T, id, rel string) string {
	t.Helper()
	source := filepath.Join(f.ref, filepath.FromSlash(rel))
	err := filepath.WalkDir(source, func(name string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		inside, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		f.write(t, filepath.Join(f.toolRoot, "tools", id, filepath.FromSlash(rel), inside), name, 0o400)
		return nil
	})
	if err != nil {
		t.Fatalf("stage %s: %v", rel, err)
	}
	return filepath.Join(f.toolRoot, "tools", id)
}

func (f *fixture) write(t *testing.T, target, source string, mode os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read reference file %s: %v", source, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, mode); err != nil {
		t.Fatal(err)
	}
}

func TestPluginMissingNamesTheInstallCommand(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	cases := []struct {
		id  string
		run func(devtools.Config) error
	}{
		{"nxformat", func(c devtools.Config) error {
			_, err := c.RunNXFormat(ctx, devtools.NXFormatRequest{Action: devtools.NXFormatInfo, Input: "/nonexistent/x.nx"}, nil)
			return err
		}},
		{"nsp-box", func(c devtools.Config) error {
			_, err := c.BuildNSP(ctx, devtools.NSPBoxRequest{}, nil)
			return err
		}},
		{"nup", func(c devtools.Config) error {
			_, err := c.RunNUP(ctx, devtools.NUPRequest{Action: devtools.NUPInfo, Input: "/nonexistent/x.nup"})
			return err
		}},
		{"dnswitch", func(c devtools.Config) error {
			_, err := c.StartDNSwitch(ctx, devtools.DNSwitchRequest{Address: "127.0.0.1"}, nil)
			return err
		}},
		{"browser-probes", func(c devtools.Config) error {
			_, err := c.StartBrowserProbes(ctx, devtools.BrowserProbesRequest{Address: "127.0.0.1", Probe: "webkit"}, nil)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			err := tc.run(f.config())
			var missing *devtools.PluginMissingError
			if !errors.As(err, &missing) {
				t.Fatalf("expected PluginMissingError, got %v", err)
			}
			if missing.ID != tc.id {
				t.Fatalf("missing plugin id = %q, want %q", missing.ID, tc.id)
			}
			if !strings.Contains(err.Error(), "plugins install "+tc.id) {
				t.Fatalf("error must guide the user to install the plugin: %v", err)
			}
		})
	}
}

func TestTamperedPluginReportsIntegrityFailure(t *testing.T) {
	f := newFixture(t)
	dir := f.stage(t, "dnswitch", "scripts/gui-dnswitch.cjs")
	script := filepath.Join(dir, "scripts", "gui-dnswitch.cjs")
	if err := os.Chmod(script, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("'use strict';\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	_, err := f.config().StartDNSwitch(context.Background(), devtools.DNSwitchRequest{Address: "127.0.0.1"}, nil)
	var missing *devtools.PluginMissingError
	if !errors.As(err, &missing) || missing.ID != "dnswitch" {
		t.Fatalf("expected a dnswitch PluginMissingError, got %v", err)
	}
	if missing.Cause == nil || !strings.Contains(err.Error(), "integrity check failed") {
		t.Fatalf("a tampered plugin must report its integrity failure: %v", err)
	}
}

// mockNXConvert records its arguments and emits the @PROGRESS forms nxconvert
// uses. It writes the pack destination so the verify step has real output.
const mockNXConvert = `#!/bin/sh
dir=$(dirname "$0")
printf '%s\n' "$*" >> "$dir/calls.log"
case "$1" in
  pack-tree)
    echo "@PROGRESS stage=Reading percent=10 message=tree"
    printf 'nx-bytes' > "$3"
    echo "@PROGRESS stage=Writing percent=99 message=archive"
    ;;
  pack-nup)
    echo "@PROGRESS stage=Reading percent=10 message=nup"
    printf 'nx-from-nup' > "$3"
    ;;
  verify)
    echo "@PROGRESS verify 100 verified"
    echo "verified $2"
    ;;
  info)
    echo '{"format":"nx","version":1}'
    ;;
  contents)
    echo 'romfs/main'
    ;;
esac
exit 0
`

func (f *fixture) nxFormatConfig(t *testing.T) (devtools.Config, string) {
	t.Helper()
	f.stage(t, "nxformat", "packaging/NXFormat-source.json", "scripts/gui-nxformat.cjs")
	mockDir := t.TempDir()
	mock := filepath.Join(mockDir, "nxconvert")
	if err := os.WriteFile(mock, []byte(mockNXConvert), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := f.config()
	cfg.NXConvertPath = mock
	return cfg, filepath.Join(mockDir, "calls.log")
}

func TestNXFormatRejectsInvalidRequests(t *testing.T) {
	f := newFixture(t)
	cfg, _ := f.nxFormatConfig(t)
	ctx := context.Background()
	dump := t.TempDir()
	archive := filepath.Join(t.TempDir(), "game.nx")
	if err := os.WriteFile(archive, []byte("nx"), 0o600); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(t.TempDir(), "game.txt")
	if err := os.WriteFile(plain, []byte("plain"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		request devtools.NXFormatRequest
		want    string
	}{
		{"relative input", devtools.NXFormatRequest{Action: devtools.NXFormatInfo, Input: "game.nx"}, "choose an existing .nx file"},
		{"wrong extension", devtools.NXFormatRequest{Action: devtools.NXFormatVerify, Input: plain}, "choose an existing .nx file"},
		{"missing archive", devtools.NXFormatRequest{Action: devtools.NXFormatContents, Input: filepath.Join(t.TempDir(), "gone.nx")}, "missing or unsafe"},
		{"unknown action", devtools.NXFormatRequest{Action: "extract", Input: archive}, "unknown NXFormat operation"},
		{"pack existing destination", devtools.NXFormatRequest{Action: devtools.NXFormatPack, Input: dump, Output: archive}, "already exists"},
		{"pack wrong source", devtools.NXFormatRequest{Action: devtools.NXFormatPack, Input: plain, Output: filepath.Join(t.TempDir(), "out.nx")}, "dump folder or a .nup file"},
		{"pack missing destination", devtools.NXFormatRequest{Action: devtools.NXFormatPack, Input: dump, Output: filepath.Join(t.TempDir(), "missing", "out.nx")}, "destination folder is missing"},
		{"pack wrong destination", devtools.NXFormatRequest{Action: devtools.NXFormatPack, Input: dump, Output: filepath.Join(t.TempDir(), "out.zip")}, "choose a .nx destination"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cfg.RunNXFormat(ctx, tc.request, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestNXFormatPackStreamsProgressAndVerifies(t *testing.T) {
	f := newFixture(t)
	cfg, callsPath := f.nxFormatConfig(t)
	dump := t.TempDir()
	if err := os.WriteFile(filepath.Join(dump, "main"), []byte("exefs"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "out.nx")
	var progress []devtools.Progress
	result, err := cfg.RunNXFormat(context.Background(),
		devtools.NXFormatRequest{Action: devtools.NXFormatPack, Input: dump, Output: destination},
		func(report devtools.Progress) { progress = append(progress, report) })
	if err != nil {
		t.Fatal(err)
	}
	if result.Saved != destination {
		t.Fatalf("saved = %q, want %q", result.Saved, destination)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "nx-bytes" {
		t.Fatalf("destination = %q, %v", data, err)
	}
	if len(progress) < 3 || progress[0].Stage != "Reading" || progress[0].Percent != 10 {
		t.Fatalf("progress = %+v", progress)
	}
	if progress[len(progress)-1].Stage != "verify" || progress[len(progress)-1].Percent != 100 {
		t.Fatalf("verify progress = %+v", progress[len(progress)-1])
	}
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if !strings.HasPrefix(lines[0], "pack-tree "+dump) ||
		!strings.HasSuffix(lines[0], " --platform switch2 --no-trail --frontend") {
		t.Fatalf("pack arguments = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "verify ") {
		t.Fatalf("verify arguments = %q", lines[1])
	}
	if leftovers, err := filepath.Glob(destination + ".mac2jb-*.partial.nx"); err != nil || len(leftovers) != 0 {
		t.Fatalf("the partial archive must be removed: %v %v", leftovers, err)
	}
}

func TestNXFormatPacksAnExistingNUP(t *testing.T) {
	f := newFixture(t)
	cfg, _ := f.nxFormatConfig(t)
	nup := filepath.Join(t.TempDir(), "game.nup")
	if err := os.WriteFile(nup, []byte("nup"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "out.nx")
	result, err := cfg.RunNXFormat(context.Background(),
		devtools.NXFormatRequest{Action: devtools.NXFormatPack, Input: nup, Output: destination}, nil)
	if err != nil || result.Saved != destination {
		t.Fatalf("pack a .nup: %+v %v", result, err)
	}
}

func TestNXFormatInspectReturnsToolOutput(t *testing.T) {
	f := newFixture(t)
	cfg, _ := f.nxFormatConfig(t)
	archive := filepath.Join(t.TempDir(), "game.nx")
	if err := os.WriteFile(archive, []byte("nx"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := cfg.RunNXFormat(context.Background(),
		devtools.NXFormatRequest{Action: devtools.NXFormatInfo, Input: archive}, nil)
	if err != nil || !strings.Contains(result.Output, `"format":"nx"`) {
		t.Fatalf("info: %+v %v", result, err)
	}
	if status, err := cfg.RunNXFormat(context.Background(), devtools.NXFormatRequest{Action: devtools.NXFormatStatus}, nil); err != nil || status.Output == "" {
		t.Fatalf("status: %+v %v", status, err)
	}
}

// nspBoxFixture replaces the staged adapter with a deterministic event stream
// so the Go side is exercised without Pillow.
const nspBoxFixture = `import json, os, signal, sys, time

cancelled = False


def handle(*_):
    global cancelled
    cancelled = True


signal.signal(signal.SIGTERM, handle)


def emit(kind, **values):
    print(json.dumps({"type": kind, **values}), flush=True)


output = sys.argv[sys.argv.index("--output") + 1]
emit("progress", stage="Checking source files", percent=12.5)
if os.environ.get("DEVTOOLS_FIXTURE_SLOW") == "1":
    while not cancelled:
        time.sleep(0.02)
    emit("cancelled", message="Package build canceled; the partial NSP was removed.")
    raise SystemExit(130)
emit("progress", stage="Building package", percent=80)
emit("result", report={"output": output, "title": "Fixture", "title_id": "0100000000010000"})
`

// pythonShim answers the PIL import check and forwards everything else to the
// real interpreter, standing in for the portable runtime in tests.
const pythonShim = `#!/bin/sh
if [ "$3" = "import PIL" ]; then
  exit 0
fi
exec '%s' "$@"
`

func (f *fixture) nspBoxConfig(t *testing.T) devtools.Config {
	t.Helper()
	dir := f.stageTree(t, "nsp-box", "integrations/nsp-box")
	f.overwrite(t, filepath.Join(dir, "integrations", "nsp-box", "cli.py"), []byte(nspBoxFixture))
	shim := filepath.Join(t.TempDir(), "python3")
	if err := os.WriteFile(shim, []byte(fmt.Sprintf(pythonShim, pythonPath(t))), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := f.config()
	cfg.HomebrewPython = shim
	return cfg
}

func (f *fixture) overwrite(t *testing.T, target string, data []byte) {
	t.Helper()
	if err := os.Chmod(target, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o400); err != nil {
		t.Fatal(err)
	}
}

func TestNSPBoxBuildsAndReportsEvents(t *testing.T) {
	f := newFixture(t)
	cfg := f.nspBoxConfig(t)
	source := t.TempDir()
	output := filepath.Join(t.TempDir(), "homebrew.nsp")
	var events []devtools.NSPBoxEvent
	result, err := cfg.BuildNSP(context.Background(), devtools.NSPBoxRequest{Source: source, Output: output, Title: "Fixture"},
		func(event devtools.NSPBoxEvent) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Type != "progress" || events[0].Percent != 12.5 || events[0].Stage != "Checking source files" {
		t.Fatalf("events = %+v", events)
	}
	if events[2].Type != "result" {
		t.Fatalf("last event = %+v", events[2])
	}
	var report struct{ Output, Title string }
	if err := json.Unmarshal(result.Report, &report); err != nil || report.Output != output || report.Title != "Fixture" {
		t.Fatalf("report = %s, %v", result.Report, err)
	}
}

func TestNSPBoxCancelsCleanly(t *testing.T) {
	f := newFixture(t)
	cfg := f.nspBoxConfig(t)
	t.Setenv("DEVTOOLS_FIXTURE_SLOW", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	started := time.Now()
	result, err := cfg.BuildNSP(ctx, devtools.NSPBoxRequest{Source: t.TempDir(), Output: filepath.Join(t.TempDir(), "cancel.nsp")}, nil)
	if !result.Cancelled {
		t.Fatalf("a cancelled build must be reported as cancelled: %+v", result)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("cancellation took %v", elapsed)
	}
}

func TestNSPBoxRejectsInvalidRequests(t *testing.T) {
	f := newFixture(t)
	cfg := f.nspBoxConfig(t)
	source := t.TempDir()
	existing := filepath.Join(t.TempDir(), "taken.nsp")
	if err := os.WriteFile(existing, []byte("nsp"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		request devtools.NSPBoxRequest
		want    string
	}{
		{"missing source", devtools.NSPBoxRequest{Source: filepath.Join(t.TempDir(), "gone"), Output: filepath.Join(t.TempDir(), "a.nsp")}, "source folder is missing"},
		{"relative source", devtools.NSPBoxRequest{Source: "dump", Output: filepath.Join(t.TempDir(), "a.nsp")}, "choose a source folder"},
		{"wrong output", devtools.NSPBoxRequest{Source: source, Output: filepath.Join(t.TempDir(), "a.zip")}, ".nsp output filename"},
		{"existing output", devtools.NSPBoxRequest{Source: source, Output: existing}, "already exists"},
		{"missing output folder", devtools.NSPBoxRequest{Source: source, Output: filepath.Join(t.TempDir(), "gone", "a.nsp")}, "destination folder is missing"},
		{"short title id", devtools.NSPBoxRequest{Source: source, Output: filepath.Join(t.TempDir(), "a.nsp"), DLC: true, TitleID: "0100"}, "16 hexadecimal digits"},
		{"long title", devtools.NSPBoxRequest{Source: source, Output: filepath.Join(t.TempDir(), "a.nsp"), Title: strings.Repeat("t", 201)}, "at most 200 characters"},
		{"relative artwork", devtools.NSPBoxRequest{Source: source, Output: filepath.Join(t.TempDir(), "a.nsp"), Artwork: "icon.png"}, "choose an artwork image"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cfg.BuildNSP(context.Background(), tc.request, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestNSPBoxReportsMissingPillow(t *testing.T) {
	f := newFixture(t)
	f.stageTree(t, "nsp-box", "integrations/nsp-box")
	if err := exec.Command(pythonPath(t), "-c", "import PIL").Run(); err == nil {
		t.Skip("Pillow is installed, so the missing-module path cannot be exercised")
	}
	_, err := f.config().BuildNSP(context.Background(),
		devtools.NSPBoxRequest{Source: t.TempDir(), Output: filepath.Join(t.TempDir(), "a.nsp")}, nil)
	if err == nil || !strings.Contains(err.Error(), "PIL") || !strings.Contains(err.Error(), "Pillow") {
		t.Fatalf("error = %v, want an actionable Pillow message", err)
	}
}

// nupFixture implements the nup.cli surface the wrapper drives, including the
// argument order mac2jb uses.
const nupFixture = `import json, os, pathlib, sys

args = sys.argv[1:]
action = args[0]
log = os.environ.get("DEVTOOLS_FIXTURE_LOG")
if log:
    with open(log, "a") as handle:
        handle.write(" ".join(args) + "\n")
if action == "pack":
    pathlib.Path(args[2]).write_text("nup-fixture")
    print("packed " + args[3])
elif action == "verify":
    print("verified " + args[1])
elif action in ("info", "list"):
    print(json.dumps({"format": "nup", "files": 1}))
elif action == "extract":
    target = pathlib.Path(args[2])
    target.mkdir(parents=True)
    (target / "main").write_text("exefs")
    print("extracted " + args[2])
else:
    raise SystemExit("unknown action")
`

func (f *fixture) nupConfig(t *testing.T) devtools.Config {
	t.Helper()
	f.stage(t, "nup", "packaging/NUP-source.json", "scripts/gui-nup.cjs")
	moduleRoot := t.TempDir()
	module := filepath.Join(moduleRoot, "nup")
	if err := os.MkdirAll(module, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "cli.py"), []byte(nupFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := f.config()
	cfg.NUPModuleRoot = moduleRoot
	return cfg
}

func requireZstandard(t *testing.T) {
	t.Helper()
	if err := exec.Command(pythonPath(t), "-c", "import zstandard").Run(); err != nil {
		t.Skip("the host python3 has no zstandard module")
	}
}

func TestNUPOperations(t *testing.T) {
	requireZstandard(t)
	f := newFixture(t)
	cfg := f.nupConfig(t)
	logPath := filepath.Join(t.TempDir(), "calls.log")
	t.Setenv("DEVTOOLS_FIXTURE_LOG", logPath)
	dump := t.TempDir()
	if err := os.WriteFile(filepath.Join(dump, "main"), []byte("exefs"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "dump.nup")
	packed, err := cfg.RunNUP(context.Background(), devtools.NUPRequest{Action: devtools.NUPPack, Input: dump, Output: archive})
	if err != nil {
		t.Fatal(err)
	}
	if packed.Path != archive {
		t.Fatalf("packed path = %q, want %q", packed.Path, archive)
	}
	if data, err := os.ReadFile(archive); err != nil || string(data) != "nup-fixture" {
		t.Fatalf("archive = %q, %v", data, err)
	}
	if list, err := cfg.RunNUP(context.Background(), devtools.NUPRequest{Action: devtools.NUPList, Input: archive}); err != nil || !strings.Contains(list.Output, `"format": "nup"`) {
		t.Fatalf("list: %+v %v", list, err)
	}
	target := filepath.Join(t.TempDir(), "extracted")
	extracted, err := cfg.RunNUP(context.Background(), devtools.NUPRequest{Action: devtools.NUPExtract, Input: archive, Output: target})
	if err != nil || extracted.Path != target {
		t.Fatalf("extract: %+v %v", extracted, err)
	}
	if data, err := os.ReadFile(filepath.Join(target, "main")); err != nil || string(data) != "exefs" {
		t.Fatalf("extracted = %q, %v", data, err)
	}
	if _, err := cfg.RunNUP(context.Background(), devtools.NUPRequest{Action: devtools.NUPVerify, Input: archive}); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if !strings.HasPrefix(lines[0], "pack "+dump+" ") || !strings.HasSuffix(lines[0], " --quiet") {
		t.Fatalf("pack arguments = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "verify ") {
		t.Fatalf("verify arguments = %q", lines[1])
	}
}

func TestNUPRejectsInvalidRequests(t *testing.T) {
	requireZstandard(t)
	f := newFixture(t)
	cfg := f.nupConfig(t)
	ctx := context.Background()
	dump := t.TempDir()
	archive := filepath.Join(t.TempDir(), "dump.nup")
	if err := os.WriteFile(archive, []byte("nup"), 0o600); err != nil {
		t.Fatal(err)
	}
	existing := t.TempDir()
	cases := []struct {
		name    string
		request devtools.NUPRequest
		want    string
	}{
		{"unknown action", devtools.NUPRequest{Action: "delete", Input: archive}, "unknown NUP operation"},
		{"missing archive", devtools.NUPRequest{Action: devtools.NUPInfo, Input: filepath.Join(t.TempDir(), "gone.nup")}, "missing or unsafe"},
		{"relative archive", devtools.NUPRequest{Action: devtools.NUPVerify, Input: "dump.nup"}, "choose an existing .nup file"},
		{"pack existing output", devtools.NUPRequest{Action: devtools.NUPPack, Input: dump, Output: archive}, "destination already exists"},
		{"pack relative output", devtools.NUPRequest{Action: devtools.NUPPack, Input: dump, Output: "dump.nup"}, "choose a .nup destination"},
		{"pack missing source", devtools.NUPRequest{Action: devtools.NUPPack, Input: filepath.Join(t.TempDir(), "gone"), Output: filepath.Join(t.TempDir(), "a.nup")}, "dump folder is missing"},
		{"extract existing folder", devtools.NUPRequest{Action: devtools.NUPExtract, Input: archive, Output: existing}, "extraction folder already exists"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := cfg.RunNUP(ctx, tc.request); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

// eventLog collects events delivered on a child's consume goroutine.
type eventLog[T any] struct {
	mu     sync.Mutex
	events []T
}

func (l *eventLog[T]) add(event T) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *eventLog[T]) snapshot() []T {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]T(nil), l.events...)
}

func (l *eventLog[T]) waitFor(timeout time.Duration, match func(T) bool) (T, bool) {
	var zero T
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, event := range l.snapshot() {
			if match(event) {
				return event, true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return zero, false
}

func TestDNSwitchServesLoopback(t *testing.T) {
	f := newFixture(t)
	f.stage(t, "dnswitch", "scripts/gui-dnswitch.cjs")
	cfg := f.config()
	cfg.Node = nodePath(t)
	var events eventLog[devtools.DNSwitchEvent]
	session, err := cfg.StartDNSwitch(context.Background(), devtools.DNSwitchRequest{
		Address:  "127.0.0.1",
		Hostname: devtools.DefaultDNSwitchHostname,
		DNSPort:  0,
		HTTPPort: 0,
	}, events.add)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := session.Stop(ctx); err != nil {
			t.Errorf("stop: %v", err)
		}
	}()
	if _, ok := events.waitFor(2*time.Second, func(event devtools.DNSwitchEvent) bool { return event.Type == "ready" }); !ok {
		t.Fatalf("no ready event: %+v", events.snapshot())
	}
	state := session.State()
	if state.Phase != "ready" || state.Address != "127.0.0.1" || state.DNSPort == 0 || state.HTTPPort == 0 || state.PID == 0 {
		t.Fatalf("state = %+v", state)
	}
	if state.Hostname != devtools.DefaultDNSwitchHostname {
		t.Fatalf("hostname = %q", state.Hostname)
	}
	if status, err := cfg.StatusDNSwitch(context.Background()); err != nil || status.Phase != "ready" || status.PID != state.PID {
		t.Fatalf("status = %+v, %v", status, err)
	}
	response := getPage(t, fmt.Sprintf("http://127.0.0.1:%d/", state.HTTPPort))
	if !strings.Contains(response, "cpypwn") {
		t.Fatalf("portal page = %q", response)
	}
	query := dnsQuery(devtools.DefaultDNSwitchHostname)
	reply := dnsRoundTrip(t, state.DNSPort, query)
	if reply[3]&0x0f != 0 || binary.BigEndian.Uint16(reply[6:]) != 1 {
		t.Fatalf("unexpected DNS reply: %x", reply)
	}
	if answer := reply[len(reply)-4:]; answer[0] != 127 || answer[1] != 0 || answer[2] != 0 || answer[3] != 1 {
		t.Fatalf("answer = %v", answer)
	}
	event, ok := events.waitFor(2*time.Second, func(event devtools.DNSwitchEvent) bool { return event.Type == "dns" })
	if !ok || event.Name != devtools.DefaultDNSwitchHostname || event.QType != 1 {
		t.Fatalf("dns event = %+v", event)
	}
}

func TestDNSwitchStatusAfterStop(t *testing.T) {
	f := newFixture(t)
	f.stage(t, "dnswitch", "scripts/gui-dnswitch.cjs")
	cfg := f.config()
	cfg.Node = nodePath(t)
	session, err := cfg.StartDNSwitch(context.Background(), devtools.DNSwitchRequest{Address: "127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := session.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if status, err := cfg.StatusDNSwitch(context.Background()); err != nil || status.Phase != "stopped" {
		t.Fatalf("status after stop = %+v, %v", status, err)
	}
	if err := cfg.StopDNSwitch(context.Background()); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("stopping a stopped helper must fail clearly: %v", err)
	}
}

func TestDNSwitchRejectsInvalidRequests(t *testing.T) {
	f := newFixture(t)
	f.stage(t, "dnswitch", "scripts/gui-dnswitch.cjs")
	cfg := f.config()
	cfg.Node = nodePath(t)
	cases := []struct {
		name    string
		request devtools.DNSwitchRequest
		want    string
	}{
		{"ipv6", devtools.DNSwitchRequest{Address: "::1"}, "choose an IPv4 address"},
		{"broadcast", devtools.DNSwitchRequest{Address: "255.255.255.255"}, "usable Mac LAN IPv4 address"},
		{"upper hostname", devtools.DNSwitchRequest{Address: "127.0.0.1", Hostname: "CTEST.example"}, "hostname is invalid"},
		{"bad url", devtools.DNSwitchRequest{Address: "127.0.0.1", URL: "ftp://example.com"}, "HTTP or HTTPS redirect URL"},
		{"port range", devtools.DNSwitchRequest{Address: "127.0.0.1", DNSPort: 70000}, "between 0 and 65535"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := cfg.StartDNSwitch(context.Background(), tc.request, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestBrowserProbesServesReports(t *testing.T) {
	f := newFixture(t)
	f.stageTree(t, "browser-probes", "integrations/browser-probes")
	dir := f.stage(t, "browser-probes", "scripts/gui-bundled-browser-probes.cjs")
	if info, err := os.Lstat(filepath.Join(dir, "integrations", "browser-probes", "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("staged probe pages: %v", err)
	}
	cfg := f.config()
	cfg.Node = nodePath(t)
	var events eventLog[devtools.BrowserProbesEvent]
	session, err := cfg.StartBrowserProbes(context.Background(), devtools.BrowserProbesRequest{
		Address: "127.0.0.1",
		Port:    0,
		Probe:   "webkit",
	}, events.add)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := session.Stop(ctx); err != nil {
			t.Errorf("stop: %v", err)
		}
	}()
	if _, ok := events.waitFor(2*time.Second, func(event devtools.BrowserProbesEvent) bool { return event.Type == "ready" }); !ok {
		t.Fatalf("no ready event: %+v", events.snapshot())
	}
	if session.Probe() != "webkit" || !strings.Contains(session.URL(), "probe=webkit") {
		t.Fatalf("session url = %q", session.URL())
	}
	page := getPage(t, session.URL())
	if !strings.Contains(page, "Browser probes") {
		t.Fatalf("probe page = %q", page)
	}
	report := map[string]any{
		"probe":     "webkit",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"userAgent": "any2jb-test",
		"result":    map[string]any{"webgl": true},
	}
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	status := postReport(t, strings.Split(session.URL(), "?")[0]+"report", body)
	if status != http.StatusCreated {
		t.Fatalf("report status = %d", status)
	}
	entries, err := os.ReadDir(session.ReportFolder())
	if err != nil || len(entries) != 1 {
		t.Fatalf("reports = %v, %v", entries, err)
	}
	saved, err := os.ReadFile(filepath.Join(session.ReportFolder(), entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		ReceivedAt string `json:"receivedAt"`
		Probe      string `json:"probe"`
	}
	if err := json.Unmarshal(saved, &stored); err != nil || stored.Probe != "webkit" || stored.ReceivedAt == "" {
		t.Fatalf("report = %s, %v", saved, err)
	}
}

func TestBrowserProbesRejectsInvalidRequests(t *testing.T) {
	f := newFixture(t)
	f.stageTree(t, "browser-probes", "integrations/browser-probes")
	f.stage(t, "browser-probes", "scripts/gui-bundled-browser-probes.cjs")
	cfg := f.config()
	cfg.Node = nodePath(t)
	cases := []struct {
		name    string
		request devtools.BrowserProbesRequest
		want    string
	}{
		{"ipv6", devtools.BrowserProbesRequest{Address: "::1", Probe: "webkit"}, "choose a Mac IPv4 address"},
		{"unknown probe", devtools.BrowserProbesRequest{Address: "127.0.0.1", Probe: "storage"}, "seven bundled browser probes"},
		{"port range", devtools.BrowserProbesRequest{Address: "127.0.0.1", Probe: "fonts", Port: 70000}, "port from 0 to 65535"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := cfg.StartBrowserProbes(context.Background(), tc.request, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func getPage(t *testing.T, url string) string {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", url, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func postReport(t *testing.T, url string, body []byte) int {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}

// dnsQuery builds one A/IN question with recursion desired.
func dnsQuery(name string) []byte {
	packet := make([]byte, 12)
	binary.BigEndian.PutUint16(packet[0:], 0x1234)
	binary.BigEndian.PutUint16(packet[2:], 0x0100)
	binary.BigEndian.PutUint16(packet[4:], 1)
	for _, label := range strings.Split(name, ".") {
		packet = append(packet, byte(len(label)))
		packet = append(packet, label...)
	}
	packet = append(packet, 0)
	packet = binary.BigEndian.AppendUint16(packet, 1)
	packet = binary.BigEndian.AppendUint16(packet, 1)
	return packet
}

func dnsRoundTrip(t *testing.T, port int, query []byte) []byte {
	t.Helper()
	connection, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write(query); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1500)
	read, err := connection.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	return buffer[:read]
}
