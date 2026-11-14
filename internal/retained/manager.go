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

	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/plugins"
	"github.com/thedavidweng/any2jb/internal/savekit"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

// Workspaces resolves the private workspace of a run this app owns.
type Workspaces interface {
	Workspace(session.Run) (string, error)
}

type Options struct {
	Support string
	Version string
	Bundled bool
	// Node and Host are the staged Node binary and the original any2jb tool
	// host. Empty values leave every tool unavailable.
	Node, Host string
	// ToolRoot is the verified private stage of the pinned tool sources, and
	// Staged lists the component IDs verified in it.
	ToolRoot string
	Staged   []string
	// EngineStaged records a verified engine stage, which the system-code
	// capture module and native launcher ABI check use.
	EngineStaged bool
	// StageError records a failed local tool stage so an unavailable tool
	// can say that integrity checking failed instead of staying silent.
	StageError error
	// EngineRoot is the verified engine stage the native launcher checks its
	// ABI against, and HomebrewRuntime the prepared portable runtime root.
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
	// PluginURLRewriter optionally redirects upstream plugin downloads in tests.
	PluginURLRewriter plugins.URLRewriter
}

// homebrewComponents lists the staged components each homebrew launcher needs
// before it may run. Every component is verified against the canonical
// manifest before staging; distribution policy still gates the tool itself.
var homebrewComponents = map[string][]string{
	"mintbrew":          {"mintbrew", "homebrew-runtime"},
	"native-hblauncher": {"mintbrew", "mintbrew-native"},
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
	options             Options
	command             func(session.Run, session.ToolRequest, string) (*exec.Cmd, error)
	mu                  sync.Mutex
	installing          bool
	homebrewRuntimeNote locales.Text
	attached            string
	active              *toolProcess
	savekitOwner        bool
	savekitCancel       context.CancelFunc
	state               State
}

func New(options Options) *Manager {
	m := &Manager{options: options}
	// RunWith calls m.command while holding m.mu, so read m.options fields directly.
	m.command = func(run session.Run, request session.ToolRequest, workspace string) (*exec.Cmd, error) {
		args := []string{m.options.Host, m.options.ToolRoot, request.Tool, workspace,
			dash(request.ConsoleIPv4), dash(m.exportPath(run, request.Tool))}
		if isHomebrew(request.Tool) || request.Tool == "modloader" {
			config, err := m.homebrewConfig(run, request)
			if err != nil {
				return nil, err
			}
			args = append(args, config)
		}
		return exec.Command(m.options.Node, args...), nil
	}
	return m
}

func (m *Manager) snapshotOptions() (Options, locales.Text) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o := m.options
	o.Staged = append([]string(nil), m.options.Staged...)
	return o, m.homebrewRuntimeNote
}

// ToolRoot returns the active verified tool stage root, if initialized.
func (m *Manager) ToolRoot() string {
	o, _ := m.snapshotOptions()
	if o.ToolRoot != "" {
		return o.ToolRoot
	}
	if o.Support != "" && o.Version != "" {
		return filepath.Join(o.Support, "tools", o.Version)
	}
	return ""
}

// HomebrewRuntimeRoot returns the verified portable Python/shader runtime path, if staged.
func (m *Manager) HomebrewRuntimeRoot() string {
	o, _ := m.snapshotOptions()
	return o.HomebrewRuntime
}

// NodePath returns the verified staged Node runtime executable path.
func (m *Manager) NodePath() string {
	o, _ := m.snapshotOptions()
	return o.Node
}

// SupportRoot returns the private support directory path.
func (m *Manager) SupportRoot() string {
	o, _ := m.snapshotOptions()
	return o.Support
}

// Manifest returns the canonical manifest.
func (m *Manager) Manifest() *manifest.Manifest {
	o, _ := m.snapshotOptions()
	return o.Manifest
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
	if request.StremioQuality != "" {
		config["stremioQuality"] = request.StremioQuality
	}
	if len(request.StremioAddonURLs) > 0 {
		config["stremioAddonUrls"] = request.StremioAddonURLs
	}
	config["codeConsent"] = request.CodeConsent
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
	return m.UnavailableText(tool, profileID).English()
}

// UnavailableText returns the catalog-backed refusal for tool, or zero when it can run.
func (m *Manager) UnavailableText(tool, profileID string) locales.Text {
	o, runtimeNote := m.snapshotOptions()
	if o.Manifest != nil && slices.Contains(LauncherIDs(o.Manifest), tool) {
		if !slices.Contains(o.Staged, tool) {
			return stagedReason(o, runtimeNote, tool)
		}
		return m.UnavailableText("savekit", profileID)
	}
	component, ok := map[string]string{
		"rpc-status": "", "system-code-dump": "",
		"ouncecake-webdav": "ouncecake", "ouncecake-ftp": "ouncecake",
		"savekit-read": "savekit", "savekit": "savekit", "modloader": "modloader",
	}[tool]
	if !ok {
		required, homebrew := homebrewComponents[tool]
		if !homebrew {
			return locales.Key("dynToolNotImplemented")
		}
		if o.Node == "" || o.Host == "" {
			return locales.Key("dynToolHostUnavailable")
		}
		// The native launcher checks its ABI against the verified engine sources.
		if tool == "native-hblauncher" && !o.EngineStaged {
			return manifestReason(o.Manifest, profileID)
		}
		for _, id := range required {
			if !slices.Contains(o.Staged, id) {
				return stagedReason(o, runtimeNote, id)
			}
		}
		return locales.Text{}
	}
	if o.Node == "" || o.Host == "" {
		return locales.Key("dynToolHostUnavailable")
	}
	if tool == "system-code-dump" {
		if o.EngineStaged {
			return locales.Text{}
		}
		return manifestReason(o.Manifest, profileID)
	}
	if component != "" {
		if slices.Contains(o.Staged, component) {
			return locales.Text{}
		}
		return stagedReason(o, runtimeNote, component)
	}
	return locales.Text{}
}

func stagedReason(o Options, runtimeNote locales.Text, id string) locales.Text {
	if id == "homebrew-runtime" && !runtimeNote.IsZero() {
		return runtimeNote
	}
	reason := manifestReason(o.Manifest, id)
	if o.StageError != nil {
		return locales.Join(reason, locales.Key("dynToolStageFailed"))
	}
	return reason
}

func manifestReason(m *manifest.Manifest, id string) locales.Text {
	if m == nil {
		return locales.Key("dynToolNotInManifest")
	}
	d, err := m.DistributionFor(id)
	if err != nil {
		return locales.Key("dynToolNotInManifest")
	}
	return locales.DistributionReason(id, d.Reason)
}

// Discover lists verified retained sessions this machine still holds across
// standard NS2 runs, CivSwitch FS hosts, and Artemis Seedsow FS hosts. Listing
// one is read-only: it claims no lease and adopts no stale artifact.
func (m *Manager) Discover(ctx context.Context) ([]session.RetainedSession, error) {
	runsRoot := filepath.Join(m.options.Support, "runs")
	entries, err := os.ReadDir(runsRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var found []session.RetainedSession
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch {
		case strings.HasPrefix(name, "run-"):
			outcome, err := engine.VerifyHeld(ctx, filepath.Join(runsRoot, name))
			if err != nil || !outcome.Valid(true) {
				continue
			}
			target := outcome.Target
			if target == "" {
				target = "switch2"
			}
			found = append(found, session.RetainedSession{
				ID:          name,
				Target:      target,
				Description: "Verified retained session " + name + " (" + target + ")",
			})
		case strings.HasPrefix(name, "civswitch-fs-"), strings.HasPrefix(name, "artemis-fs-"):
			nestedRuns, err := resolveNestedRunsDir(runsRoot, name)
			if err != nil {
				continue
			}
			subEntries, err := os.ReadDir(nestedRuns)
			if err != nil {
				continue
			}
			for _, sub := range subEntries {
				if !sub.IsDir() || !strings.HasPrefix(sub.Name(), "switch2-") {
					continue
				}
				outcome, err := engine.VerifyHeld(ctx, filepath.Join(nestedRuns, sub.Name()))
				if err != nil || !outcome.Valid(true) {
					continue
				}
				id := name + "/" + sub.Name()
				found = append(found, session.RetainedSession{
					ID:          id,
					Target:      outcome.Target,
					Description: "Verified retained session " + id + " (" + outcome.Target + ")",
				})
			}
		}
	}
	return found, nil
}

// Attach re-verifies the chosen session's private evidence and its live lease.
// It never prepares a host, sends console input or claims the owner's lease.
func (m *Manager) Attach(ctx context.Context, _ session.Run, id string) (session.Outcome, error) {
	workspace, err := resolveAttachWorkspace(filepath.Join(m.options.Support, "runs"), id)
	if err != nil {
		return session.Outcome{}, err
	}
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

func resolveNestedRunsDir(runsRoot, hostDir string) (string, error) {
	if filepath.Base(hostDir) != hostDir || !(strings.HasPrefix(hostDir, "civswitch-fs-") || strings.HasPrefix(hostDir, "artemis-fs-")) {
		return "", errors.New("invalid multi-target host directory")
	}
	cur := runsRoot
	for _, seg := range []string{hostDir, "port", "engine", "runs"} {
		cur = filepath.Join(cur, seg)
		info, err := os.Lstat(cur)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("invalid multi-target run path")
		}
	}
	return cur, nil
}

func resolveAttachWorkspace(runsRoot, id string) (string, error) {
	if id == "" {
		return "", errors.New("invalid retained session identity")
	}
	if !strings.Contains(id, "/") {
		if filepath.Base(id) != id || !strings.HasPrefix(id, "run-") {
			return "", errors.New("invalid retained session identity")
		}
		return filepath.Join(runsRoot, id), nil
	}
	hostDir, subSession, ok := strings.Cut(id, "/")
	if !ok || strings.Contains(subSession, "/") || filepath.Base(subSession) != subSession || !strings.HasPrefix(subSession, "switch2-") {
		return "", errors.New("invalid retained session identity")
	}
	nestedRuns, err := resolveNestedRunsDir(runsRoot, hostDir)
	if err != nil {
		return "", errors.New("invalid retained session identity")
	}
	return filepath.Join(nestedRuns, subSession), nil
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
