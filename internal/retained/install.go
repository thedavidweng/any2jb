package retained

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/plugins"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

// OptionalHomebrewComponents lists the optional homebrew components downloaded
// and verified on demand rather than bundled into the base package.
var OptionalHomebrewComponents = []string{
	"mintbrew", "mintbrew-native", "homebrew-runtime",
}

const (
	HomebrewRuntimeDarwinOnly = "The portable GPU homebrew runtime requires Apple Silicon macOS; Native HBLauncher is ready."
	HomebrewRuntimeNeedsTools = "Portable GPU homebrew runtime preparation requires python3, cmake, and Xcode Command Line Tools on Apple Silicon macOS; Native HBLauncher is ready."
)

// VerifyInstalledPlugins re-verifies all previously installed optional plugins
// in toolRoot without touching the network.
func VerifyInstalledPlugins(toolRoot string, m *manifest.Manifest) []string {
	if toolRoot == "" || m == nil {
		return nil
	}
	staged := VerifyStagedComponents(toolRoot, m, m.OptionalToolIDs())
	kept := staged[:0]
	for _, id := range staged {
		if id == "homebrew-runtime" {
			if CheckHomebrewRuntime(toolRoot) == "" {
				continue
			}
		}
		if err := plugins.VerifyUpstreamArtifact(toolRoot, "", id); err != nil {
			continue
		}
		kept = append(kept, id)
	}
	return kept
}

// PluginsStatus returns the current support and installation state of all
// plugins in the canonical catalog.
func (m *Manager) PluginsStatus() []plugins.Status {
	o, _ := m.snapshotOptions()
	specs := plugins.All()
	out := make([]plugins.Status, 0, len(specs))
	for _, spec := range specs {
		rev := ""
		if o.Manifest != nil {
			if c, err := o.Manifest.ToolComponent(spec.ID); err == nil {
				rev = c.Revision
			}
		}
		out = append(out, plugins.Status{
			Spec:      spec,
			Revision:  rev,
			Supported: spec.SupportedOnHost(),
			Installed: slices.Contains(o.Staged, spec.ID),
		})
	}
	return out
}

// HomebrewInstallable reports whether the selected profile offers homebrew
// launchers whose permitted optional components can be downloaded or prepared.
func (m *Manager) HomebrewInstallable(profile session.Profile) bool {
	o, _ := m.snapshotOptions()
	if o.Manifest == nil || o.Support == "" || o.Version == "" || o.Node == "" || o.Host == "" {
		return false
	}
	if !slices.ContainsFunc(profile.Tools, isHomebrew) {
		return false
	}
	for _, id := range OptionalHomebrewComponents {
		if id == "homebrew-runtime" && (runtime.GOOS != "darwin" || runtime.GOARCH != "arm64") {
			continue
		}
		c, err := o.Manifest.ToolComponent(id)
		if err != nil || !c.Distribution.Permitted() {
			continue
		}
		if !slices.Contains(o.Staged, id) {
			return true
		}
	}
	return false
}

// InstallPlugins expands selectors ("all", a category name, or plugin IDs),
// downloads and verifies every required component and upstream artifact, and
// stages them into the private support directory.
func (m *Manager) InstallPlugins(ctx context.Context, selectors ...string) ([]string, error) {
	if len(selectors) == 0 {
		return nil, errors.New("choose at least one plugin to install")
	}
	ids, err := plugins.ExpandTargets(runtime.GOOS+"-"+runtime.GOARCH, selectors...)
	if err != nil {
		return nil, err
	}
	return m.installComponentIDs(ctx, ids, false)
}

// InstallHomebrew downloads and verifies the optional homebrew launcher plugins
// (plus ouncecake when present in the manifest for SD ROM listing).
func (m *Manager) InstallHomebrew(ctx context.Context) error {
	o, _ := m.snapshotOptions()
	ids := append([]string(nil), OptionalHomebrewComponents...)
	if o.Manifest != nil {
		if _, err := o.Manifest.ToolComponent("ouncecake"); err == nil && !slices.Contains(o.Staged, "ouncecake") {
			ids = append([]string{"ouncecake"}, ids...)
		}
	}
	_, err := m.installComponentIDs(ctx, ids, true)
	return err
}

func (m *Manager) installComponentIDs(ctx context.Context, ids []string, softRuntimeNote bool) ([]string, error) {
	m.mu.Lock()
	if m.installing {
		m.mu.Unlock()
		return nil, locales.Key("dynHbInstallRunning")
	}
	if m.active != nil || m.savekitOwner {
		m.mu.Unlock()
		return nil, locales.Key("dynHbStopActiveTool")
	}
	o := m.options
	o.Staged = append([]string(nil), m.options.Staged...)
	m.installing = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.installing = false
		m.mu.Unlock()
	}()

	if o.Manifest == nil || o.Support == "" || o.Version == "" || o.Node == "" || o.Host == "" {
		return nil, locales.Key("dynToolHostUnavailable")
	}
	ref, err := o.Manifest.ReferenceInfo()
	if err != nil {
		return nil, err
	}
	var allowedComponents []manifest.Component
	for _, id := range ids {
		c, err := o.Manifest.ToolComponent(id)
		if err != nil {
			return nil, err
		}
		if !c.Distribution.Permitted() {
			return nil, locales.DistributionReason(id, c.Distribution.Reason)
		}
		allowedComponents = append(allowedComponents, c)
	}

	cacheDir := filepath.Join(o.Support, "cache")
	archivePath, err := engine.FetchReferenceArchive(ctx, cacheDir, o.Manifest)
	if err != nil {
		return nil, err
	}
	extractDir, err := os.MkdirTemp(cacheDir, ".plugin-src-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(extractDir)

	allow := func(rel string) bool {
		for _, c := range allowedComponents {
			if rel == c.Path {
				return true
			}
			if c.Source.Directory != "" && (rel == c.Source.Directory || strings.HasPrefix(rel, c.Source.Directory+"/")) {
				return true
			}
			if _, ok := c.Source.Embedded[rel]; ok {
				return true
			}
			if _, ok := c.Source.Extra[rel]; ok {
				return true
			}
		}
		return false
	}
	if err := engine.ExtractReferenceFiles(archivePath, ref.Revision, extractDir, allow); err != nil {
		return nil, err
	}

	stageOptions := StageOptions{
		Support:    o.Support,
		Version:    o.Version,
		SourceRoot: extractDir,
		Bundled:    o.Bundled,
		Manifest:   o.Manifest,
		Components: ids,
	}
	toolRoot, staged, stageErr := StageTools(stageOptions)
	if stageErr != nil {
		return nil, stageErr
	}

	platform := runtime.GOOS + "-" + runtime.GOARCH
	var upstreamErr error
	kept := make([]string, 0, len(staged))
	for _, id := range staged {
		if id == "homebrew-runtime" {
			kept = append(kept, id)
			continue
		}
		if err := plugins.StageUpstream(ctx, cacheDir, toolRoot, platform, id, o.PluginURLRewriter); err != nil {
			upstreamErr = errors.Join(upstreamErr, err)
			_ = os.RemoveAll(filepath.Join(toolRoot, "tools", id))
			_ = plugins.RemoveUpstreamArtifact(toolRoot, id)
			continue
		}
		kept = append(kept, id)
	}
	staged = kept

	runtimeRoot := o.HomebrewRuntime
	var runtimeNote locales.Text
	if slices.Contains(staged, "homebrew-runtime") {
		if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
			staged = slices.DeleteFunc(staged, func(id string) bool { return id == "homebrew-runtime" })
			runtimeNote = locales.Key("dynHbRuntimeDarwinOnly")
		} else {
			runtimeRoot, err = StageHomebrewRuntimeContext(ctx, stageOptions, toolRoot, staged)
			if err != nil {
				staged = slices.DeleteFunc(staged, func(id string) bool { return id == "homebrew-runtime" })
				if softRuntimeNote {
					runtimeNote = locales.Key("dynHbRuntimeNeedsTools")
				} else {
					upstreamErr = errors.Join(upstreamErr, err)
				}
			}
		}
	}

	m.mu.Lock()
	merged := append([]string(nil), m.options.Staged...)
	for _, id := range staged {
		if !slices.Contains(merged, id) {
			merged = append(merged, id)
		}
	}
	if slices.Contains(ids, "homebrew-runtime") && runtimeRoot == "" {
		merged = slices.DeleteFunc(merged, func(id string) bool { return id == "homebrew-runtime" })
	}
	m.options.ToolRoot = toolRoot
	m.options.Staged = merged
	m.options.HomebrewRuntime = runtimeRoot
	if !runtimeNote.IsZero() {
		m.homebrewRuntimeNote = runtimeNote
	} else if runtimeRoot != "" {
		m.homebrewRuntimeNote = locales.Text{}
	}
	m.mu.Unlock()

	if upstreamErr != nil {
		return staged, upstreamErr
	}
	if !runtimeNote.IsZero() {
		return staged, runtimeNote
	}
	return staged, nil
}

// UninstallPlugins removes the staged files and upstream artifacts for the
// selected plugins ("all", a category name, or plugin IDs).
func (m *Manager) UninstallPlugins(_ context.Context, selectors ...string) ([]string, error) {
	if len(selectors) == 0 {
		return nil, errors.New("choose at least one plugin to remove")
	}
	var targetIDs []string
	for _, sel := range selectors {
		switch sel {
		case "all":
			for _, s := range plugins.All() {
				targetIDs = append(targetIDs, s.ID)
			}
		case string(plugins.CategorySession), string(plugins.CategoryHomebrew), string(plugins.CategoryCivSwitch), string(plugins.CategoryArtemis), string(plugins.CategoryUtilities):
			for _, s := range plugins.ByCategory(plugins.Category(sel)) {
				targetIDs = append(targetIDs, s.ID)
			}
		default:
			spec, ok := plugins.Lookup(sel)
			if !ok {
				return nil, &plugins.UnknownPluginError{ID: sel}
			}
			targetIDs = append(targetIDs, spec.ID)
		}
	}

	m.mu.Lock()
	if m.installing {
		m.mu.Unlock()
		return nil, locales.Key("dynHbInstallRunning")
	}
	if m.active != nil || m.savekitOwner {
		m.mu.Unlock()
		return nil, locales.Key("dynHbStopActiveTool")
	}
	toolRoot := m.options.ToolRoot
	if toolRoot == "" && m.options.Support != "" && m.options.Version != "" {
		toolRoot = filepath.Join(m.options.Support, "tools", m.options.Version)
	}
	m.installing = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.installing = false
		m.mu.Unlock()
	}()

	var removed []string
	var removeErr error
	for _, id := range targetIDs {
		if filepath.Base(id) != id {
			continue
		}
		if toolRoot != "" {
			if err := os.RemoveAll(filepath.Join(toolRoot, "tools", id)); err != nil {
				removeErr = errors.Join(removeErr, err)
				continue
			}
			if err := plugins.RemoveUpstreamArtifact(toolRoot, id); err != nil {
				removeErr = errors.Join(removeErr, err)
				continue
			}
			if id == "homebrew-runtime" {
				_ = os.RemoveAll(filepath.Join(filepath.Dir(toolRoot), "homebrew-runtime", "homebrew"))
			}
		}
		removed = append(removed, id)
	}

	m.mu.Lock()
	m.options.Staged = slices.DeleteFunc(m.options.Staged, func(id string) bool {
		return slices.Contains(removed, id)
	})
	if slices.Contains(removed, "homebrew-runtime") {
		m.options.HomebrewRuntime = ""
	}
	m.mu.Unlock()
	return removed, removeErr
}
