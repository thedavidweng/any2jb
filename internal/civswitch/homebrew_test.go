package civswitch

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/thedavidweng/any2jb/internal/manifest"
)

// The fixture plugin scripts below are test doubles for the pinned CivHomebrew
// scripts. They speak the same length-prefixed companion protocol and report
// the same events and results, so the adapter's process plumbing, address
// handling and result decoding are exercised against a real loopback service.
const fixtureCompanion = `'use strict';
const net = require('node:net');
const MAX = 1024 * 1024;

function request(address, payload, options = {}) {
  const port = options.port || 9027;
  const body = Buffer.from(JSON.stringify({protocol: 1, ...payload}));
  if (body.length > MAX) return Promise.reject(Error('request too large'));
  return new Promise((resolve, reject) => {
    const socket = net.createConnection({host: address, port});
    let pending = Buffer.alloc(0), expected = null, settled = false;
    const finish = (error, value) => {
      if (settled) return;
      settled = true;
      socket.destroy();
      if (error) reject(error); else resolve(value);
    };
    socket.setTimeout(options.timeout || 5000, () => finish(Error('CivHomebrew companion timed out')));
    socket.once('connect', () => {
      const length = Buffer.alloc(4);
      length.writeUInt32BE(body.length);
      socket.write(Buffer.concat([length, body]));
    });
    socket.on('data', chunk => {
      pending = Buffer.concat([pending, chunk]);
      if (expected === null && pending.length >= 4) expected = pending.readUInt32BE(0);
      if (expected !== null && pending.length >= expected + 4) {
        let reply;
        try { reply = JSON.parse(pending.subarray(4, expected + 4).toString('utf8')); }
        catch { return finish(Error('Invalid CivHomebrew companion response')); }
        if (!reply || reply.ok !== true) return finish(Error(reply && reply.error || 'CivHomebrew companion rejected the request'));
        finish(null, reply);
      }
      if (pending.length > MAX + 4) finish(Error('CivHomebrew companion response is too large'));
    });
    socket.once('error', error => finish(Error('CivHomebrew connection failed: ' + (error.code || error.message))));
    socket.once('end', () => finish(Error('CivHomebrew companion closed before its response')));
  });
}

async function probe(address, options) {
  const hello = await request(address, {op: 'hello'}, options);
  if (hello.app !== 'CivHomebrew' || hello.protocol !== 1 || hello.core !== 1) {
    throw Error('Port 9027 did not identify a compatible CivHomebrew core');
  }
  const status = await request(address, {op: 'status'}, options);
  return status.state;
}

// The package helper probes the companion's package protocol instead of the
// core handshake, exactly like the pinned Moonlight script does.
async function probePackage(address, options) {
  const hello = await request(address, {op: 'hello'}, options);
  if (hello.app !== 'CivHomebrew' || hello.protocol !== 1 || hello.packageVersion !== 1) {
    throw Error('The service on port 9027 is not a compatible CivHomebrew companion');
  }
  const status = await request(address, {op: 'status'}, options);
  const list = await request(address, {op: 'list'}, options);
  if (!status.state || !Array.isArray(list.entries)) throw Error('CivHomebrew status is incomplete');
  return {state: status.state, entries: list.entries};
}

module.exports = {request, probe, probePackage};
`

const fixtureHomebrew = `'use strict';
const {request, probe} = require('./fixture-companion.cjs');

async function main(args, options = {}) {
  const emit = options.emit || (() => {});
  const [action, runtimeRoot, address] = args;
  if (action === 'probe') {
    const state = await probe(address, options);
    emit('probe-ready', {mode: state.mode, launcher: state.launcher, ftp: state.ftp,
      message: 'CivHomebrew ready'});
    return state;
  }
  if (action === 'shutdown') {
    const state = await probe(address, options);
    if (state.mode !== 'launcher' || state.busy) {
      return {blocked: true, message: 'Close the foreground app on your Switch and return to CivHomebrew, then press Stop again'};
    }
    await request(address, {op: 'shutdown'}, options);
    const closed = await request(address, {op: 'status'}, options);
    emit('stopped', {message: 'CivHomebrew closed'});
    return {stopped: closed.state.closed === true};
  }
  throw Error('the fixture does not implement ' + action + ' on ' + runtimeRoot);
}

module.exports = {main};
`

const fixturePackage = `'use strict';
const fs = require('node:fs/promises');
const path = require('node:path');
const {request, probePackage} = require('./fixture-companion.cjs');

async function main(args, options = {}) {
  const emit = options.emit || (() => {});
  const [action, runtimeRoot, address, file] = args;
  if (action !== 'install' || path.extname(file).toLowerCase() !== '.civapp') {
    throw Error('Choose a .civapp package');
  }
  const item = JSON.parse(await fs.readFile(file, 'utf8'));
  if (!item || item.format !== 'civswitch-app' || typeof item.id !== 'string' || typeof item.name !== 'string') {
    throw Error('The selected file is not a supported CivHomebrew app package');
  }
  const companion = await probePackage(address, options);
  const state = companion.state;
  if (state.busy || state.mode !== 'launcher' || state.closed) {
    throw Error('Return to the CivHomebrew launcher before installing a package');
  }
  emit('status', {mode: state.mode, message: 'Checking the selected CivHomebrew package'});
  const reply = await request(address, {op: 'install', package: item}, options);
  if (!reply.entry || reply.entry.id !== item.id) {
    throw Error('CivHomebrew did not confirm the package installation');
  }
  emit('installed', {id: item.id, message: item.name + ' installed in CivHomebrew'});
  return {id: item.id, name: item.name, entry: reply.entry};
}

module.exports = {main};
`

// companionServer is a loopback stand-in for the CivHomebrew core on port 9027.
type companionServer struct {
	listener net.Listener
	mu       sync.Mutex
	requests []map[string]any
	respond  func(request map[string]any) map[string]any
}

func newCompanion(t *testing.T, respond func(map[string]any) map[string]any) *companionServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &companionServer{listener: listener, respond: respond}
	t.Cleanup(func() { listener.Close() })
	go server.serve()
	return server
}

func (s *companionServer) port() int { return s.listener.Addr().(*net.TCPAddr).Port }

func (s *companionServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *companionServer) handle(conn net.Conn) {
	defer conn.Close()
	var pending []byte
	buffer := make([]byte, 4096)
	for {
		read, err := conn.Read(buffer)
		if err != nil {
			return
		}
		pending = append(pending, buffer[:read]...)
		if len(pending) < 4 || len(pending) < 4+int(binary.BigEndian.Uint32(pending)) {
			continue
		}
		length := int(binary.BigEndian.Uint32(pending))
		var request map[string]any
		if json.Unmarshal(pending[4:4+length], &request) != nil {
			return
		}
		s.mu.Lock()
		s.requests = append(s.requests, request)
		s.mu.Unlock()
		body, err := json.Marshal(s.respond(request))
		if err != nil {
			return
		}
		frame := make([]byte, 4+len(body))
		binary.BigEndian.PutUint32(frame, uint32(len(body)))
		copy(frame[4:], body)
		if _, err := conn.Write(frame); err != nil {
			return
		}
		return
	}
}

func (s *companionServer) operations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var operations []string
	for _, request := range s.requests {
		operation, _ := request["op"].(string)
		operations = append(operations, operation)
	}
	return operations
}

func helloReply() map[string]any {
	return map[string]any{"ok": true, "app": "CivHomebrew", "protocol": 1, "core": 1, "packageVersion": 1}
}

func helloResponder(state map[string]any) func(map[string]any) map[string]any {
	return func(request map[string]any) map[string]any {
		if request["op"] == "hello" {
			return helloReply()
		}
		return map[string]any{"ok": true, "state": state}
	}
}

type homebrewFixture struct {
	adapter   *Adapter
	toolRoot  string
	support   string
	companion *companionServer
}

func newHomebrewFixture(t *testing.T, respond func(map[string]any) map[string]any) *homebrewFixture {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("CivHomebrew needs Node")
	}
	root, support := t.TempDir(), t.TempDir()
	toolRoot := filepath.Join(root, "tools-version")
	stage := filepath.Join(toolRoot, "tools", homebrewComponent)
	files := map[string]string{
		"packaging/CivHomebrew-source.json":              `{"commit":"` + strings.Repeat("a", 40) + `"}`,
		"scripts/gui-civhomebrew.cjs":                    fixtureHomebrew,
		"scripts/gui-civswitch-package.cjs":              fixturePackage,
		"scripts/fixture-companion.cjs":                  fixtureCompanion,
		"integrations/civhomebrew-catalog/catalog.js":    "// fixture catalog\n",
		"integrations/civhomebrew-catalog/catalog-ui.js": "// fixture catalog ui\n",
	}
	extra := map[string]string{}
	for name, body := range files {
		target := filepath.Join(stage, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0o400); err != nil {
			t.Fatal(err)
		}
		extra[name] = fmt.Sprintf("%x", sha256Sum([]byte(body)))
	}
	component := manifest.Component{
		ID:           homebrewComponent,
		Path:         "packaging/CivHomebrew-source.json",
		SHA256:       extra["packaging/CivHomebrew-source.json"],
		Distribution: manifest.Distribution{Decision: "may-ship"},
		Source: &manifest.ToolSource{
			Directory: "integrations/civhomebrew-catalog",
			Format:    "selected-files",
			Files:     len(extra),
			Extra:     extra,
		},
	}
	companion := newCompanion(t, respond)
	adapter := New(Options{
		Manifest:      &manifest.Manifest{Components: []manifest.Component{component}},
		ToolRoot:      toolRoot,
		NodeBinary:    node,
		Support:       support,
		HomebrewPort:  companion.port(),
		AllowLoopback: true,
	})
	return &homebrewFixture{adapter: adapter, toolRoot: toolRoot, support: support, companion: companion}
}

func TestCivHomebrewProbeReadsTheCompanionStatus(t *testing.T) {
	fixture := newHomebrewFixture(t, helloResponder(map[string]any{
		"mode": "launcher", "launcher": true, "ftp": true, "busy": false, "closed": false}))
	state, err := fixture.adapter.CivHomebrewProbe(context.Background(), "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != "launcher" || !state.Launcher || !state.FTP || state.Busy || state.Closed {
		t.Fatalf("companion state: %+v", state)
	}
	if state.Message != "CivHomebrew ready" {
		t.Fatalf("probe message: %q", state.Message)
	}
	if operations := fixture.companion.operations(); strings.Join(operations, ",") != "hello,status" {
		t.Fatalf("companion operations: %v", operations)
	}
}

func TestCivHomebrewProbeRejectsAnUnrelatedService(t *testing.T) {
	fixture := newHomebrewFixture(t, func(map[string]any) map[string]any {
		return map[string]any{"ok": true, "app": "something else", "protocol": 1, "core": 1}
	})
	_, err := fixture.adapter.CivHomebrewProbe(context.Background(), "127.0.0.1")
	if err == nil || !strings.Contains(err.Error(), "compatible CivHomebrew core") {
		t.Fatalf("unrelated service: %v", err)
	}
}

func TestCivHomebrewShutdownStopsAndReportsBlockedApps(t *testing.T) {
	t.Run("launcher stops", func(t *testing.T) {
		// A real companion reports closed: true once it accepted the shutdown.
		closed := false
		fixture := newHomebrewFixture(t, func(request map[string]any) map[string]any {
			switch request["op"] {
			case "hello":
				return helloReply()
			case "shutdown":
				closed = true
				return map[string]any{"ok": true}
			}
			return map[string]any{"ok": true, "state": map[string]any{
				"mode": "launcher", "launcher": true, "closed": closed}}
		})
		stop, err := fixture.adapter.CivHomebrewShutdown(context.Background(), "127.0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		if !stop.Stopped || stop.Blocked {
			t.Fatalf("shutdown result: %+v", stop)
		}
		if operations := strings.Join(fixture.companion.operations(), ","); operations != "hello,status,shutdown,status" {
			t.Fatalf("companion operations: %s", operations)
		}
	})
	t.Run("foreground app blocks", func(t *testing.T) {
		fixture := newHomebrewFixture(t, helloResponder(map[string]any{"mode": "app", "closed": false}))
		stop, err := fixture.adapter.CivHomebrewShutdown(context.Background(), "127.0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		if stop.Stopped || !stop.Blocked || !strings.Contains(stop.Message, "Close the foreground app") {
			t.Fatalf("blocked shutdown: %+v", stop)
		}
		if operations := strings.Join(fixture.companion.operations(), ","); operations != "hello,status" {
			t.Fatalf("a blocked shutdown sent: %s", operations)
		}
	})
}

func TestCivHomebrewInstallPackageSendsAChosenCivApp(t *testing.T) {
	fixture := newHomebrewFixture(t, func(request map[string]any) map[string]any {
		switch request["op"] {
		case "hello":
			return helloReply()
		case "status":
			return map[string]any{"ok": true, "state": map[string]any{"mode": "launcher", "launcher": true}}
		case "list":
			return map[string]any{"ok": true, "entries": []any{}}
		}
		item, _ := request["package"].(map[string]any)
		return map[string]any{"ok": true, "entry": map[string]any{"id": item["id"], "name": item["name"]}}
	})
	file := filepath.Join(t.TempDir(), "moonlight.civapp")
	packageJSON := `{"format":"civswitch-app","version":1,"id":"moonlight","name":"Moonlight","scripts":[{"name":"app.js","source":"log(1)"}]}`
	if err := os.WriteFile(file, []byte(packageJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	var events []CivHomebrewEvent
	app, err := fixture.adapter.CivHomebrewInstallPackage(context.Background(), "127.0.0.1", file,
		func(event CivHomebrewEvent) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if app.ID != "moonlight" || app.Name != "Moonlight" || app.File != file || app.Pinned {
		t.Fatalf("installed package: %+v", app)
	}
	var kinds []string
	for _, event := range events {
		kinds = append(kinds, event.Type)
	}
	if strings.Join(kinds, ",") != "status,installed" {
		t.Fatalf("package events: %v", kinds)
	}
}

func TestCivHomebrewInstallPackageValidatesTheChosenFile(t *testing.T) {
	fixture := newHomebrewFixture(t, helloResponder(map[string]any{"mode": "launcher"}))
	for name, file := range map[string]string{
		"not a package": "notes.txt",
		"relative":      "relative.civapp",
		"empty":         "empty.civapp",
		"missing":       filepath.Join(t.TempDir(), "missing.civapp"),
		"a directory":   t.TempDir(),
	} {
		t.Run(name, func(t *testing.T) {
			if strings.HasSuffix(file, ".txt") || !filepath.IsAbs(file) {
				if _, err := fixture.adapter.CivHomebrewInstallPackage(context.Background(), "127.0.0.1", file, nil); err == nil {
					t.Fatal("accepted a file that is not a .civapp package")
				}
				return
			}
			if _, err := fixture.adapter.CivHomebrewInstallPackage(context.Background(), "127.0.0.1", file, nil); err == nil {
				t.Fatal("accepted an unusable package")
			}
		})
	}
	if operations := len(fixture.companion.operations()); operations != 0 {
		t.Fatalf("an unusable package reached the console: %d requests", operations)
	}
}

func TestCivHomebrewPinnedPackageNeedsAVerifiedPluginStage(t *testing.T) {
	fixture := newHomebrewFixture(t, helloResponder(map[string]any{"mode": "launcher"}))
	for _, id := range homebrewPinnedApps {
		_, err := fixture.adapter.CivHomebrewInstallPackage(context.Background(), "127.0.0.1", id, nil)
		if err == nil || !strings.Contains(err.Error(), "not installed") {
			t.Fatalf("%s without its plugin: %v", id, err)
		}
	}
	if operations := len(fixture.companion.operations()); operations != 0 {
		t.Fatalf("an uninstalled package reached the console: %d requests", operations)
	}
}

func TestCivHomebrewRequiresPrivateLANAddressesAndAStagedPlugin(t *testing.T) {
	fixture := newHomebrewFixture(t, helloResponder(map[string]any{"mode": "launcher"}))
	loopback := fixture.adapter
	loopback.options.AllowLoopback = false
	if _, err := loopback.CivHomebrewProbe(context.Background(), "127.0.0.1"); err == nil {
		t.Fatal("accepted a loopback console address")
	}
	if _, err := fixture.adapter.CivHomebrewProbe(context.Background(), "192.0.2.2"); err == nil {
		t.Fatal("accepted a console address outside the LAN")
	}
	if !privateIPv4("192.168.1.20", false) || privateIPv4("8.8.8.8", false) {
		t.Fatal("the LAN address check does not cover the private ranges")
	}
	if operations := len(fixture.companion.operations()); operations != 0 {
		t.Fatalf("an unusable address reached the companion: %d requests", operations)
	}
	unstaged := New(Options{Manifest: fixture.adapter.options.Manifest, NodeBinary: fixture.adapter.options.NodeBinary,
		Support: fixture.support, HomebrewPort: fixture.companion.port(), AllowLoopback: true})
	if _, err := unstaged.CivHomebrewProbe(context.Background(), "127.0.0.1"); err == nil {
		t.Fatal("probed without the CivHomebrew plugin staged")
	}
	tampered := filepath.Join(fixture.toolRoot, "tools", homebrewComponent, "scripts/gui-civhomebrew.cjs")
	if err := os.Chmod(tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tampered, []byte("// changed\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.adapter.CivHomebrewProbe(context.Background(), "127.0.0.1"); err == nil {
		t.Fatal("ran a changed plugin script")
	}
}

func TestCivHomebrewLaunchNeedsTheStagedRuntime(t *testing.T) {
	fixture := newHomebrewFixture(t, helloResponder(map[string]any{"mode": "launcher"}))
	err := fixture.adapter.CivHomebrewLaunch(context.Background(), "127.0.0.1", nil)
	if err == nil || !strings.Contains(err.Error(), "Python runtime") {
		t.Fatalf("launch without a runtime: %v", err)
	}
	_, err = fixture.adapter.CivHomebrewInstallTrigger(context.Background(), "127.0.0.1", true, nil)
	if err == nil || !strings.Contains(err.Error(), "Python runtime") {
		t.Fatalf("trigger install without a runtime: %v", err)
	}
	if _, err := fixture.adapter.CivHomebrewInstallTrigger(context.Background(), "127.0.0.1", false, nil); err == nil {
		t.Fatal("installed the trigger without confirming the save replacement")
	}
	var scriptErr error
	_, scriptErr = fixture.adapter.CivHomebrewInstallPackage(context.Background(), "127.0.0.1",
		filepath.Join(t.TempDir(), "x.civapp"), nil)
	if scriptErr == nil {
		t.Fatal("accepted a missing package file")
	}
	var pathErr *os.PathError
	if !errors.As(scriptErr, &pathErr) && !strings.Contains(scriptErr.Error(), "package") {
		t.Fatalf("missing package: %v", scriptErr)
	}
}

// A staged runtime resolves the interpreter, but the source tree still has to be
// the verified upstream one, so nothing runs from an unverified copy.
func TestCivHomebrewLaunchNeedsTheVerifiedSourceTree(t *testing.T) {
	fixture := newHomebrewFixture(t, helloResponder(map[string]any{"mode": "launcher"}))
	runtimeRoot := filepath.Join(t.TempDir(), "homebrew-runtime")
	python := filepath.Join(runtimeRoot, "homebrew", "python", "bin", "python3")
	if err := os.MkdirAll(filepath.Dir(python), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(python, []byte("#!/bin/sh\nexit 0\n"), 0o500); err != nil {
		t.Fatal(err)
	}
	fixture.adapter.options.HomebrewRuntimeRoot = runtimeRoot
	if got := fixture.adapter.python(); got != python {
		t.Fatalf("interpreter: %q", got)
	}
	err := fixture.adapter.CivHomebrewLaunch(context.Background(), "127.0.0.1", nil)
	if err == nil || !strings.Contains(err.Error(), "verified CivHomebrew source") {
		t.Fatalf("launch without the verified source: %v", err)
	}
}

func TestHomebrewSourceCacheIsRewrittenFromTheVerifiedTree(t *testing.T) {
	upstream, support := t.TempDir(), t.TempDir()
	files := map[string]string{
		"packaging/CivHomebrew-source.json": "{}",
		"tools/payload_menu.py":             "print('menu')\n",
		"app/boot.js":                       "// boot\n",
	}
	for name, body := range files {
		target := filepath.Join(upstream, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0o400); err != nil {
			t.Fatal(err)
		}
	}
	adapter := New(Options{Support: support})
	cache := filepath.Join(support, "civswitch", "civhomebrew", "source", strings.Repeat("b", 40))
	if err := adapter.syncHomebrewCache(upstream, cache); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		file := filepath.Join(cache, filepath.FromSlash(name))
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != body {
			t.Fatalf("cached %s: %q", name, data)
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("cached %s is %v", name, info.Mode())
		}
	}
	// A stale cache is replaced rather than merged.
	stale := filepath.Join(cache, "stale.py")
	if err := os.WriteFile(stale, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := adapter.syncHomebrewCache(upstream, cache); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale cache entry survived: %v", err)
	}
	// A special file in the source tree is refused instead of copied.
	if err := os.Symlink(filepath.Join(upstream, "app/boot.js"), filepath.Join(upstream, "app/link.js")); err != nil {
		t.Fatal(err)
	}
	if err := adapter.syncHomebrewCache(upstream, filepath.Join(support, "civswitch", "civhomebrew", "source", "other")); err == nil {
		t.Fatal("copied a special source entry")
	}
}
