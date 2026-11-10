package transport

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thedavidweng/any2jb/session"
)

type helperProcess struct {
	owner    *Darwin
	run      session.Run
	cmd      *exec.Cmd
	input    io.WriteCloser
	lines    chan string
	done     chan struct{}
	err      error
	mu       sync.Mutex
	count    int
	closed   bool
	closeErr error
	released bool
	lease    *os.File
	audit    *usbAudit
}

func (d *Darwin) start(ctx context.Context, run session.Run, workspace string) (*helperProcess, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.active != nil {
		return nil, errors.New("a USB writer is already active")
	}
	if run.ID == "" {
		return nil, errors.New("missing USB run identity")
	}
	if err := d.Check(ctx, run); err != nil {
		return nil, err
	}
	env, err := d.environment()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := d.command("keyboard")
	if cmd == nil {
		return nil, errors.New("missing helper command")
	}
	// Do not inherit MAC2JB_READY_FD, controller probing, or timing overrides.
	var filtered []string
	for _, entry := range cmd.Environ() {
		if !strings.HasPrefix(entry, "MAC2JB_") && !strings.HasPrefix(entry, "DYLD_") {
			filtered = append(filtered, entry)
		}
	}
	cmd.Env = append(filtered, env...)
	if err := configureHelper(cmd); err != nil {
		return nil, err
	}
	lease, err := d.claim(run)
	if err != nil {
		return nil, err
	}
	started := false
	audit, err := d.audit(run, workspace)
	if err != nil {
		if lease != nil {
			lease.Close()
		}
		return nil, err
	}
	defer func() {
		if !started && lease != nil {
			lease.Close()
		}
		if !started && audit != nil {
			audit.file.Close()
		}
	}()
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	cmd.Stderr = io.Discard // never expose text/pairing material
	p := &helperProcess{owner: d, run: run, cmd: cmd, input: input, lines: make(chan string, 32), done: make(chan struct{}), lease: lease, audit: audit}
	if err := cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, err
	}
	d.active = p
	started = true
	go func() {
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 4096), 65536)
		for scanner.Scan() {
			p.lines <- scanner.Text()
		}
		p.err = errors.Join(scanner.Err(), cmd.Wait())
		close(p.lines)
		close(p.done)
	}()
	go func() {
		select {
		case <-ctx.Done():
			terminateHelper(cmd, false)
		case <-p.done:
			return
		}
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-p.done:
		case <-timer.C:
			terminateHelper(cmd, true)
		}
	}()
	return p, nil
}

func (p *helperProcess) next(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case line, ok := <-p.lines:
		if ok {
			if line == "MAC2JB_KEYBOARD_RELEASED" {
				p.released = true
			}
			return line, nil
		}
		<-p.done
		if p.err != nil {
			return "", errors.New("USB helper failed; inspect and clear partial console input before restarting")
		}
		return "", io.EOF
	}
}

func (p *helperProcess) clear() {
	p.owner.mu.Lock()
	defer p.owner.mu.Unlock()
	if p.owner.active == p {
		p.owner.active = nil
	}
}

func (p *helperProcess) close(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	force := time.AfterFunc(2*time.Second, func() { terminateHelper(p.cmd, true) })
	defer force.Stop()
	// An unacknowledged Send must not keep focus loss/Stop waiting on its
	// mutex. The helper's signal handler releases keys before exiting.
	if !p.mu.TryLock() {
		terminateHelper(p.cmd, false)
		p.mu.Lock()
	}
	defer p.mu.Unlock()
	if p.closed {
		return p.closeErr
	}
	p.closed = true
	var err error
	// Acknowledge the neutral report before closing stdin. EOF makes the
	// helper attempt release again even if the explicit write fails.
	if !p.released {
		err = p.write(ctx, [8]byte{})
	}
	p.input.Close()
	waiting := true
	for waiting {
		select {
		case line, ok := <-p.lines:
			if !ok {
				waiting = false
			} else if line == "MAC2JB_KEYBOARD_RELEASED" {
				p.released = true
			}
		case <-ctx.Done():
			err = errors.Join(err, ctx.Err())
			terminateHelper(p.cmd, true)
			waiting = false
		}
	}
	// Unblock the bounded output reader even when cancellation won the select.
	for line := range p.lines {
		if line == "MAC2JB_KEYBOARD_RELEASED" {
			p.released = true
		}
	}
	<-p.done
	if p.err != nil {
		err = errors.Join(err, errors.New("keyboard helper exited with an error"))
	}
	status := "neutral_release_confirmed"
	if !p.released {
		status = "neutral_release_unconfirmed"
	}
	err = errors.Join(err, p.audit.event(status, "delivered_reports="+strconv.Itoa(p.count)), p.audit.close())
	// The leader may have exited with children still in its owned group.
	terminateHelper(p.cmd, true)
	err = errors.Join(err, p.releaseLease())
	p.clear()
	if !p.released {
		err = errors.Join(err, errors.New("neutral release unconfirmed; reconnect the USB cable if a key repeats"))
	}
	p.closeErr = err
	return err
}
