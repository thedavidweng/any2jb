// Package retained owns the lease of a verified held session and the read-only
// tools that may use it. The lease belongs to one verified run at a time: a
// tool runs only after a fresh live check, and only one tool owns the session.
package retained

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/launcher"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/savekit"
	"github.com/thedavidweng/any2jb/session"
)

// Workspaces resolves the private workspace of a run this app owns.
type Workspaces interface {
	Workspace(session.Run) (string, error)
}

type Options struct {
	Support string
	// Node and Host are the staged Node binary and the original any2jb tool
	// host. Empty values leave every tool unavailable.
	Node, Host string
	// ToolRoot is the developer stage of the pinned tool sources, and Staged
	// lists the component IDs verified in it. Packaged startup leaves both empty.
	ToolRoot string
	Staged   []string
	// EngineStaged records a developer engine stage, which the system-code
	// capture module is part of. It is never set by a packaged app.
	EngineStaged bool
	// StageError records a failed local developer stage so an unavailable tool
	// can say that integrity checking failed instead of staying silent.
	StageError error
	// EngineRoot is the verified engine stage the native launcher checks its
	// ABI against, and HomebrewRuntime the prepared portable runtime root.
	// Both exist only for a developer stage; a packaged app leaves them empty.
	EngineRoot      string
	HomebrewRuntime string
	Manifest        *manifest.Manifest
	Runs            Workspaces
	// SaveKitModule is the explicit host-module test seam, mirroring
	// CommandFactory. Production leaves it empty and uses the embedded module.
	SaveKitModule string
	// SaveKitScenario selects the structured fixture's behavior. It is a test
	// seam and stays empty in production.
	SaveKitScenario string
}

// homebrewComponents lists the staged components each homebrew launcher needs
// before it may run. Every component is verified against the canonical
// manifest before staging; distribution policy still gates the tool itself.
var homebrewComponents = map[string][]string{
	"mintbrew":          {"mintbrew", "homebrew-runtime"},
	"native-hblauncher": {"mintbrew", "mintbrew-native"},
	"izlauncher":        {"izlauncher", "izlauncher-blitbun", "izlauncher-shell", "izlauncher-vacootube", "homebrew-runtime"},
	"custom-homebrew":   {"mintbrew", "homebrew-runtime"},
}

func isHomebrew(tool string) bool {
	_, ok := homebrewComponents[tool]
	return ok
}

// Game is a console-reported title offered for a read-only export.
type Game struct {
	TitleID string `json:"titleId"`
	Name    string `json:"name"`
}

// SaveKitWrite is the reported outcome of one gated save operation. Verified
// is set only after the post-write read-back matched (or, for a compare, the
// live save matched the export); it is never assumed. Backup keeps the
// verified pre-operation backup accessible for recovery. A nonempty Failure
// blocks further mutations until the console restarts: nothing is retried.
type SaveKitWrite struct {
	Action   string
	Verified bool
	Files    int
	Replaced int
	Backup   string
	Failure  string
}

// SaveKitState accumulates save observations for the UI: each listing and
// operation outcome stays visible until the lease ends. It is ephemeral
// tool-owned data, not session state.
type SaveKitState struct {
	Users     []string
	Titles    []string
	Inventory []savekit.Entry
	Action    string
	Output    string
	Path      string
	Write     *SaveKitWrite
}

// State is ephemeral tool-owned data for the UI. It is not session state.
type State struct {
	Tool           string
	Endpoint       string
	Export         string
	Games          []Game
	SaveKit        *SaveKitState
	Lost           bool
	CleanupBlocked bool
	SaveKitBlocked string
	// ModloaderInstalled records a verified modloader handoff. Its overlay
	// stays resident until the console restarts, so no further tool may run
	// in this boot.
	ModloaderInstalled bool
}

// CommandFactory is the explicit trusted-process test seam, mirroring the
// engine's. Production composition uses New, never this seam.
type CommandFactory func(session.Run, session.ToolRequest, string) *exec.Cmd

type Manager struct {
	options       Options
	command       func(session.Run, session.ToolRequest, string) (*exec.Cmd, error)
	mu            sync.Mutex
	attached      string
	active        *toolProcess
	savekitOwner  bool
	savekitCancel context.CancelFunc
	state         State
}

func New(options Options) *Manager {
	m := &Manager{options: options}
	m.command = func(run session.Run, request session.ToolRequest, workspace string) (*exec.Cmd, error) {
		args := []string{options.Host, options.ToolRoot, request.Tool, workspace,
			dash(request.ConsoleIPv4), dash(m.exportPath(run, request.Tool))}
		if isHomebrew(request.Tool) || request.Tool == "modloader" {
			config, err := m.homebrewConfig(run, request)
			if err != nil {
				return nil, err
			}
			args = append(args, config)
		}
		return exec.Command(options.Node, args...), nil
	}
	return m
}

// homebrewConfig carries the validated launch configuration to the tool host
// as one JSON argument. The host re-validates it against staged, digest-pinned
// sources; this JSON is never trusted on its own.
func (m *Manager) homebrewConfig(run session.Run, request session.ToolRequest) (string, error) {
	config := map[string]any{
		"profileID":       run.Profile.ID,
		"engineSourceDir": m.options.EngineRoot,
		"homebrewRuntime": m.options.HomebrewRuntime,
		"supportDir":      m.options.Support,
	}
	if m.options.Manifest != nil {
		profile, err := m.options.Manifest.Profile(run.Profile.ID)
		if err != nil {
			return "", err
		}
		config["sourceDirectory"] = profile.SourceDirectory
	}
	if len(request.Modules) > 0 {
		config["modules"] = request.Modules
	}
	if request.SDRomFolder != "" {
		config["sdRomFolder"] = request.SDRomFolder
	}
	if request.AppFolder != "" {
		config["appFolder"] = request.AppFolder
	}
	if request.ModsFolder != "" {
		config["modsFolder"] = request.ModsFolder
	}
	if request.CustomHomebrewFolder != "" {
		config["customHomebrewFolder"] = request.CustomHomebrewFolder
	}
	data, err := json.Marshal(config)
	return string(data), err
}

// NewProtocolManager supplies the tool process explicitly for fixtures. It does
// not authorize upstream tool execution and is never used by the app entrypoint.
func NewProtocolManager(options Options, command CommandFactory) *Manager {
	return &Manager{options: options, command: func(run session.Run, request session.ToolRequest, workspace string) (*exec.Cmd, error) {
		return command(run, request, workspace), nil
	}}
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func (m *Manager) exportPath(run session.Run, tool string) string {
	if tool != "system-code-dump" {
		return ""
	}
	return filepath.Join(m.options.Support, "verified-exports", "system-code-"+run.ID)
}

// State copies the current tool observations.
func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.state
	s.Games = append([]Game(nil), m.state.Games...)
	if m.state.SaveKit != nil {
		observed := *m.state.SaveKit
		observed.Users = append([]string(nil), observed.Users...)
		observed.Titles = append([]string(nil), observed.Titles...)
		observed.Inventory = append([]savekit.Entry(nil), observed.Inventory...)
		if observed.Write != nil {
			outcome := *observed.Write
			observed.Write = &outcome
		}
		s.SaveKit = &observed
	}
	return s
}

// Unavailable states why a tool cannot run, or "" when it can. Distribution
// decisions come from the canonical manifest, never from host capabilities.
func (m *Manager) Unavailable(tool, profileID string) string {
	if m.options.Manifest != nil && slices.Contains(launcher.IDs(m.options.Manifest), tool) {
		if !slices.Contains(m.options.Staged, tool) {
			return m.stagedReason(tool)
		}
		return m.Unavailable("savekit", profileID)
	}
	component, ok := map[string]string{
		"rpc-status": "", "system-code-dump": "",
		"ouncecake-webdav": "ouncecake", "ouncecake-ftp": "ouncecake", "sd-browser": "sd-browser",
		"savekit-read": "savekit", "savekit": "savekit", "modloader": "modloader",
	}[tool]
	if !ok {
		required, homebrew := homebrewComponents[tool]
		if !homebrew {
			return "This read-only tool is not implemented in this build."
		}
		if m.options.Node == "" || m.options.Host == "" {
			return "The bundled tool host is unavailable in this build."
		}
		// The native launcher checks its ABI against the verified engine
		// sources, which only a developer stage provides.
		if tool == "native-hblauncher" && !m.options.EngineStaged {
			return m.reason(profileID)
		}
		for _, id := range required {
			if !slices.Contains(m.options.Staged, id) {
				return m.stagedReason(id)
			}
		}
		return ""
	}
	if m.options.Node == "" || m.options.Host == "" {
		return "The bundled tool host is unavailable in this build."
	}
	if tool == "system-code-dump" {
		if m.options.EngineStaged {
			return ""
		}
		return m.reason(profileID)
	}
	if component != "" {
		if slices.Contains(m.options.Staged, component) {
			return ""
		}
		return m.stagedReason(component)
	}
	return ""
}

func (m *Manager) stagedReason(id string) string {
	reason := m.reason(id)
	if m.options.StageError != nil {
		reason += " A local developer stage was attempted and failed its integrity check."
	}
	return reason
}

func (m *Manager) reason(id string) string {
	if m.options.Manifest == nil {
		return "This component is not recorded in the canonical manifest."
	}
	d, err := m.options.Manifest.DistributionFor(id)
	if err != nil {
		return "This component is not recorded in the canonical manifest."
	}
	return d.Reason
}

// Discover lists verified retained sessions this machine still holds. Listing
// one is read-only: it claims no lease and adopts no stale artifact.
func (m *Manager) Discover(ctx context.Context) ([]session.RetainedSession, error) {
	entries, err := os.ReadDir(filepath.Join(m.options.Support, "runs"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var found []session.RetainedSession
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "run-") {
			continue
		}
		outcome, err := engine.VerifyHeld(ctx, filepath.Join(m.options.Support, "runs", entry.Name()))
		if err != nil || !outcome.Valid(true) {
			continue
		}
		found = append(found, session.RetainedSession{ID: entry.Name(),
			Description: "Verified retained session " + entry.Name()})
	}
	return found, nil
}

// Attach re-verifies the chosen session's private evidence and its live lease.
// It never prepares a host, sends console input or claims the owner's lease.
func (m *Manager) Attach(ctx context.Context, _ session.Run, id string) (session.Outcome, error) {
	if id == "" || filepath.Base(id) != id || !strings.HasPrefix(id, "run-") {
		return session.Outcome{}, errors.New("invalid retained session identity")
	}
	workspace := filepath.Join(m.options.Support, "runs", id)
	outcome, err := engine.VerifyHeld(ctx, workspace)
	if err != nil {
		return session.Outcome{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attached = workspace
	m.state = State{}
	return outcome, nil
}

// Detach releases this app's attachment and its own tool child only. The
// original host keeps running; its owner alone can finish it.
func (m *Manager) Detach(ctx context.Context, run session.Run) error {
	err := m.Stop(ctx, run)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attached = ""
	m.state = State{}
	return err
}

// Watch reports the loss of an attached lease. It never reconnects.
func (m *Manager) Watch(ctx context.Context, _ session.Run) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		workspace := m.attached
		m.mu.Unlock()
		if workspace == "" {
			return errors.New("no attached retained session")
		}
		_, err := engine.VerifyHeld(ctx, workspace)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			m.mu.Lock()
			m.state.Lost = true
			m.mu.Unlock()
			return fmt.Errorf("the attached retained session was lost: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// workspace resolves the lease owner: an attachment, or this app's own run.
func (m *Manager) workspace(run session.Run) (string, error) {
	m.mu.Lock()
	attached := m.attached
	m.mu.Unlock()
	if attached != "" {
		return attached, nil
	}
	if m.options.Runs == nil {
		return "", errors.New("no retained session is owned by this app")
	}
	return m.options.Runs.Workspace(run)
}

var _ session.RetainedAdapter = (*Manager)(nil)
