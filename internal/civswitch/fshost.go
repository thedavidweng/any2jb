package civswitch

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/session"
)

const (
	fsHostComponent = "civswitch-fs"
	fsHostPrefix    = "MAC2JB_CIVSWITCH_FS "
	fsHostScript    = "integrations/civswitch/fs-host.cjs"
	// fsHostEventLimit matches the GUI: one supervisor event is small JSON.
	fsHostEventLimit = 8192
)

// FSHostRequest describes one CivSwitch FS supervisor run. ToolRoot is the
// versioned plugin stage, so the supervisor runs only from verified staged code.
type FSHostRequest struct {
	Manifest   *manifest.Manifest
	ToolRoot   string
	NodeBinary string
	// Support holds runs/, where the staged FS port and its held run live.
	Support       string
	SwitchIPv4    string
	HostIPv4      string
	WaitForLoader bool
	// AllowLoopback permits 127.0.0.1 peers in loopback fixtures.
	AllowLoopback bool
}

// FSHostEvent is one prefixed supervisor observation. Only fixed fields cross
// the boundary: the supervisor's own log text is never forwarded.
type FSHostEvent struct {
	Type    string
	Message string
	RunPath string
	LogPath string
}

// FSHostSession is one running supervisor. Wait returns only after the process
// exited, so a caller can distinguish a verified stop from a crash.
type FSHostSession struct {
	cmd  *exec.Cmd
	done chan struct{}

	mu      sync.Mutex
	waitErr error
	runPath string
	failed  bool
	stopped bool
	exit    bool
}

// StartFSHost verifies the staged CivSwitch FS plugin and starts its pinned
// supervisor. The returned session stays live until Wait or Stop.
func StartFSHost(ctx context.Context, request FSHostRequest, onEvent func(FSHostEvent)) (*FSHostSession, error) {
	if request.Manifest == nil {
		return nil, errors.New("CivSwitch FS requires the canonical manifest")
	}
	if !filepath.IsAbs(request.ToolRoot) {
		return nil, errors.New("the CivSwitch FS plugin stage root must be an absolute path")
	}
	if !filepath.IsAbs(request.Support) {
		return nil, errors.New("the CivSwitch support root must be an absolute path")
	}
	// The console is reached exactly like the loader probe reaches it; the host
	// address is this machine's own private LAN address.
	if !consoleAddress(request.SwitchIPv4) {
		return nil, errors.New("enter the Switch IPv4 address on the same reachable LAN")
	}
	if !privateIPv4(request.HostIPv4, request.AllowLoopback) {
		return nil, errors.New("enter this computer's IPv4 address on the same reachable LAN")
	}
	node, err := executable(request.NodeBinary, "the CivSwitch FS host needs a Node runtime")
	if err != nil {
		return nil, err
	}
	stage := filepath.Join(request.ToolRoot, "tools", fsHostComponent)
	if err := request.Manifest.VerifyToolSource(stage, fsHostComponent); err != nil {
		return nil, fmt.Errorf("the CivSwitch FS plugin is not installed or failed verification: %w", err)
	}
	script := filepath.Join(stage, filepath.FromSlash(fsHostScript))
	if _, err := os.Lstat(script); err != nil {
		return nil, err
	}
	// The staged plugin is read-only. The supervisor copies that tree into the
	// runs directory, which makes every copied file read-only too, so a preload
	// widens permissions on the copy only: never on the verified stage.
	preload, err := writeCopyPreload(request.Support)
	if err != nil {
		return nil, err
	}
	args := []string{"--require", preload, script, request.SwitchIPv4, request.HostIPv4, request.Support}
	if request.WaitForLoader {
		args = append(args, "--wait-for-loader")
	}
	cmd := exec.Command(node, args...)
	cmd.Dir = request.ToolRoot
	cmd.Env = append(os.Environ(), "ANY2JB_RUNS_ROOT="+filepath.Join(request.Support, "runs"))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	session := &FSHostSession{cmd: cmd, done: make(chan struct{})}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		session.collect(stdout, onEvent)
	}()
	go func() {
		err := cmd.Wait()
		// Wait closes the pipe; draining it first keeps a final failed or
		// stopped event from being lost to the exit race.
		<-collected
		session.mu.Lock()
		if err == nil && session.failed {
			err = errors.New("the CivSwitch FS host reported a failure")
		}
		if err == nil && !session.stopped {
			err = errors.New("the CivSwitch FS host exited without confirming its stop")
		}
		session.waitErr, session.exit = err, true
		session.mu.Unlock()
		close(session.done)
	}()
	stop := context.AfterFunc(ctx, func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = session.Stop(stopCtx)
	})
	go func() {
		<-session.done
		stop()
	}()
	return session, nil
}

// StartCivSwitchFSHost runs the staged CivSwitch FS supervisor for the session
// and blocks until it stops. Cancelling ctx interrupts it at the same safe
// checkpoint the supervisor's own stop button uses.
func (a *Adapter) StartCivSwitchFSHost(ctx context.Context, request session.CivSwitchFSRequest, onEvent func(session.CivSwitchFSEvent)) error {
	run, err := StartFSHost(ctx, FSHostRequest{
		Manifest:      a.options.Manifest,
		ToolRoot:      a.options.ToolRoot,
		NodeBinary:    a.options.NodeBinary,
		Support:       a.options.Support,
		SwitchIPv4:    request.SwitchIPv4,
		HostIPv4:      request.HostIPv4,
		WaitForLoader: request.WaitForLoader,
		AllowLoopback: a.options.AllowLoopback,
	}, func(event FSHostEvent) {
		if onEvent != nil {
			onEvent(session.CivSwitchFSEvent{Type: event.Type, Message: event.Message,
				RunPath: event.RunPath, LogPath: event.LogPath})
		}
	})
	if err != nil {
		return err
	}
	return run.Wait()
}

var _ session.CivSwitchFSHost = (*Adapter)(nil)

func (s *FSHostSession) collect(stdout io.Reader, onEvent func(FSHostEvent)) {
	reader := bufio.NewReaderSize(stdout, 64<<10)
	for {
		line, err := readLine(reader, fsHostEventLimit)
		if strings.HasPrefix(line, fsHostPrefix) {
			var event FSHostEvent
			if json.Unmarshal([]byte(strings.TrimPrefix(line, fsHostPrefix)), &event) == nil && event.Type != "" {
				s.mu.Lock()
				if event.Type == "held-ready" && event.RunPath != "" {
					s.runPath = event.RunPath
				}
				s.failed = s.failed || event.Type == "failed"
				s.stopped = s.stopped || event.Type == "stopped"
				s.mu.Unlock()
				if onEvent != nil {
					onEvent(event)
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// Wait blocks until the supervisor exited and reports its outcome.
func (s *FSHostSession) Wait() error {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waitErr
}

// RunPath is the verified held run the supervisor reported, if any.
func (s *FSHostSession) RunPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runPath
}

// Stop interrupts the supervisor at its next safe checkpoint, then terminates
// it if the verified stop does not arrive before the deadline.
func (s *FSHostSession) Stop(ctx context.Context) error {
	s.mu.Lock()
	running := !s.exit
	s.mu.Unlock()
	if !running {
		return s.Wait()
	}
	_ = s.cmd.Process.Signal(syscall.SIGINT)
	select {
	case <-s.done:
		return s.Wait()
	case <-ctx.Done():
	}
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-s.done:
		return s.Wait()
	case <-time.After(5 * time.Second):
	}
	_ = s.cmd.Process.Kill()
	return s.Wait()
}

// privateIPv4 accepts only the LAN ranges a console and a host can share. A
// loopback fixture has to ask for 127.0.0.1 explicitly.
func privateIPv4(address string, allowLoopback bool) bool {
	if allowLoopback && address == "127.0.0.1" {
		return true
	}
	ip, err := netip.ParseAddr(address)
	return err == nil && ip.Is4() && ip.IsPrivate()
}

func executable(path, message string) (string, error) {
	if message == "" {
		message = "a required runtime is missing or not executable"
	}
	if path == "" {
		return "", errors.New(message)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return "", errors.New(message)
	}
	return path, nil
}

// copyPreload widens permissions on the supervisor's own copy of the staged
// read-only port. It never touches a file outside the runs root, so the
// verified plugin stage keeps its 0400 digests.
const copyPreload = `'use strict';
const fsp = require('node:fs/promises');
const path = require('node:path');

const runs = process.env.ANY2JB_RUNS_ROOT;
if (runs && path.isAbsolute(runs)) {
  const owned = target => typeof target === 'string' &&
    (target === runs || target.startsWith(runs + path.sep));
  const relax = async target => {
    const info = await fsp.lstat(target).catch(() => null);
    if (!info || info.isSymbolicLink()) return;
    if (info.isDirectory()) {
      await fsp.chmod(target, 0o700);
      for (const name of await fsp.readdir(target)) await relax(path.join(target, name));
    } else if (info.isFile()) {
      await fsp.chmod(target, 0o600);
    }
  };
  const copy = fsp.cp;
  fsp.cp = async (source, destination, options) => {
    const result = await copy(source, destination, options);
    if (owned(destination)) await relax(destination);
    return result;
  };
}
`

// writeCopyPreload writes the copy preload into retained support storage with a
// rename, so a stale or linked file is replaced rather than followed.
func writeCopyPreload(support string) (string, error) {
	dir := filepath.Join(support, "civswitch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, ".cp-preload-*.cjs")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(copyPreload); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	target := filepath.Join(dir, "cp-preload.cjs")
	if err := os.Rename(file.Name(), target); err != nil {
		return "", err
	}
	return target, nil
}
