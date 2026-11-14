package civswitch

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/manifest"
)

// fsHostFixture is a staged civswitch-fs plugin: the supervisor script and the
// pinned port it copies, written read-only the way StageTools writes them.
const fsHostFixture = `'use strict';
const fs = require('node:fs/promises');
const path = require('node:path');
const crypto = require('node:crypto');
const [consoleIp, macIp, runtimeRoot] = process.argv.slice(2);
const emit = event => process.stdout.write('MAC2JB_CIVSWITCH_FS ' + JSON.stringify(event) + '\n');
(async () => {
  emit({type: 'preparing', message: 'fixture', console: consoleIp});
  const runs = path.join(runtimeRoot, 'runs');
  await fs.mkdir(runs, {recursive: true, mode: 0o700});
  const root = path.join(runs, 'civswitch-fs-' + crypto.randomBytes(8).toString('hex'));
  await fs.mkdir(root, {mode: 0o700});
  const port = path.join(root, 'port');
  await fs.cp(path.join(__dirname, 'fs-kit', 'port'), port, {recursive: true, errorOnExist: true, force: false});
  const bridge = path.join(port, 'civ_bridge.js');
  const original = await fs.readFile(bridge, 'utf8');
  await fs.writeFile(bridge, original.replace("<PC_IP>", macIp), {mode: 0o600});
  await fs.writeFile(path.join(port, 'engine/shared/ipc.cjs'), 'patched\n', {mode: 0o600});
  const runPath = path.join(port, 'engine', 'runs', 'switch2-abc123');
  await fs.mkdir(runPath, {recursive: true, mode: 0o700});
  const timer = setInterval(() => {}, 1000);
  process.on('SIGINT', () => {
    emit({type: 'stopped', message: 'CivSwitch FS host stopped', runPath});
    clearInterval(timer);
  });
  emit({type: 'host-starting', stagePath: root, message: 'Waiting for the CivSwitch FS host'});
  emit({type: 'progress', message: '4/4 verified.'});
  emit({type: 'held-ready', runPath, target: 'civ7', message: 'CivSwitch FS and RPC verified'});
})().catch(error => {
  emit({type: 'failed', message: error.message});
  process.exitCode = 1;
});
`

type fsFixture struct {
	request FSHostRequest
	stage   string
	support string
}

func newFSHostFixture(t *testing.T) *fsFixture {
	t.Helper()
	return newFSHostFixtureWith(t, fsHostFixture)
}

func newFSHostFixtureWith(t *testing.T, supervisor string) *fsFixture {
	t.Helper()
	root, support := t.TempDir(), t.TempDir()
	toolRoot := filepath.Join(root, "tools-version")
	stage := filepath.Join(toolRoot, "tools", "civswitch-fs")
	files := map[string]string{
		"integrations/civswitch/fs-host.cjs":                          supervisor,
		"integrations/civswitch/fs-kit/port/civ_bridge.js":            "module.exports = {PC_IP: '<PC_IP>'};\n",
		"integrations/civswitch/fs-kit/port/engine/shared/ipc.cjs":    "original\n",
		"integrations/civswitch/fs-kit/port/engine/shared/target.cjs": "exports.loadTarget = () => ({});\n",
	}
	embedded := map[string]string{}
	extra := map[string]string{}
	for name, body := range files {
		target := filepath.Join(stage, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0o400); err != nil {
			t.Fatal(err)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
		if name == "integrations/civswitch/fs-host.cjs" {
			extra[name] = digest
		} else {
			embedded[name] = digest
		}
	}
	component := manifest.Component{
		ID:           fsHostComponent,
		Path:         "integrations/civswitch/fs-kit/port/civ_bridge.js",
		SHA256:       embedded["integrations/civswitch/fs-kit/port/civ_bridge.js"],
		Distribution: manifest.Distribution{Decision: "may-ship"},
		Source: &manifest.ToolSource{
			Directory: "integrations/civswitch/fs-kit",
			Format:    "embedded",
			Files:     len(embedded),
			Embedded:  embedded,
			Extra:     extra,
		},
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("the CivSwitch FS supervisor needs Node")
	}
	return &fsFixture{
		request: FSHostRequest{
			Manifest:      &manifest.Manifest{Components: []manifest.Component{component}},
			ToolRoot:      toolRoot,
			NodeBinary:    node,
			Support:       support,
			SwitchIPv4:    "192.0.2.2",
			HostIPv4:      "192.168.1.10",
			WaitForLoader: false,
		},
		stage:   stage,
		support: support,
	}
}

func TestFSHostPreloadMakesTheReadOnlyStageCopyWritable(t *testing.T) {
	fixture := newFSHostFixture(t)
	var mu sync.Mutex
	var events []FSHostEvent
	held := make(chan FSHostEvent, 1)
	run, err := StartFSHost(context.Background(), fixture.request, func(event FSHostEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
		if event.Type == "held-ready" {
			select {
			case held <- event:
			default:
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	var ready FSHostEvent
	select {
	case ready = <-held:
	case <-time.After(30 * time.Second):
		t.Fatalf("the supervisor never held: %v", run.Wait())
	}
	port := filepath.Dir(filepath.Dir(filepath.Dir(ready.RunPath)))
	if !strings.HasPrefix(ready.RunPath, filepath.Join(fixture.support, "runs", "civswitch-fs-")) ||
		filepath.Base(filepath.Dir(ready.RunPath)) != "runs" ||
		!strings.HasPrefix(filepath.Base(ready.RunPath), "switch2-") {
		t.Fatalf("held run path: %s", ready.RunPath)
	}
	if run.RunPath() != ready.RunPath {
		t.Fatalf("session run path: %s", run.RunPath())
	}
	// The copied port is writable: the supervisor rewrote two staged 0400 files.
	bridge, err := os.ReadFile(filepath.Join(port, "civ_bridge.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bridge), "192.168.1.10") {
		t.Fatalf("staged bridge was not rewritten: %q", bridge)
	}
	for name, want := range map[string]string{"engine/shared/ipc.cjs": "patched\n", "engine/shared/target.cjs": "exports.loadTarget = () => ({});\n"} {
		file := filepath.Join(port, filepath.FromSlash(name))
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("staged %s: %q", name, data)
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s kept its read-only stage mode: %v", name, info.Mode())
		}
	}
	// The verified plugin stage itself keeps the read-only modes its digests were
	// recorded for; only the copy under runs/ was widened.
	staged, err := os.Stat(filepath.Join(fixture.stage, "integrations/civswitch/fs-kit/port/engine/shared/ipc.cjs"))
	if err != nil {
		t.Fatal(err)
	}
	if staged.Mode().Perm() != 0o400 {
		t.Fatalf("the plugin stage was modified: %v", staged.Mode())
	}
	if err := run.Stop(context.Background()); err != nil {
		t.Fatalf("a stopped supervisor did not exit cleanly: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !sawEvent(events, "stopped") {
		t.Fatalf("the supervisor did not report its stop: %+v", events)
	}
}

func sawEvent(events []FSHostEvent, kind string) bool {
	for _, event := range events {
		if event.Type == kind {
			return true
		}
	}
	return false
}

// The preload exists only because the staged tree is read-only: this pins the
// failure it prevents, so a future preload change cannot pass by accident.
func TestStagedPluginCopyIsNotWritableWithoutThePreload(t *testing.T) {
	fixture := newFSHostFixture(t)
	script := filepath.Join(t.TempDir(), "copy.cjs")
	body := `'use strict';
const fs = require('node:fs/promises');
const path = require('node:path');
const [source, target] = process.argv.slice(2);
(async () => {
  await fs.cp(source, target, {recursive: true, errorOnExist: true, force: false});
  await fs.writeFile(path.join(target, 'engine/shared/ipc.cjs'), 'patched\n', {mode: 0o600});
})().catch(error => {
  process.stderr.write(error.code + '\n');
  process.exitCode = 1;
});
`
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(fixture.stage, "integrations/civswitch/fs-kit/port")
	runs := filepath.Join(fixture.support, "runs")
	preload, err := writeCopyPreload(fixture.support)
	if err != nil {
		t.Fatal(err)
	}
	if err := runNode(t, fixture.request.NodeBinary, nil, script, source, filepath.Join(runs, "plain")); err == nil {
		t.Fatal("the read-only stage copy was writable without the preload")
	}
	if err := runNode(t, fixture.request.NodeBinary, []string{"ANY2JB_RUNS_ROOT=" + runs},
		"--require", preload, script, source, filepath.Join(runs, "relaxed")); err != nil {
		t.Fatalf("the preload did not relax the staged copy: %v", err)
	}
}

func runNode(t *testing.T, node string, environment []string, args ...string) error {
	t.Helper()
	cmd := exec.Command(node, args...)
	cmd.Env = append(os.Environ(), environment...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func TestFSHostRefusesUnstagedPluginsAndNonLANPeers(t *testing.T) {
	fixture := newFSHostFixture(t)
	cases := map[string]func(*FSHostRequest){
		"switch loopback":   func(r *FSHostRequest) { r.SwitchIPv4 = "127.0.0.1" },
		"switch link local": func(r *FSHostRequest) { r.SwitchIPv4 = "169.254.1.1" },
		"host public":       func(r *FSHostRequest) { r.HostIPv4 = "192.0.2.9" },
		"host empty":        func(r *FSHostRequest) { r.HostIPv4 = "" },
		"no tool root":      func(r *FSHostRequest) { r.ToolRoot = "" },
		"relative root":     func(r *FSHostRequest) { r.ToolRoot = "tools" },
		"no node":           func(r *FSHostRequest) { r.NodeBinary = "" },
		"node not a file":   func(r *FSHostRequest) { r.NodeBinary = t.TempDir() },
		"plugin missing":    func(r *FSHostRequest) { r.ToolRoot = filepath.Join(t.TempDir(), "empty") },
		"no manifest":       func(r *FSHostRequest) { r.Manifest = nil },
		"support relative":  func(r *FSHostRequest) { r.Support = "support" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			request := fixture.request
			mutate(&request)
			run, err := StartFSHost(context.Background(), request, nil)
			if err == nil {
				run.Stop(context.Background())
				t.Fatal("started the supervisor with an unusable request")
			}
		})
	}
}

func TestFSHostRefusesAChangedStagedFile(t *testing.T) {
	fixture := newFSHostFixture(t)
	tampered := filepath.Join(fixture.stage, "integrations/civswitch/fs-kit/port/engine/shared/ipc.cjs")
	if err := os.Chmod(tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tampered, []byte("tampered\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	run, err := StartFSHost(context.Background(), fixture.request, nil)
	if err == nil {
		run.Stop(context.Background())
		t.Fatal("ran a supervisor whose staged port changed")
	}
	if !strings.Contains(err.Error(), "failed verification") {
		t.Fatalf("changed stage error: %v", err)
	}
}

// A supervisor that exits without the pinned stopped event is not a clean stop,
// even when the process exit code is zero.
func TestFSHostRequiresAConfirmedStop(t *testing.T) {
	fixture := newFSHostFixtureWith(t, `'use strict';
process.stdout.write('MAC2JB_CIVSWITCH_FS ' + JSON.stringify({type: 'preparing', message: 'fixture'}) + '\n');
`)
	run, err := StartFSHost(context.Background(), fixture.request, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Wait(); err == nil {
		t.Fatal("a supervisor that stopped without confirming it was reported as clean")
	}
	if err := run.Stop(context.Background()); err == nil {
		t.Fatal("a stopped supervisor reported success after the fact")
	}
}
