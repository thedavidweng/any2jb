package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/thedavidweng/any2jb/internal/diagnostics"
	"github.com/thedavidweng/any2jb/internal/procgroup"
	"github.com/thedavidweng/any2jb/internal/session"
)

var ErrProtocol = errors.New("invalid engine bridge protocol")

type CommandFactory func(session.Run, string) *exec.Cmd

// HostRunner owns one process group and one exclusive on-disk session lease.
type HostRunner struct {
	support string
	launch  func(session.Run, string) (*exec.Cmd, error)
	mu      sync.Mutex
	active  *hostProcess
}

type hostProcess struct {
	run        session.Run
	workspace  string
	cmd        *exec.Cmd
	input      io.WriteCloser
	events     chan bridgeEvent
	done       chan struct{}
	err        error // published by closing done
	held       bool
	outcome    session.Outcome
	sideEffect atomic.Bool
}

// NewHostRunner is the production constructor. A successful Stage, matching
// bundled or developer provenance, and fresh VerifyProfile are mandatory on
// every launch.
func NewHostRunner(options Options) *HostRunner {
	r := &HostRunner{support: options.Support}
	r.launch = func(run session.Run, workspace string) (*exec.Cmd, error) {
		if options.Manifest == nil {
			return nil, errors.New("missing runtime manifest")
		}
		profile, err := options.Manifest.Profile(run.Profile.ID)
		if err != nil {
			return nil, err
		}
		if profile.Engine == "izmysh" {
			return nil, errors.New("IZMYSH requires its separate native qualification adapter")
		}
		developerMode := options.DeveloperSourceRoot != "" || options.DeveloperProfileID != ""
		if developerMode {
			if options.DeveloperSourceRoot == "" || options.DeveloperProfileID != run.Profile.ID {
				return nil, errors.New("engine launch requires explicit developer staging for the selected profile")
			}
		} else if !profile.Distribution.Available() {
			return nil, fmt.Errorf("engine %s unavailable: %s", profile.ID, profile.Distribution.Reason)
		}
		root, err := Stage(options)
		if err != nil {
			return nil, err
		}
		record, err := ReadRecord(root)
		if err != nil {
			return nil, err
		}
		if run.Profile.Revision != profile.Revision || run.Profile.SourceSHA256 != profile.SourceSHA256 ||
			run.Profile.BootstrapSHA256 != profile.BootstrapSHA256 || run.Profile.StartPort != profile.StartPort ||
			run.Profile.StartArgumentCount != profile.StartArgumentCount || run.Profile.RetainsRPC != profile.SupportsHeldRPC {
			return nil, errors.New("engine profile identity mismatch")
		}
		if developerMode {
			if record.Source != "developer-staged" || record.Redistributable || record.ProfileID != run.Profile.ID ||
				record.Revision != profile.Revision || record.SourceSHA256 != profile.SourceSHA256 {
				return nil, errors.New("developer engine identity mismatch")
			}
		} else if record.Source != "bundled" || !record.Redistributable {
			return nil, errors.New("bundled engine provenance mismatch")
		}
		if err := options.Manifest.VerifyProfile(root, profile.ID); err != nil {
			return nil, err
		}
		return exec.Command(filepath.Join(root, "runtime/node"), filepath.Join(root, "runtime/bridge.cjs"),
			filepath.Join(root, "profiles.json"), profile.ID, "--launch", workspace, run.ID, run.LANIPv4), nil
	}
	return r
}

// NewProtocolRunner exposes the structured-process seam for fixture harnesses.
// The caller supplies a trusted executable. App startup must use NewHostRunner,
// not this constructor; this seam does not authorize upstream engine execution.
func NewProtocolRunner(support string, command CommandFactory) *HostRunner {
	return &HostRunner{support: support, launch: func(run session.Run, workspace string) (*exec.Cmd, error) {
		return command(run, workspace), nil
	}}
}

type bridgeEvent struct {
	Type     string `json:"type"`
	Sequence uint64 `json:"sequence"`
	RunID    string `json:"runID"`
	Command  string `json:"command,omitempty"`
	Stage    int    `json:"stage,omitempty"`
}

// PairingDetails is ephemeral input-adapter data, never diagnostics or a snapshot.
type PairingDetails struct {
	RunID, Workspace, Command string
}

func Pairing(host session.PreparedHost) (PairingDetails, error) {
	p, ok := host.Opaque.(PairingDetails)
	if !ok || !host.Listening || !host.PairingValid {
		return PairingDetails{}, errors.New("host has no validated pairing details")
	}
	return p, nil
}

// PairingFor validates the identity and command again at the input boundary.
func PairingFor(host session.PreparedHost, run session.Run) (PairingDetails, error) {
	p, err := Pairing(host)
	if err != nil || p.RunID != run.ID || !validCommand(p.Command, run) {
		return PairingDetails{}, errors.New("host pairing does not match this run")
	}
	return p, nil
}

func (r *HostRunner) Prepare(ctx context.Context, run session.Run, report session.Report) (host session.PreparedHost, err error) {
	r.mu.Lock()
	var created *hostProcess
	defer func() {
		r.mu.Unlock()
		if err != nil && created != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 305*time.Second)
			err = errors.Join(err, r.Cleanup(cleanupCtx, run))
			cancel()
			if created.sideEffect.Load() {
				report(session.Progress{SideEffect: true})
			}
		}
	}()
	if r.active != nil {
		return session.PreparedHost{}, errors.New("a session lease is already active")
	}
	if err := ctx.Err(); err != nil {
		return session.PreparedHost{}, err
	}
	if run.ID == "" {
		return session.PreparedHost{}, errors.New("missing run identity")
	}
	if err := privateDir(r.support); err != nil {
		return session.PreparedHost{}, err
	}
	lease, err := os.OpenFile(filepath.Join(r.support, "session.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return session.PreparedHost{}, fmt.Errorf("session lease unavailable: %w", err)
	}
	if _, err = lease.WriteString(run.ID); err != nil {
		lease.Close()
		os.Remove(filepath.Join(r.support, "session.lock"))
		return session.PreparedHost{}, err
	}
	if err := lease.Close(); err != nil {
		return session.PreparedHost{}, errors.Join(err, os.Remove(filepath.Join(r.support, "session.lock")))
	}
	if err := (diagnostics.Store{Support: r.support}).RecordOwner(run.ID); err != nil {
		os.Remove(filepath.Join(r.support, "session.lock"))
		return session.PreparedHost{}, err
	}
	workspace, err := NewWorkspace(r.support)
	if err != nil {
		os.Remove(filepath.Join(r.support, "session.lock"))
		return session.PreparedHost{}, err
	}
	p := &hostProcess{run: run, workspace: workspace, events: make(chan bridgeEvent, 16), done: make(chan struct{})}
	r.active = p
	created = p
	if err := os.WriteFile(filepath.Join(workspace, "diagnostics-id"), []byte(run.ID), 0600); err != nil {
		return session.PreparedHost{}, err
	}
	cmd, err := r.launch(run, workspace)
	if err != nil {
		return session.PreparedHost{}, err
	}
	if err := ctx.Err(); err != nil {
		return session.PreparedHost{}, err
	}
	if cmd == nil {
		return session.PreparedHost{}, errors.New("missing engine command")
	}
	cmd.Dir = workspace
	p.cmd = cmd
	var output io.ReadCloser
	p.input, output, err = procgroup.Start(cmd)
	if err != nil {
		return session.PreparedHost{}, err
	}
	go p.read(output)
	report(session.Progress{Processes: []session.Process{{PID: cmd.Process.Pid, Name: "engine"}}})
	for {
		event, err := p.next(ctx)
		if err != nil {
			return session.PreparedHost{}, err
		}
		if event.Type != "ready" || !validCommand(event.Command, run) {
			return session.PreparedHost{}, ErrProtocol
		}
		report(session.Progress{SideEffect: true})
		return session.PreparedHost{Listening: true, PairingValid: true,
			Opaque: PairingDetails{RunID: run.ID, Workspace: workspace, Command: event.Command}}, nil
	}
}

func validCommand(command string, run session.Run) bool {
	if !strings.HasPrefix(command, "V=START(") || !strings.HasSuffix(command, ")") {
		return false
	}
	args := strings.Split(strings.TrimSuffix(strings.TrimPrefix(command, "V=START("), ")"), ",")
	if len(args) != run.Profile.StartArgumentCount || (len(args) != 5 && len(args) != 9) {
		return false
	}
	ip, err := netip.ParseAddr(run.LANIPv4)
	if err != nil || !ip.Is4() {
		return false
	}
	for i, arg := range args {
		n, err := strconv.ParseUint(arg, 10, 53)
		if err != nil || strconv.FormatUint(n, 10) != arg {
			return false
		}
		if i == 0 && n != uint64(run.Profile.StartPort) || i > 0 && i < 5 && n != uint64(ip.As4()[i-1]) {
			return false
		}
	}
	return true
}

func (p *hostProcess) read(output io.ReadCloser) {
	defer close(p.done)
	defer close(p.events)
	stream := procgroup.NewNDJSONReader(output, 65536, func(e bridgeEvent) uint64 { return e.Sequence })
	state, stage := "", 0
	for {
		e, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		valid := err == nil && e.RunID == p.run.ID
		switch e.Type {
		case "ready":
			valid = valid && state == "" && e.Stage == 0 && validCommand(e.Command, p.run)
		case "connected":
			valid = valid && state == "ready" && e.Stage == 0 && e.Command == ""
		case "stage":
			valid = valid && (state == "connected" || state == "stage") && e.Stage == stage+1 && e.Stage <= 4 && e.Command == ""
			stage = e.Stage
		case "held":
			valid = valid && state == "stage" && stage == 4 && p.run.Profile.RetainsRPC && e.Stage == 0 && e.Command == ""
		case "result":
			valid = valid && (state == "held" || state == "stage" && stage == 4) && e.Stage == 0 && e.Command == ""
		case "failed":
			valid = valid && state != "result" && state != "failed" && e.Command == "" && e.Stage == 0
		default:
			valid = false
		}
		if !valid {
			p.err = ErrProtocol
			procgroup.Signal(p.cmd, true)
			break
		}
		state = e.Type
		log, err := os.OpenFile(filepath.Join(p.workspace, "host.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			p.err = err
			procgroup.Signal(p.cmd, true)
			break
		}
		_, writeErr := fmt.Fprintf(log, "%s stage=%d\n", e.Type, e.Stage)
		p.err = errors.Join(writeErr, log.Close())
		if p.err != nil {
			procgroup.Signal(p.cmd, true)
			break
		}
		if e.Type == "ready" {
			p.sideEffect.Store(true)
		}
		p.events <- e
	}
	p.err = errors.Join(p.err, p.cmd.Wait())
	if p.err == nil && state != "result" {
		p.err = errors.New("engine exited without final verification")
	}
}

func (p *hostProcess) next(ctx context.Context) (bridgeEvent, error) {
	select {
	case <-ctx.Done():
		return bridgeEvent{}, ctx.Err()
	case event, ok := <-p.events:
		if !ok {
			<-p.done
			if p.err != nil {
				return bridgeEvent{}, p.err
			}
			return bridgeEvent{}, errors.New("engine protocol ended")
		}
		if event.Type == "failed" {
			return bridgeEvent{}, errors.New("engine reported failure")
		}
		return event, nil
	}
}

func (r *HostRunner) owned(run session.Run) (*hostProcess, error) {
	if r.active == nil || r.active.run.ID != run.ID {
		return nil, errors.New("run does not own the session lease")
	}
	return r.active, nil
}

func (r *HostRunner) Run(ctx context.Context, run session.Run, host session.PreparedHost, report session.Report) (session.Outcome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, err := r.owned(run)
	if err != nil {
		return session.Outcome{}, err
	}
	details, err := Pairing(host)
	if err != nil || details.RunID != run.ID || details.Workspace != p.workspace {
		return session.Outcome{}, ErrProtocol
	}
	for {
		event, err := p.next(ctx)
		if err != nil {
			return session.Outcome{}, err
		}
		switch event.Type {
		case "connected", "stage":
			report(session.Progress{SideEffect: true, Current: event.Stage, Total: 4})
		case "held", "result":
			held := event.Type == "held"
			if held != run.Profile.RetainsRPC {
				return session.Outcome{}, ErrProtocol
			}
			if held {
				select {
				case <-p.done:
					return session.Outcome{}, errors.New("held engine exited before verification")
				default:
				}
			}
			if !held {
				select {
				case <-ctx.Done():
					return session.Outcome{}, ctx.Err()
				case <-p.done:
				}
				if p.err != nil {
					return session.Outcome{}, p.err
				}
			}
			outcome, err := verifyResult(ctx, p.workspace, held)
			if err == nil {
				p.held, p.outcome = held, outcome
			}
			return outcome, err
		default:
			return session.Outcome{}, ErrProtocol
		}
	}
}

func (r *HostRunner) Wait(ctx context.Context, run session.Run) error {
	r.mu.Lock()
	p, err := r.owned(run)
	r.mu.Unlock()
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		if p.err != nil {
			return p.err
		}
		return errors.New("owned host exited")
	}
}

// Workspace resolves the private workspace of a run this runner owns, only
// after its retained lease was verified. Tools never receive another run's path.
func (r *HostRunner) Workspace(run session.Run) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, err := r.owned(run)
	if err != nil {
		return "", err
	}
	if !p.held {
		return "", errors.New("this run has no verified retained lease")
	}
	return p.workspace, nil
}

func (r *HostRunner) Finish(ctx context.Context, run session.Run) (session.Outcome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, err := r.owned(run)
	if err != nil {
		return session.Outcome{}, err
	}
	if !p.held {
		return p.outcome, nil
	}
	if _, err := io.WriteString(p.input, "{\"type\":\"finish\"}\n"); err != nil {
		return session.Outcome{}, err
	}
	event, err := p.next(ctx)
	if err != nil || event.Type != "result" {
		return session.Outcome{}, errors.Join(ErrProtocol, err)
	}
	select {
	case <-ctx.Done():
		return session.Outcome{}, ctx.Err()
	case <-p.done:
	}
	if p.err != nil {
		return session.Outcome{}, p.err
	}
	return verifyResult(ctx, p.workspace, false)
}

func (r *HostRunner) Cleanup(ctx context.Context, run session.Run) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil {
		return nil
	}
	p, err := r.owned(run)
	if err != nil {
		return err
	}
	if p.cmd != nil && p.cmd.Process != nil {
		grace := 2 * time.Second
		if p.run.Profile.RetainsRPC {
			grace = 300 * time.Second
		}
		if err := procgroup.Stop(ctx, p.cmd, p.done, true, grace, 0); err != nil {
			return err
		}
	}
	lock := filepath.Join(r.support, "session.lock")
	info, err := os.Lstat(lock)
	if err != nil || !info.Mode().IsRegular() || !ownedFile(info) {
		return errors.New("invalid session lease")
	}
	owner, err := os.ReadFile(lock)
	if err != nil || string(owner) != run.ID {
		return errors.New("session lease ownership changed")
	}
	diagnosticErr := (diagnostics.Store{Support: r.support}).Capture(p.workspace, run.ID)
	if err := (diagnostics.Store{Support: r.support}).RetainBackups(p.workspace); err != nil {
		return err
	}
	if err := os.RemoveAll(p.workspace); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(r.support, "session-owner.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(lock); err != nil {
		return err
	}
	if p.input != nil {
		p.input.Close()
	}
	r.active = nil
	return diagnosticErr
}

var _ session.HostRunner = (*HostRunner)(nil)
