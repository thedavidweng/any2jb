package artemis

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/procgroup"
)

// The pinned host emits one JSON frame per line and prefixes its own log output.
const (
	framePrefix = "MAC2JB_ARTEMIS_FS "
	maxLogLine  = 512
	// hostGrace bounds a graceful stop: the host releases the held RPC and
	// verifies its cleanup before it exits.
	hostGrace = 60 * time.Second
	// fsRuntime is the pinned host module inside the artemis-fs plugin.
	fsRuntime = "integrations/artemis-fs/runtime-host.cjs"
)

// ErrHostFailed reports a Seedsow filesystem host that stopped without the
// verified held session, or was stopped before it produced one.
var ErrHostFailed = errors.New("the Artemis Seedsow filesystem host failed")

//go:embed cp-preload.cjs
var cpPreload []byte

// fsDriver starts the pinned host with the paths this package validated.
// runtime-host.cjs only exports runFsHost; its own CLI is fs-host.cjs, which
// takes SWITCH_IPV4 MAC_IPV4 RUNTIME_ROOT and pins the launcher port to 8080. The
// driver passes exactly those three values plus the port a test or a future
// launcher may select.
const fsDriver = `const [consoleIp, hostIp, runtimeRoot, port] = process.argv.slice(1);` +
	`require(process.env.ANY2JB_ARTEMIS_FS_RUNTIME).runFsHost({consoleIp, macIp: hostIp, runtimeRoot, port: Number(port)})` +
	`.catch(error => {console.error(error && error.message || error); process.exitCode = 1;});`

// SeedsowFSRequest launches the experimental Artemis Seedsow filesystem host from
// the verified artemis-fs plugin.
type SeedsowFSRequest struct {
	Node     NodeBinary
	ToolRoot string
	Manifest *manifest.Manifest
	// Support is the private app support root. The host stages into
	// <Support>/runs and the holder writes its preload beneath <Support>/artemis.
	Support string
	// ConsoleIP is the Switch 2 on the local network; HostIP is this Mac's
	// address the console dials back.
	ConsoleIP, HostIP string
	// Port is the launcher HTTP port; zero selects 8080.
	Port int
	// AllowLoopback permits 127.0.0.1 console and computer fixtures.
	AllowLoopback bool
}

// SeedsowFSEvent is one message from a Seedsow host run. Type is "preparing",
// "progress", "held-ready", "stopped" or "failed" for the pinned host's own
// frames, and "host-starting" or "bridge-log" for what this package adds: the
// start of the child and any unprefixed output it wrote. Message is bounded and
// safe to show; RunPath, ResultPath and LogPath appear only on the frames that
// report them.
type SeedsowFSEvent struct {
	Type       string
	Message    string
	RunPath    string
	ResultPath string
	LogPath    string
}

// hostFrame is the pinned host's wire form.
type hostFrame struct {
	Type       string `json:"type"`
	Message    string `json:"message"`
	RunPath    string `json:"runPath"`
	ResultPath string `json:"resultPath"`
	LogPath    string `json:"logPath"`
}

// SeedsowFSSession owns one running Artemis Seedsow host. The console keeps the
// launcher open until the host stops.
type SeedsowFSSession struct {
	cmd    *exec.Cmd
	events func(SeedsowFSEvent)
	read   sync.WaitGroup
	done   chan struct{}

	mu       sync.Mutex
	runPath  string
	held     bool
	stopped  bool
	failure  string
	frameErr error
	exitErr  error
}

// StartSeedsowFS verifies the plugin, spawns the pinned host and returns before
// the console answers. Events arrive on their own goroutine, so onEvent must not
// block. The session lives until the host exits, Stop is called, or ctx is
// cancelled.
func StartSeedsowFS(ctx context.Context, request SeedsowFSRequest, onEvent func(SeedsowFSEvent)) (*SeedsowFSSession, error) {
	if request.Node == "" {
		return nil, errors.New("no verified Node binary for the Artemis Seedsow host")
	}
	if err := ConsoleAddress(request.ConsoleIP, request.AllowLoopback); err != nil {
		return nil, fmt.Errorf("console address: %w", err)
	}
	if err := ConsoleAddress(request.HostIP, request.AllowLoopback); err != nil {
		return nil, fmt.Errorf("computer address: %w", err)
	}
	support, err := canonicalDir(request.Support)
	if err != nil {
		return nil, err
	}
	port := request.Port
	if port == 0 {
		port = DefaultPort
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("the Artemis HTTP port must be 1..65535")
	}
	root, err := PluginRoot(request.ToolRoot, request.Manifest, ArtemisFSPlugin)
	if err != nil {
		return nil, err
	}
	if _, err := readRecorded(root, request.Manifest, ArtemisFSPlugin, fsRuntime); err != nil {
		return nil, err
	}
	runtime := filepath.Join(root, filepath.FromSlash(fsRuntime))
	preload, err := writePreload(support)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, string(request.Node), "--require", preload, "-e", fsDriver, "--",
		request.ConsoleIP, request.HostIP, support, strconv.Itoa(port))
	// The preload only widens copies inside the run tree the host stages into.
	cmd.Env = childEnv(
		"ANY2JB_RUNS_ROOT="+filepath.Join(support, "runs"),
		"ANY2JB_ARTEMIS_FS_RUNTIME="+runtime,
	)
	if err := procgroup.Configure(cmd); err != nil {
		return nil, err
	}
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
	session := &SeedsowFSSession{cmd: cmd, events: onEvent, done: make(chan struct{})}
	session.read.Add(2)
	go session.consume(stdout)
	go session.logs(stderr)
	go session.wait()
	session.emit(SeedsowFSEvent{Type: "host-starting", Message: "Starting the pinned Seedsow filesystem host"})
	return session, nil
}

// Wait blocks until the host exits and reports the outcome: nil only for a
// verified stop, otherwise the frame or exit failure that ended the session.
func (s *SeedsowFSSession) Wait() error {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.frameErr != nil:
		return s.frameErr
	case s.failure != "":
		return fmt.Errorf("%w: %s", ErrHostFailed, s.failure)
	case s.exitErr != nil:
		return fmt.Errorf("%w: %v", ErrHostFailed, s.exitErr)
	case !s.stopped:
		return fmt.Errorf("%w: the host exited without reporting a stop", ErrHostFailed)
	}
	return nil
}

// Stop ends the session the way Ctrl-C does: SIGINT to the host, a bounded wait
// for it to release the held RPC and verify cleanup, then SIGKILL of the whole
// process group. It reports only a failure to stop; the host's own outcome stays
// with Wait.
func (s *SeedsowFSSession) Stop(ctx context.Context) error {
	select {
	case <-s.done:
		return nil
	default:
	}
	if err := s.cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	timer := time.NewTimer(hostGrace)
	defer timer.Stop()
	select {
	case <-s.done:
	case <-ctx.Done():
		if err := procgroup.Signal(s.cmd, true); err != nil {
			return err
		}
		<-s.done
	case <-timer.C:
		if err := procgroup.Signal(s.cmd, true); err != nil {
			return err
		}
		<-s.done
	}
	return nil
}

// RunPath is the staged run folder this host owns, empty until held-ready. It is
// the run the retained manager discovers and attaches to, and the pinned target
// configuration keeps the Seedsow demo and full-game program IDs off the host's
// own save writes.
func (s *SeedsowFSSession) RunPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runPath
}

// Held reports whether the host proved verified filesystem access and a held RPC
// endpoint.
func (s *SeedsowFSSession) Held() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.held
}

func (s *SeedsowFSSession) consume(stdout io.Reader) {
	defer s.read.Done()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		frame, ok, err := parseFrame(line)
		if err != nil {
			s.fail(err)
			break
		}
		if !ok {
			if message := strings.TrimSpace(line); message != "" {
				s.emit(SeedsowFSEvent{Type: "bridge-log", Message: truncate(message, maxLogLine)})
			}
			continue
		}
		s.mu.Lock()
		switch frame.Type {
		case "held-ready":
			s.runPath, s.held = frame.RunPath, true
		case "failed":
			s.failure = frame.Message
		case "stopped":
			s.stopped = true
		}
		s.mu.Unlock()
		s.emit(SeedsowFSEvent{Type: frame.Type, Message: truncate(frame.Message, maxLogLine),
			RunPath: frame.RunPath, ResultPath: frame.ResultPath, LogPath: frame.LogPath})
	}
	if err := scanner.Err(); err != nil {
		s.fail(err)
	}
	// The host is not read past its frame limit, but its pipe must keep draining
	// so it can never block on a write this package refuses.
	_, _ = io.Copy(io.Discard, stdout)
}

func (s *SeedsowFSSession) logs(stderr io.Reader) {
	defer s.read.Done()
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 4<<10), 64<<10)
	for scanner.Scan() {
		if message := strings.TrimSpace(scanner.Text()); message != "" {
			s.emit(SeedsowFSEvent{Type: "bridge-log", Message: truncate(message, maxLogLine)})
		}
	}
	_, _ = io.Copy(io.Discard, stderr)
}

func (s *SeedsowFSSession) wait() {
	s.read.Wait()
	s.mu.Lock()
	s.exitErr = s.cmd.Wait()
	s.mu.Unlock()
	// Sweep any descendant the host left in the group.
	_ = procgroup.Signal(s.cmd, true)
	close(s.done)
}

// fail records the first protocol or read failure and ends the host: a stream
// this package cannot read is never interpreted.
func (s *SeedsowFSSession) fail(err error) {
	s.mu.Lock()
	if s.frameErr == nil {
		s.frameErr = err
	}
	s.mu.Unlock()
	_ = procgroup.Signal(s.cmd, true)
}

func (s *SeedsowFSSession) emit(event SeedsowFSEvent) {
	if s.events != nil {
		s.events(event)
	}
}

// parseFrame decodes one prefixed frame. A line without the prefix is host log
// output; a malformed or unknown frame is a protocol error, never a log line.
func parseFrame(line string) (hostFrame, bool, error) {
	if !strings.HasPrefix(line, framePrefix) {
		return hostFrame{}, false, nil
	}
	var frame hostFrame
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, framePrefix)), &frame); err != nil {
		return hostFrame{}, false, fmt.Errorf("%w: the Artemis Seedsow host sent a malformed frame: %v", ErrProtocol, err)
	}
	switch frame.Type {
	case "preparing", "progress", "held-ready", "stopped", "failed":
		return frame, true, nil
	default:
		return hostFrame{}, false, fmt.Errorf("%w: the Artemis Seedsow host sent an unknown frame type %q", ErrProtocol, frame.Type)
	}
}

// childEnv is the inherited environment without the two values this package
// owns, so a caller's shell cannot point the stage at another run tree.
func childEnv(values ...string) []string {
	owned := []string{"ANY2JB_RUNS_ROOT=", "ANY2JB_ARTEMIS_FS_RUNTIME="}
	env := make([]string, 0, len(os.Environ())+len(values))
	for _, entry := range os.Environ() {
		if slices.ContainsFunc(owned, func(prefix string) bool { return strings.HasPrefix(entry, prefix) }) {
			continue
		}
		env = append(env, entry)
	}
	return append(env, values...)
}

// writePreload writes the private Node preload that widens the read-only stage
// copy inside the run tree.
func writePreload(support string) (string, error) {
	directory := filepath.Join(support, "artemis")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	file := filepath.Join(directory, ".cp-preload.cjs")
	if err := os.WriteFile(file, cpPreload, 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(file, 0o600); err != nil {
		return "", err
	}
	return file, nil
}

// canonicalDir resolves the private support root. The pinned host stages only
// beneath an absolute canonical root and refuses anything else.
func canonicalDir(directory string) (string, error) {
	if !filepath.IsAbs(directory) {
		return "", errors.New("the app support directory must be an absolute path")
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", err
	}
	if resolved != directory {
		return "", fmt.Errorf("the app support directory %s is not canonical (%s)", directory, resolved)
	}
	return resolved, nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := value[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "..."
}
