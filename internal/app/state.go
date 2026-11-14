package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/thedavidweng/any2jb/internal/artemis"
	"github.com/thedavidweng/any2jb/internal/devtools"
	"github.com/thedavidweng/any2jb/internal/driver"
	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/plugins"
	"github.com/thedavidweng/any2jb/internal/retained"
	"github.com/thedavidweng/any2jb/internal/savekit"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/transport"
	"github.com/thedavidweng/any2jb/locales"
)

// USBPreferences stores user-selected USB controller and HID timing overrides.
type USBPreferences struct {
	Controller string           `json:"controller"`
	Timing     transport.Timing `json:"timing"`
}

// Preferences stores non-sensitive user choices across restarts.
// Readiness and pairing never survive a restart.
type Preferences struct {
	USB       *USBPreferences `json:"usb,omitempty"`
	Profile   string          `json:"profile"`
	Edition   string          `json:"edition"`
	LANIPv4   string          `json:"lanIPv4,omitempty"`
	Transport string          `json:"transport"`
	Language  string          `json:"language,omitempty"`
}

// ToolAdapter exposes staged retained tools and launcher archive previews.
type ToolAdapter interface {
	UnavailableText(tool, profileID string) locales.Text
	State() retained.State
	Select(titleID string) error
	PreviewLauncher(context.Context, retained.LauncherRequest, savekit.Request) (savekit.Preview, error)
}

// HomebrewInstaller exposes on-demand hbmenu staging when ToolAdapter supports it.
type HomebrewInstaller interface {
	HomebrewInstallable(session.Profile) bool
	InstallHomebrew(context.Context) error
}

// PluginManager exposes on-demand plugin status, installation, uninstallation,
// and resolved runtime paths when ToolAdapter supports them.
type PluginManager interface {
	PluginsStatus() []plugins.Status
	InstallPlugins(ctx context.Context, selectors ...string) ([]string, error)
	UninstallPlugins(ctx context.Context, ids ...string) ([]string, error)
	ToolRoot() string
	NodePath() string
	HomebrewRuntimeRoot() string
	SupportRoot() string
}

// State holds headless runtime configuration, controller state, and adapters
// shared by the native GUI and CLI entrypoints.
type State struct {
	Manifest        *manifest.Manifest
	Preferences     Preferences
	PreferencesPath string
	DeveloperStaged bool
	Controller      *session.SessionController
	Manual          *transport.Manual
	KeyboardInput   transport.USBKeyboard
	Tools           ToolAdapter
	DriverGate      *driver.Gate
	USBConfirmation USBPrompt
	bootstrapMu     sync.Mutex
}

// Load reads manifest and persisted preferences from path (or initializes
// defaults when path does not yet exist).
func Load(path string) (*State, error) {
	s := &State{}
	if err := s.Init(path); err != nil {
		return nil, err
	}
	return s, nil
}

// Init populates s in place from the embedded manifest and persisted preferences
// at path without copying embedded mutexes.
func (s *State) Init(path string) error {
	m, err := manifest.Load()
	if err != nil {
		return err
	}
	s.Manifest = m
	s.PreferencesPath = path
	s.Preferences = Preferences{Profile: m.Default, Edition: "full", Transport: "manual"}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		s.Preferences = Preferences{}
		if err := json.Unmarshal(data, &s.Preferences); err != nil {
			return err
		}
	}
	return s.ValidatePreferences()
}

// ValidatePreferences ensures USB, Profile, Edition, Transport, and LANIPv4 are valid.
func (s *State) ValidatePreferences() error {
	if s.Preferences.USB != nil {
		if s.Preferences.USB.Timing.Validate() != nil || transport.ValidateAnyController(s.Preferences.USB.Controller) != nil {
			return errors.New(locales.UsbInvalidSettings())
		}
	}
	p, err := s.Manifest.Profile(s.Preferences.Profile)
	if err != nil {
		return errors.New(locales.ChooseKnownHost())
	}
	if !slices.Contains(p.SessionProfile().Editions, s.Preferences.Edition) {
		return errors.New(locales.UnsupportedEdition())
	}
	if !slices.Contains([]string{"manual", "keyboard", "composite", "external-pico"}, s.Preferences.Transport) {
		return errors.New(locales.ChooseTransport())
	}
	if s.Preferences.LANIPv4 != "" {
		ip, err := netip.ParseAddr(s.Preferences.LANIPv4)
		if err != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.String() == "255.255.255.255" {
			return errors.New(locales.ChooseLANIPv4())
		}
	}
	normalizedLang, ok := locales.NormalizeLanguagePreference(s.Preferences.Language)
	if !ok {
		return errors.New(locales.ChooseLanguage())
	}
	s.Preferences.Language = normalizedLang
	return nil
}

// Plugins returns the PluginManager when Tools supports on-demand plugins.
func (s *State) Plugins() PluginManager {
	pm, _ := s.Tools.(PluginManager)
	return pm
}

// DevToolsConfig resolves the runtime paths for standalone devtools plugins.
func (s *State) DevToolsConfig() devtools.Config {
	cfg := devtools.Config{Manifest: s.Manifest}
	if pm := s.Plugins(); pm != nil {
		cfg.ToolRoot = pm.ToolRoot()
		cfg.Support = pm.SupportRoot()
		cfg.Node = pm.NodePath()
		if rt := pm.HomebrewRuntimeRoot(); rt != "" {
			cfg.HomebrewPython = filepath.Join(rt, "python", "bin", "python3")
		}
	}
	return cfg
}

// ArtemisClient builds an Artemis HTTP client using the staged SaveKit plugin.
func (s *State) ArtemisClient(port int) artemis.Client {
	client := artemis.Client{Manifest: s.Manifest, Port: port}
	if pm := s.Plugins(); pm != nil {
		client.Node = artemis.NodeBinary(pm.NodePath())
		client.ToolRoot = pm.ToolRoot()
		client.Support = pm.SupportRoot()
	}
	return client
}

// ApplyPreferences validates, pushes Preferences into Controller when idle/safe,
// and saves Preferences atomically.
func (s *State) ApplyPreferences() error {
	if err := s.ValidatePreferences(); err != nil {
		return err
	}
	if s.Controller != nil {
		snap := s.Controller.Snapshot()
		if snap.Busy || snap.SideEffects || !slices.Contains([]session.Stage{session.Idle, session.Ready, session.NeedsSetup, session.Finished, session.Failed}, snap.Stage) {
			return errors.New(locales.CommandRejected())
		}
		p, _ := s.Manifest.Profile(s.Preferences.Profile)
		if snap.Profile.ID != p.ID || snap.Edition != s.Preferences.Edition {
			if _, err := s.Controller.SelectProfile(p.SessionProfile(), s.Preferences.Edition); err != nil {
				return err
			}
		}
		if _, err := s.Controller.ConfigureTransport(s.PreferredTransport(), s.Preferences.LANIPv4); err != nil {
			return err
		}
	}
	return s.SavePreferences()
}

// SavePreferences writes Preferences atomically to PreferencesPath.
func (s *State) SavePreferences() error {
	if err := s.ValidatePreferences(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.Preferences, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.PreferencesPath), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.PreferencesPath), ".preferences-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.PreferencesPath)
}

// InitializeSession is called once real adapters are available, never with a
// production fake. Preferences restore choices, not observed readiness.
func (s *State) InitializeSession(env session.Environment) (*session.Rejection, error) {
	if err := s.ValidatePreferences(); err != nil {
		return nil, err
	}
	p, _ := s.Manifest.Profile(s.Preferences.Profile)
	c := session.New(env, p.SessionProfile())
	if _, err := c.SelectProfile(p.SessionProfile(), s.Preferences.Edition); err != nil {
		return nil, err
	}
	if _, err := c.ConfigureTransport(s.PreferredTransport(), s.Preferences.LANIPv4); err != nil {
		return nil, err
	}
	s.Controller = c
	reject, err := s.RestoreBootstrap()
	if err != nil {
		return nil, err
	}
	if inputs, ok := env.Input.(*transport.Dispatcher); ok {
		s.Manual = inputs.Manual()
		s.KeyboardInput = inputs.USB()
	} else {
		s.Manual, _ = env.Input.(*transport.Manual)
		s.KeyboardInput = nil
	}
	s.Tools, _ = env.Tools.(ToolAdapter)
	return reject, nil
}

// PreferredTransport returns the session.Transport for the saved Transport mode.
func (s *State) PreferredTransport() session.Transport {
	return s.TransportFor(s.Preferences.Transport)
}

// TransportFor returns the session.Transport for mode, including any saved USB
// controller for the plain USB keyboard.
func (s *State) TransportFor(mode string) session.Transport {
	t := PreferredTransport(mode)
	if s.Preferences.USB != nil && t.Capabilities.Keyboard && !t.Capabilities.Gamepad && !t.Capabilities.ExternalPico {
		t.Controller = s.Preferences.USB.Controller
	}
	return t
}

// PreferredTransport returns the baseline session.Transport descriptor for mode.
func PreferredTransport(mode string) session.Transport {
	c := session.Capabilities{}
	switch mode {
	case "manual":
		c.Manual = true
	case "keyboard":
		c.Keyboard = true
	case "composite":
		c.Keyboard, c.Gamepad = true, true
	case "external-pico":
		c.Keyboard, c.ExternalPico = true, true
	}
	return session.Transport{Mode: mode, Capabilities: c}
}

// BootstrapPath returns the path to bootstrap.json alongside PreferencesPath.
func (s *State) BootstrapPath() string {
	return filepath.Join(filepath.Dir(s.PreferencesPath), "bootstrap.json")
}

// RestoreBootstrap loads bootstrap.json into Controller if present.
// A valid but no-longer-matching record returns its *session.Rejection without
// failing startup so a new bootstrap can still be prepared.
func (s *State) RestoreBootstrap() (*session.Rejection, error) {
	data, err := os.ReadFile(s.BootstrapPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record session.BootstrapRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	if record.SavedAt.IsZero() {
		return nil, errors.New("saved bootstrap identity has no confirmation timestamp")
	}
	_, err = s.Controller.UseSavedBootstrap(record)
	var reject *session.Rejection
	if errors.As(err, &reject) {
		return reject, nil
	}
	return nil, err
}

// ConfirmExistingBootstrap records explicit confirmation for the active profile's
// bootstrap program and persists it to bootstrap.json.
func (s *State) ConfirmExistingBootstrap() error {
	snap := s.Controller.Snapshot()
	record := session.BootstrapRecord{
		ProfileID: snap.Profile.ID, Revision: snap.Profile.Revision,
		SourceSHA256: snap.Profile.SourceSHA256, BootstrapSHA256: snap.Profile.BootstrapSHA256,
		SaveName: snap.Profile.SaveName, Edition: snap.Edition, Confirmed: true, SavedAt: time.Now().UTC(),
	}
	if _, err := s.Controller.UseSavedBootstrap(record); err != nil {
		return err
	}
	return s.PersistBootstrap(s.BootstrapPath())
}

// PersistBootstrap writes path when Controller holds a confirmed bootstrap identity.
func (s *State) PersistBootstrap(path string) error {
	s.bootstrapMu.Lock()
	defer s.bootstrapMu.Unlock()
	snap := s.Controller.Snapshot()
	if !snap.BootstrapSaved {
		return nil
	}
	return SaveBootstrapRecord(path, snap.SavedBootstrap)
}

// SaveBootstrapRecord atomically writes record to path.
func SaveBootstrapRecord(path string, record session.BootstrapRecord) error {
	if !record.Confirmed || record.SavedAt.IsZero() {
		return errors.New("saved bootstrap requires explicit confirmation")
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".bootstrap-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// USBPrompt coordinates synchronous console-step confirmations between a USB
// worker goroutine and the active GUI or CLI observer.
type USBPrompt struct {
	mu               sync.Mutex
	step             transport.ConsoleStep
	sequence         uint64
	pending          chan struct{}
	notify           func()
	profile, edition string
}

// SetNotify sets the callback invoked whenever prompt state changes.
func (p *USBPrompt) SetNotify(fn func()) {
	p.mu.Lock()
	p.notify = fn
	p.mu.Unlock()
}

// Wait blocks until Confirm acknowledges step or ctx is cancelled.
func (p *USBPrompt) Wait(ctx context.Context, run session.Run, step transport.ConsoleStep) error {
	if ConsolePrompt(step) == "" {
		return errors.New("unsupported USB console confirmation")
	}
	p.mu.Lock()
	if p.pending != nil {
		p.mu.Unlock()
		return errors.New("USB confirmation already pending")
	}
	done := make(chan struct{})
	p.pending = done
	p.step = step
	p.profile, p.edition = run.Profile.ID, run.Edition
	p.sequence++
	notify := p.notify
	p.mu.Unlock()
	if notify != nil {
		notify()
	}
	defer func() {
		p.mu.Lock()
		if p.pending == done {
			p.pending = nil
		}
		notify := p.notify
		p.mu.Unlock()
		if notify != nil {
			notify()
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

// Current reports the pending console step, sequence, and whether one is active.
func (p *USBPrompt) Current() (transport.ConsoleStep, uint64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.step, p.sequence, p.pending != nil
}

// CurrentWithIdentity reports the pending step, profile, edition, sequence, and
// active state, while registering notify for subsequent state transitions.
func (p *USBPrompt) CurrentWithIdentity(notify func()) (step transport.ConsoleStep, profile, edition string, id uint64, pending bool) {
	p.mu.Lock()
	if notify != nil {
		p.notify = notify
	}
	step, profile, edition, id, pending = p.step, p.profile, p.edition, p.sequence, p.pending != nil
	p.mu.Unlock()
	return
}

// Confirm releases only the step matching id, never a later one.
func (p *USBPrompt) Confirm(id uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending == nil || p.sequence != id {
		return false
	}
	close(p.pending)
	p.pending = nil
	return true
}

// RefreshSession schedules fresh reads whenever controller or manual emits an
// event until closed is closed.
func RefreshSession(closed <-chan struct{}, controller, manual <-chan struct{}, update func()) {
	for {
		select {
		case <-closed:
			return
		case <-controller:
		case <-manual:
		}
		select {
		case <-closed:
			return
		default:
			update()
		}
	}
}
