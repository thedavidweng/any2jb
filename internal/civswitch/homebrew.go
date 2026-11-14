package civswitch

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/thedavidweng/any2jb/internal/plugins"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

const (
	homebrewComponent = "civhomebrew"
	homebrewPrefix    = "MAC2JB_CIVHOMEBREW "
	homebrewEventLine = 4 << 20
	maxCivAppBytes    = 32 << 20
	homebrewStopGrace = 5 * time.Second
)

// The CivHomebrew payload types are defined by the session, which owns the
// adapter contract that the console round trips through.
type (
	CivHomebrewState = session.CivHomebrewState
	CivHomebrewEvent = session.CivHomebrewEvent
	CivHomebrewStop  = session.CivHomebrewStop
	CivHomebrewApp   = session.CivHomebrewApp
)

var _ session.CivSwitchApps = (*Adapter)(nil)

// homebrewPinnedApps are the catalog plugins Any2JB resolves and verifies itself
// before sending one .civapp package.
var homebrewPinnedApps = []string{"moonlight-civswitch", "sw2nes", "jellyfin-civswitch"}

// homebrewBootstrap adapts the pinned plugin scripts to Any2JB: one event
// prefix, the verified source cache instead of a second download, and the
// resolved interpreter for the plugin's own action scripts. The pinned scripts
// report through emit(type, fields), so the emit option keeps that signature.
const homebrewBootstrap = `'use strict';
const {spawn} = require('node:child_process');
const config = JSON.parse(process.env.ANY2JB_CIVHOMEBREW);
const emit = (type, fields = {}) => process.stdout.write('MAC2JB_CIVHOMEBREW ' + JSON.stringify({type, ...fields}) + '\n');
const options = {emit, cacheRoot: config.cacheRoot, allowLoopback: config.allowLoopback === true};
if (config.metadata) options.metadata = config.metadata;
if (config.port) options.port = config.port;
if (config.python) options.spawn = (command, args, spawnOptions) => spawn(config.python, args, spawnOptions);
Promise.resolve(require(config.module).main(config.args, options)).then(
  result => emit('result', {result: result === undefined ? null : result}),
  error => {
    emit('failed', {message: String((error && error.message) || error)});
    process.exitCode = 1;
  });
`

// homebrewConfig is the single config value the bootstrap reads.
type homebrewConfig struct {
	Module        string          `json:"module"`
	Args          []string        `json:"args"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	CacheRoot     string          `json:"cacheRoot"`
	Port          int             `json:"port,omitempty"`
	AllowLoopback bool            `json:"allowLoopback"`
	Python        string          `json:"python,omitempty"`
}

// CivHomebrewProbe verifies the companion on port 9027 and, when the console is
// idle in its launcher, refreshes the staged catalog overlay there.
func (a *Adapter) CivHomebrewProbe(ctx context.Context, address string) (CivHomebrewState, error) {
	if err := a.homebrewAddress(address); err != nil {
		return CivHomebrewState{}, err
	}
	root, err := a.homebrewRoot()
	if err != nil {
		return CivHomebrewState{}, err
	}
	message := ""
	result, err := a.runHomebrew(ctx, homebrewRequest{
		module: "gui-civhomebrew.cjs",
		args:   []string{"probe", a.homebrewRuntime(root), address},
		event: func(event CivHomebrewEvent) {
			if event.Message != "" {
				message = event.Message
			}
		},
	})
	if err != nil {
		return CivHomebrewState{}, err
	}
	var state struct {
		Mode     string `json:"mode"`
		Launcher bool   `json:"launcher"`
		FTP      bool   `json:"ftp"`
		Busy     bool   `json:"busy"`
		Closed   bool   `json:"closed"`
	}
	if err := json.Unmarshal(result, &state); err != nil {
		return CivHomebrewState{}, fmt.Errorf("the CivHomebrew companion status is invalid: %w", err)
	}
	return CivHomebrewState{Mode: state.Mode, Launcher: state.Launcher, FTP: state.FTP,
		Busy: state.Busy, Closed: state.Closed, Message: message}, nil
}

// CivHomebrewLaunch sends the temporary launcher to the open loader and blocks
// until the console closes it or ctx is cancelled, in which case it shuts the
// companion down and reaps the sender.
func (a *Adapter) CivHomebrewLaunch(ctx context.Context, address string, onEvent func(CivHomebrewEvent)) error {
	if err := a.homebrewAddress(address); err != nil {
		return err
	}
	root, err := a.homebrewRoot()
	if err != nil {
		return err
	}
	runtime, err := a.homebrewRuntimeRoot()
	if err != nil {
		return err
	}
	metadata, err := a.homebrewSource(root)
	if err != nil {
		return err
	}
	_, err = a.runHomebrew(ctx, homebrewRequest{
		module:   "gui-civhomebrew.cjs",
		args:     []string{"launch", runtime, address},
		metadata: metadata,
		event:    onEvent,
	})
	return err
}

// CivHomebrewInstallTrigger installs the persistent CivHomebrew boot trigger.
// The console backs the single-player saves up before it writes anything.
func (a *Adapter) CivHomebrewInstallTrigger(ctx context.Context, address string, confirmedSaveReplacement bool, onEvent func(CivHomebrewEvent)) (string, error) {
	if !confirmedSaveReplacement {
		return "", errors.New("installing the CivHomebrew boot trigger replaces the Civilization VII single-player save files and needs explicit confirmation")
	}
	if err := a.homebrewAddress(address); err != nil {
		return "", err
	}
	root, err := a.homebrewRoot()
	if err != nil {
		return "", err
	}
	runtime, err := a.homebrewRuntimeRoot()
	if err != nil {
		return "", err
	}
	metadata, err := a.homebrewSource(root)
	if err != nil {
		return "", err
	}
	result, err := a.runHomebrew(ctx, homebrewRequest{
		module:   "gui-civhomebrew.cjs",
		args:     []string{"install", runtime, address},
		metadata: metadata,
		event:    onEvent,
	})
	if err != nil {
		return "", err
	}
	var installed struct {
		BackupPath string `json:"backupPath"`
	}
	if err := json.Unmarshal(result, &installed); err != nil || installed.BackupPath == "" {
		return "", errors.New("the console did not report where it kept the save backup")
	}
	return installed.BackupPath, nil
}

// CivHomebrewRestoreTrigger puts the original CivSwitch trigger back.
func (a *Adapter) CivHomebrewRestoreTrigger(ctx context.Context, address string, onEvent func(CivHomebrewEvent)) error {
	if err := a.homebrewAddress(address); err != nil {
		return err
	}
	root, err := a.homebrewRoot()
	if err != nil {
		return err
	}
	runtime, err := a.homebrewRuntimeRoot()
	if err != nil {
		return err
	}
	metadata, err := a.homebrewSource(root)
	if err != nil {
		return err
	}
	_, err = a.runHomebrew(ctx, homebrewRequest{
		module:   "gui-civhomebrew.cjs",
		args:     []string{"restore", runtime, address},
		metadata: metadata,
		event:    onEvent,
	})
	return err
}

// CivHomebrewShutdown stops the companion, or reports why it cannot stop while
// an app is in the foreground.
func (a *Adapter) CivHomebrewShutdown(ctx context.Context, address string) (CivHomebrewStop, error) {
	if err := a.homebrewAddress(address); err != nil {
		return CivHomebrewStop{}, err
	}
	root, err := a.homebrewRoot()
	if err != nil {
		return CivHomebrewStop{}, err
	}
	result, err := a.runHomebrew(ctx, homebrewRequest{
		module: "gui-civhomebrew.cjs",
		args:   []string{"shutdown", a.homebrewRuntime(root), address},
	})
	if err != nil {
		return CivHomebrewStop{}, err
	}
	var stop struct {
		Stopped bool   `json:"stopped"`
		Blocked bool   `json:"blocked"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(result, &stop); err != nil {
		return CivHomebrewStop{}, fmt.Errorf("the CivHomebrew shutdown report is invalid: %w", err)
	}
	return CivHomebrewStop{Stopped: stop.Stopped, Blocked: stop.Blocked, Message: stop.Message}, nil
}

// CivHomebrewInstallPackage installs one verified catalog app or a .civapp file
// the user chose. Pinned plugin packages are resolved and their SHA-256 is
// checked before anything is sent to the console.
func (a *Adapter) CivHomebrewInstallPackage(ctx context.Context, address, packageSpec string, onEvent func(CivHomebrewEvent)) (CivHomebrewApp, error) {
	if err := a.homebrewAddress(address); err != nil {
		return CivHomebrewApp{}, err
	}
	root, err := a.homebrewRoot()
	if err != nil {
		return CivHomebrewApp{}, err
	}
	file, pinned, err := a.civAppPackage(packageSpec)
	if err != nil {
		return CivHomebrewApp{}, err
	}
	result, err := a.runHomebrew(ctx, homebrewRequest{
		module: "gui-civswitch-package.cjs",
		args:   []string{"install", a.homebrewRuntime(root), address, file},
		event:  onEvent,
	})
	if err != nil {
		return CivHomebrewApp{}, err
	}
	var installed struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(result, &installed); err != nil || installed.ID == "" {
		return CivHomebrewApp{}, errors.New("the console did not confirm the package installation")
	}
	return CivHomebrewApp{ID: installed.ID, Name: installed.Name, File: file, Pinned: pinned}, nil
}

// civAppPackage resolves either a pinned catalog plugin package or an absolute
// .civapp path the user chose.
func (a *Adapter) civAppPackage(spec string) (string, bool, error) {
	for _, id := range homebrewPinnedApps {
		if spec == id {
			if a.options.Manifest == nil || a.options.ToolRoot == "" {
				return "", false, fmt.Errorf("the %s package is not installed", id)
			}
			pkg, err := plugins.CivAppPackagePath(a.options.ToolRoot, a.options.Manifest, id)
			if err != nil {
				return "", false, fmt.Errorf("the %s package is not installed or failed verification: %w", id, err)
			}
			return pkg, true, nil
		}
	}
	if !filepath.IsAbs(spec) || strings.ToLower(filepath.Ext(spec)) != ".civapp" {
		return "", false, errors.New("choose an absolute path to a .civapp package, or a catalog app")
	}
	info, err := os.Lstat(spec)
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxCivAppBytes {
		return "", false, errors.New("choose a regular .civapp package smaller than 32 MiB")
	}
	return spec, false, nil
}

// homebrewAddress accepts the private LAN addresses the pinned scripts accept;
// a loopback fixture has to ask for 127.0.0.1 explicitly.
func (a *Adapter) homebrewAddress(address string) error {
	if !privateIPv4(address, a.options.AllowLoopback) {
		return errors.New("enter the Switch IPv4 address on the same reachable LAN")
	}
	return nil
}

// homebrewRoot verifies the staged civhomebrew plugin tree and returns it.
func (a *Adapter) homebrewRoot() (string, error) {
	if a.options.Manifest == nil {
		return "", locales.Key("dynCivManifestUnavailable")
	}
	if a.options.ToolRoot == "" {
		return "", locales.Key("dynCivNotStaged")
	}
	root := filepath.Join(a.options.ToolRoot, "tools", homebrewComponent)
	if err := a.options.Manifest.VerifyToolSource(root, homebrewComponent); err != nil {
		return "", fmt.Errorf("the CivHomebrew plugin is not installed or failed verification: %w", err)
	}
	return root, nil
}

// homebrewRuntime is the runtime root the plugin scripts resolve. Only the
// actions that spawn Python get past the companion handshake.
func (a *Adapter) homebrewRuntime(pluginRoot string) string {
	if a.options.HomebrewRuntimeRoot != "" {
		return a.options.HomebrewRuntimeRoot
	}
	return pluginRoot
}

// homebrewRuntimeRoot is required by the actions that run the console scripts.
func (a *Adapter) homebrewRuntimeRoot() (string, error) {
	if a.options.HomebrewRuntimeRoot == "" {
		return "", errors.New("the portable Python runtime is not staged; install the homebrew runtime first")
	}
	return a.options.HomebrewRuntimeRoot, nil
}

// homebrewSource places the already verified upstream CivHomebrew tree in the
// plugin's own source cache, so no second download or extraction is needed.
func (a *Adapter) homebrewSource(pluginRoot string) (json.RawMessage, error) {
	metadata, err := a.options.Manifest.Read(pluginRoot, homebrewComponent)
	if err != nil {
		return nil, err
	}
	var pin struct {
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal(metadata, &pin); err != nil || !commitID(pin.Commit) {
		return nil, errors.New("the pinned CivHomebrew source metadata is invalid")
	}
	upstream, _, err := plugins.CivHomebrewSourceRoot(a.options.ToolRoot, a.options.Manifest)
	if err != nil {
		return nil, fmt.Errorf("the verified CivHomebrew source is not installed: %w", err)
	}
	cache := filepath.Join(a.options.Support, "civswitch", "civhomebrew", "source", pin.Commit)
	if err := a.syncHomebrewCache(upstream, cache); err != nil {
		return nil, err
	}
	return json.RawMessage(metadata), nil
}

// syncHomebrewCache rewrites the source cache from the verified upstream tree.
// The plugin verifies the cache again before it runs anything from it.
func (a *Adapter) syncHomebrewCache(upstream, cache string) error {
	if err := os.MkdirAll(filepath.Dir(cache), 0o700); err != nil {
		return err
	}
	if err := os.RemoveAll(cache); err != nil {
		return err
	}
	return copyTree(upstream, cache)
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected CivHomebrew source entry: %s", relative)
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o600)
	})
}

func commitID(value string) bool {
	if len(value) != 40 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// homebrewRequest is one plugin script invocation.
type homebrewRequest struct {
	module   string
	args     []string
	metadata json.RawMessage
	event    func(CivHomebrewEvent)
}

// runHomebrew runs one pinned plugin script and returns its reported result.
func (a *Adapter) runHomebrew(ctx context.Context, request homebrewRequest) (json.RawMessage, error) {
	node, err := executable(a.options.NodeBinary, "CivHomebrew needs a staged Node runtime")
	if err != nil {
		return nil, err
	}
	if a.options.Support == "" || !filepath.IsAbs(a.options.Support) {
		return nil, errors.New("CivHomebrew needs an absolute support root")
	}
	root, err := a.homebrewRoot()
	if err != nil {
		return nil, err
	}
	config := homebrewConfig{
		Module:        filepath.Join(root, "scripts", request.module),
		Args:          request.args,
		Metadata:      request.metadata,
		CacheRoot:     filepath.Join(a.options.Support, "civswitch", "civhomebrew"),
		Port:          a.options.HomebrewPort,
		AllowLoopback: a.options.AllowLoopback,
		Python:        a.python(),
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(node, "-e", homebrewBootstrap)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "ANY2JB_CIVHOMEBREW="+string(payload))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	exited := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(homebrewStopGrace):
			_ = cmd.Process.Kill()
		}
	})
	defer stop()
	var diagnostics limitedBuffer
	var readErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, readErr = io.Copy(&diagnostics, stderr)
	}()
	result, actionErr := decodeHomebrewEvents(stdout, request.event)
	<-done
	waitErr := cmd.Wait()
	close(exited)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if waitErr != nil || actionErr != nil {
		return nil, homebrewFailure(waitErr, actionErr, diagnostics.String())
	}
	if readErr != nil {
		return nil, readErr
	}
	if len(result) == 0 {
		return nil, errors.New("CivHomebrew did not report a result")
	}
	return result, nil
}

// decodeHomebrewEvents reads the prefixed event stream and keeps the last
// reported result and failure message. Only fixed fields cross this boundary.
func decodeHomebrewEvents(stdout io.Reader, onEvent func(CivHomebrewEvent)) (json.RawMessage, error) {
	var result json.RawMessage
	var failure error
	reader := bufio.NewReaderSize(stdout, 64<<10)
	for {
		line, err := readLine(reader, homebrewEventLine)
		if line != "" && strings.HasPrefix(line, homebrewPrefix) {
			var event struct {
				Type    string          `json:"type"`
				Message string          `json:"message"`
				Result  json.RawMessage `json:"result"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, homebrewPrefix)), &event) == nil && event.Type != "" {
				switch event.Type {
				case "result":
					result = event.Result
				case "failed":
					failure = errors.New(event.Message)
				}
				if onEvent != nil && event.Type != "result" {
					onEvent(CivHomebrewEvent{Type: event.Type, Message: event.Message})
				}
			}
		}
		if err != nil {
			if failure == nil && !errors.Is(err, io.EOF) {
				failure = err
			}
			return result, failure
		}
	}
}

// readLine reads one newline-terminated line, keeping at most limit bytes and
// discarding the rest, so one oversized event cannot exhaust memory.
func readLine(reader *bufio.Reader, limit int) (string, error) {
	var line []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(line) < limit {
			room := limit - len(line)
			if len(chunk) > room {
				chunk = chunk[:room]
			}
			line = append(line, chunk...)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return strings.TrimRight(string(line), "\r\n"), err
	}
}

func homebrewFailure(waitErr, actionErr error, diagnostics string) error {
	if actionErr != nil {
		return actionErr
	}
	if message := firstLine(diagnostics); message != "" {
		return errors.New(message)
	}
	return fmt.Errorf("CivHomebrew action failed: %w", waitErr)
}

func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return truncated(trimmed, 400)
		}
	}
	return ""
}

func truncated(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}

// limitedBuffer keeps the head of a diagnostic stream; a failing plugin cannot
// grow it without bound.
type limitedBuffer struct {
	mu     sync.Mutex
	buffer strings.Builder
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := 8192 - b.buffer.Len(); room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		b.buffer.Write(p)
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

// python resolves the interpreter the plugin scripts run the console tools with.
func (a *Adapter) python() string {
	var candidates []string
	if a.options.HomebrewPython != "" {
		candidates = append(candidates, a.options.HomebrewPython)
	}
	if a.options.HomebrewRuntimeRoot != "" {
		candidates = append(candidates,
			filepath.Join(a.options.HomebrewRuntimeRoot, "python", "bin", "python3"),
			filepath.Join(a.options.HomebrewRuntimeRoot, "homebrew", "python", "bin", "python3"),
		)
	}
	if a.options.ToolRoot != "" {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(a.options.ToolRoot), "homebrew-runtime", "homebrew", "python", "bin", "python3"),
		)
	}
	for _, path := range candidates {
		if _, err := executable(path, ""); err == nil {
			return path
		}
	}
	if path, err := exec.LookPath("python3"); err == nil {
		return path
	}
	return ""
}
