package retained

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/internal/procgroup"
	"github.com/thedavidweng/any2jb/session"
)

var errProtocol = errors.New("invalid read-only tool protocol")

type toolEvent struct {
	Type                string   `json:"type"`
	Sequence            uint64   `json:"sequence"`
	Tool                string   `json:"tool"`
	Message             string   `json:"message,omitempty"`
	Endpoint            string   `json:"endpoint,omitempty"`
	Path                string   `json:"path,omitempty"`
	Total               int      `json:"total,omitempty"`
	ReadOnly            bool     `json:"readOnly,omitempty"`
	Ready               bool     `json:"ready,omitempty"`
	CleanupBlocked      bool     `json:"cleanupBlocked,omitempty"`
	CleanupVerified     bool     `json:"cleanupVerified,omitempty"`
	AttemptMarkerSHA256 string   `json:"attemptMarkerSha256,omitempty"`
	AttemptMarkers      []string `json:"attemptMarkers,omitempty"`
	Games               []Game   `json:"games,omitempty"`
}

// attemptMarkerName is the single attempt marker the homebrew supervisor may
// prove removed; any other name is a protocol violation.
const attemptMarkerName = ".mac2jb-hblauncher-attempt.json"

// verifiedCleanup accepts only the homebrew supervisor's stated proof shape: a
// verified cleanup, the recorded attempt marker, and its SHA-256 digest.
func verifiedCleanup(e toolEvent) bool {
	_, err := hex.DecodeString(e.AttemptMarkerSHA256)
	return e.CleanupVerified && len(e.AttemptMarkers) == 1 &&
		e.AttemptMarkers[0] == attemptMarkerName && len(e.AttemptMarkerSHA256) == 64 &&
		e.AttemptMarkerSHA256 == strings.ToLower(e.AttemptMarkerSHA256) && err == nil
}

type toolProcess struct {
	tool            string
	cmd             *exec.Cmd
	input           io.WriteCloser
	events          chan toolEvent
	done            chan struct{}
	err             error
	selection       chan string
	reportedFailure bool
	cleanupBlocked  bool
}

// Run satisfies the session adapter for tools that need no user parameter.
func (m *Manager) Run(ctx context.Context, run session.Run, tool string, report session.Report) error {
	return m.RunWith(ctx, run, session.ToolRequest{Tool: tool}, report)
}

// RunWith starts one retained tool. It returns when the tool has reported a
// verified result or a ready endpoint; an endpoint keeps running until Stop.
func (m *Manager) RunWith(ctx context.Context, run session.Run, request session.ToolRequest, report session.Report) error {
	if err := request.ValidateCodeConsent(); err != nil {
		return err
	}
	workspace, err := m.workspace(run)
	if err != nil {
		return err
	}
	if reason := m.Unavailable(request.Tool, run.Profile.ID); reason != "" {
		return errors.New(reason)
	}
	// A stale result file is not a lease: re-check the private evidence and ask
	// the live endpoint before every tool.
	outcome, err := engine.VerifyHeld(ctx, workspace)
	if err != nil || !outcome.Valid(true) {
		return errors.Join(errors.New("the retained session no longer holds a live verified lease"), err)
	}
	m.mu.Lock()
	if m.state.Lost {
		m.mu.Unlock()
		return errors.New("the retained session was lost; restart the console before another attempt")
	}
	if m.state.ModloaderInstalled {
		m.mu.Unlock()
		return errors.New("the modloader overlay stays active until the console restarts; finish this session and restart the console before launching another tool")
	}
	// An unproven homebrew shutdown leaves console state unknown; only a
	// console restart makes the workspace safe for another launcher.
	if (isHomebrew(request.Tool) || request.Tool == "modloader" || request.Tool == "savekit" || request.Tool == "savekit-read") && m.state.CleanupBlocked {
		m.mu.Unlock()
		return errors.New("the previous homebrew launcher did not prove a clean shutdown; restart the console before launching homebrew again")
	}
	if request.Tool == "savekit-read" || request.Tool == "savekit" {
		m.mu.Unlock()
		if request.Tool == "savekit-read" {
			return m.runSaveKit(ctx, run, request, workspace, report)
		}
		return m.runSaveKitWrite(ctx, run, request, workspace, report)
	}
	if m.active != nil {
		owner := m.active.tool
		m.mu.Unlock()
		return fmt.Errorf("%s already owns the retained session; stop it before starting another tool", owner)
	}
	if m.savekitOwner {
		owner := m.state.Tool
		m.mu.Unlock()
		return errors.New(owner + " already owns the retained session; wait for it to finish")
	}
	cmd, err := m.command(run, request, workspace)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if cmd == nil {
		m.mu.Unlock()
		return errors.New("missing read-only tool command")
	}
	p := &toolProcess{tool: request.Tool, cmd: cmd, events: make(chan toolEvent, 16),
		done: make(chan struct{}), selection: make(chan string, 1)}
	m.active = p
	// Console-state blocks outlive the tool that caused them.
	m.state = State{Tool: request.Tool, CleanupBlocked: m.state.CleanupBlocked,
		SaveKitBlocked: m.state.SaveKitBlocked}
	m.mu.Unlock()
	if err := m.start(ctx, p, report); err != nil {
		m.mu.Lock()
		m.active = nil
		m.mu.Unlock()
		return errors.Join(err, m.Stop(context.WithoutCancel(ctx), run))
	}
	err = m.observe(ctx, p, request, report)
	if err != nil || p.tool == "rpc-status" || p.tool == "system-code-dump" || p.tool == "modloader" {
		return errors.Join(err, m.Stop(context.WithoutCancel(ctx), run))
	}
	return nil
}

func (m *Manager) start(ctx context.Context, p *toolProcess, report session.Report) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := procgroup.Configure(p.cmd); err != nil {
		return err
	}
	output, err := p.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	// Raw tool stderr can contain console addresses and native details.
	p.cmd.Stderr = io.Discard
	p.input, err = p.cmd.StdinPipe()
	if err != nil {
		output.Close()
		return err
	}
	if err := p.cmd.Start(); err != nil {
		output.Close()
		p.input.Close()
		return err
	}
	go p.read(output)
	report(session.Progress{SideEffect: true,
		Processes: []session.Process{{PID: p.cmd.Process.Pid, Name: "tool"}}})
	return nil
}

func (p *toolProcess) read(output io.ReadCloser) {
	defer close(p.done)
	defer close(p.events)
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 65536)
	var sequence uint64
	state := ""
	delivered := false
	for scanner.Scan() {
		var e toolEvent
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		err := decoder.Decode(&e)
		if err == nil && decoder.Decode(new(any)) != io.EOF {
			err = errProtocol
		}
		valid := err == nil && e.Tool == p.tool && e.Sequence == sequence+1
		switch e.Type {
		case "started":
			valid = valid && state != "running" && state != "catalog"
		case "progress":
			valid = valid && state != "catalog"
		case "status":
			valid = valid && state == "" && e.Ready
		case "catalog":
			valid = valid && state != "running" && len(e.Games) > 0
		case "running":
			// A homebrew supervisor owns no endpoint; its running event only
			// states that the pinned frame loop proved itself.
			valid = valid && state != "running" && (e.ReadOnly && e.Endpoint != "" || isHomebrew(p.tool))
		case "complete":
			valid = valid && e.Path != "" && e.Total > 0
		case "ready":
			// Only the modloader reports a verified game handoff.
			valid = valid && p.tool == "modloader" && state != ""
		case "stopped":
			// A homebrew supervisor must state the cleanup proof shape the
			// stop path verifies before it allows another launch.
			valid = valid && state != "" && (!isHomebrew(p.tool) || verifiedCleanup(e))
		case "failed":
			p.reportedFailure = true
			p.cleanupBlocked = e.CleanupBlocked
			p.err = errors.New("the read-only tool reported a failure; see redacted diagnostics")
		default:
			valid = false
		}
		if !valid {
			p.err = errProtocol
			procgroup.Signal(p.cmd, true)
			break
		}
		sequence = e.Sequence
		if e.Type != "progress" {
			state = e.Type
		}
		// observe returns after its result; keep scanning and reaping without
		// leaving an unconsumed event queue between the child and Stop.
		if !delivered {
			p.events <- e
			switch e.Type {
			case "running", "ready", "stopped", "status", "complete":
				delivered = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		p.err = errProtocol
		procgroup.Signal(p.cmd, true)
	}
	p.err = errors.Join(p.err, p.cmd.Wait())
}

func (m *Manager) observe(ctx context.Context, p *toolProcess, request session.ToolRequest, report session.Report) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-p.events:
			if !ok {
				<-p.done
				if p.err != nil {
					return p.err
				}
				return errors.New("the read-only tool ended without a result")
			}
			switch event.Type {
			case "started", "progress":
				report(session.Progress{SideEffect: true})
			case "status":
				return nil
			case "complete":
				// This app chooses the destination: accept only a verified
				// export kept outside any private session workspace.
				if !strings.HasPrefix(event.Path, filepath.Join(m.options.Support, "verified-exports")+string(filepath.Separator)) {
					return errProtocol
				}
				m.mu.Lock()
				m.state.Export = event.Path
				m.mu.Unlock()
				return nil
			case "catalog":
				m.mu.Lock()
				m.state.Games = event.Games
				m.mu.Unlock()
				select {
				case <-ctx.Done():
					return ctx.Err()
				case title := <-p.selection:
					if _, err := io.WriteString(p.input, `{"type":"select","id":`+quote(title)+"}\n"); err != nil {
						return err
					}
				}
			case "running":
				if isHomebrew(p.tool) {
					return nil
				}
				if err := validEndpoint(p.tool, event.Endpoint, request.ConsoleIPv4); err != nil {
					return err
				}
				m.mu.Lock()
				m.state.Endpoint = event.Endpoint
				m.mu.Unlock()
				return nil
			case "stopped":
				return nil
			case "ready":
				m.mu.Lock()
				m.state.ModloaderInstalled = true
				m.mu.Unlock()
				return nil
			default:
				m.mu.Lock()
				m.state.CleanupBlocked = event.CleanupBlocked
				m.mu.Unlock()
				return errors.New("the read-only tool reported a failure; see redacted diagnostics")
			}
		}
	}
}

func quote(value string) string { return strconv.Quote(value) }

// validEndpoint keeps exposure inside the stated boundary: the console's own
// service, or a Mac-local bridge. No service is published on any other address.
func validEndpoint(tool, endpoint, consoleIPv4 string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return errProtocol
	}
	switch tool {
	case "ouncecake-webdav":
		if parsed.Scheme == "http" && parsed.Host == consoleIPv4+":2121" && parsed.Path == "/" {
			return nil
		}
	case "ouncecake-ftp":
		if parsed.Scheme == "ftp" && parsed.Hostname() == "127.0.0.1" && localPort(parsed.Port()) {
			return nil
		}
	case "sd-browser":
		port, convErr := strconv.Atoi(parsed.Port())
		if parsed.Scheme == "ftp" && parsed.Hostname() == consoleIPv4 && convErr == nil &&
			port >= 2122 && port <= 2199 && parsed.Path == "/sd/" {
			return nil
		}
	}
	return fmt.Errorf("%w: refused endpoint outside the stated boundary", errProtocol)
}

func localPort(value string) bool {
	port, err := strconv.Atoi(value)
	return err == nil && port > 0 && port <= 65535
}

// Select forwards one offered game choice to the running export.
func (m *Manager) Select(titleID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return errors.New("no read-only export is waiting for a selection")
	}
	for _, game := range m.state.Games {
		if game.TitleID != titleID {
			continue
		}
		select {
		case m.active.selection <- titleID:
			return nil
		default:
			return errors.New("a game was already selected for this export")
		}
	}
	return errors.New("choose one game from the current catalog")
}

// Stop ends this app's tool child and its process group. It never touches the
// host process or the retained lease.
func (m *Manager) Stop(ctx context.Context, _ session.Run) error {
	m.mu.Lock()
	p := m.active
	m.active = nil
	m.state.Endpoint, m.state.Games = "", nil
	cancel := m.savekitCancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if p == nil || p.cmd.Process == nil {
		return nil
	}
	// Cancellation can also leave observe before its first result. Drain while
	// waiting for shutdown so a full startup queue cannot prevent reaping.
	go func() {
		for range p.events {
		}
	}()
	if p.input != nil {
		io.WriteString(p.input, "{\"type\":\"stop\"}\n")
	}
	// Homebrew owner children confirm their own shutdown proofs; give the
	// supervisor the window its contract needs before escalating.
	graceful := 10 * time.Second
	if isHomebrew(p.tool) {
		graceful = 100 * time.Second
	}
	timer := time.NewTimer(graceful)
	defer timer.Stop()
	select {
	case <-p.done:
	case <-ctx.Done():
		if err := procgroup.Signal(p.cmd, true); err != nil {
			return err
		}
		<-p.done
	case <-timer.C:
		if err := procgroup.Signal(p.cmd, false); err != nil {
			return err
		}
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
			if err := procgroup.Signal(p.cmd, true); err != nil {
				return err
			}
			<-p.done
		}
	}
	// Remaining group members must not outlive the owning tool.
	if err := procgroup.Signal(p.cmd, true); err != nil {
		return err
	}
	if p.input != nil {
		p.input.Close()
	}
	m.mu.Lock()
	m.state.Tool = ""
	m.mu.Unlock()
	if p.err != nil {
		// A homebrew supervisor that exits without a protocol-reported failure
		// (protocol violation, kill, crash) left its cleanup unproven.
		if p.cleanupBlocked || isHomebrew(p.tool) && !p.reportedFailure {
			m.mu.Lock()
			m.state.CleanupBlocked = true
			m.mu.Unlock()
		}
		return fmt.Errorf("the read-only tool did not stop cleanly: %w", p.err)
	}
	return nil
}

var _ session.ToolRunner = (*Manager)(nil)
var _ session.ParameterizedToolRunner = (*Manager)(nil)
