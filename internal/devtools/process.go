package devtools

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/thedavidweng/any2jb/internal/procgroup"
)

const (
	// stopGrace is how long a child may take to exit after SIGTERM before the
	// whole process group is killed.
	stopGrace = 5 * time.Second
	// maxCapturedOutput caps the stdout kept for the UI, matching mac2jb.
	maxCapturedOutput = 2 << 20
	// maxLineBytes bounds one stdout or stderr line.
	maxLineBytes = 1 << 20
	// stderrTail caps the diagnostics kept for an exit error.
	stderrTail = 4096
)

// supervised owns one child process group and frames its stdout into lines.
// lines is closed after stdout reaches EOF; done is closed after the child is
// reaped. A consumer must drain lines until it closes.
type supervised struct {
	cmd    *exec.Cmd
	lines  chan string
	done   chan struct{}
	stderr *tail
	err    error
}

// startSupervised starts cmd in its own process group and streams its stdout.
func startSupervised(cmd *exec.Cmd) (*supervised, error) {
	if err := procgroup.Configure(cmd); err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdout.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, err
	}
	s := &supervised{cmd: cmd, lines: make(chan string), done: make(chan struct{}),
		stderr: &tail{limit: stderrTail}}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer close(s.lines)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
		for scanner.Scan() {
			s.lines <- strings.TrimSuffix(scanner.Text(), "\r")
		}
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(s.stderr, stderr)
	}()
	go func() {
		wg.Wait()
		s.err = cmd.Wait()
		close(s.done)
	}()
	return s, nil
}

// Stop ends the child's process group, escalating to SIGKILL after stopGrace.
func (s *supervised) Stop(ctx context.Context) error {
	return procgroup.Stop(ctx, s.cmd, s.done, true, stopGrace, stopGrace)
}

// runSupervised runs cmd to completion, forwarding stdout lines to onLine and
// stopping the whole group when ctx is done. It returns the captured stdout.
func runSupervised(ctx context.Context, cmd *exec.Cmd, onLine func(string)) (string, error) {
	s, err := startSupervised(cmd)
	if err != nil {
		return "", err
	}
	cancelled := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Stop(context.Background())
		case <-cancelled:
		}
	}()
	var captured strings.Builder
	for line := range s.lines {
		if captured.Len() < maxCapturedOutput {
			captured.WriteString(line)
			captured.WriteByte('\n')
		}
		if onLine != nil {
			onLine(line)
		}
	}
	close(cancelled)
	<-s.done
	if ctx.Err() != nil {
		return captured.String(), ctx.Err()
	}
	if s.err != nil {
		return captured.String(), s.exitError()
	}
	return captured.String(), nil
}

// exitError turns a failed exit into a message that names the child and keeps
// the last stderr diagnostics.
func (s *supervised) exitError() error {
	code := -1
	if s.cmd.ProcessState != nil {
		code = s.cmd.ProcessState.ExitCode()
	}
	name := filepath.Base(s.cmd.Path)
	if detail := strings.TrimSpace(s.stderr.String()); detail != "" {
		if index := strings.LastIndexByte(detail, '\n'); index >= 0 {
			detail = detail[index+1:]
		}
		return fmt.Errorf("%s exited with status %d: %s", name, code, detail)
	}
	return fmt.Errorf("%s exited with status %d", name, code)
}

// tail keeps the last limit bytes written to it.
type tail struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.limit {
		copy(t.buf, t.buf[len(t.buf)-t.limit:])
		t.buf = t.buf[:t.limit]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
