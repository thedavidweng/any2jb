package retained

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

// OptionalHomebrewComponents lists the large optional homebrew components
// downloaded and verified on demand rather than bundled into the base package.
var OptionalHomebrewComponents = []string{
	"mintbrew", "mintbrew-native",
	"izlauncher", "izlauncher-blitbun", "izlauncher-shell", "izlauncher-vacootube",
	"homebrew-runtime",
}

const (
	HomebrewRuntimeDarwinOnly = "The portable GPU homebrew runtime requires Apple Silicon macOS; Native HBLauncher is ready."
	HomebrewRuntimeNeedsTools = "Portable GPU homebrew runtime preparation requires python3, cmake, and Xcode Command Line Tools on Apple Silicon macOS; Native HBLauncher is ready."
)

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

// InstallHomebrew downloads the pinned upstream archive when needed, verifies
// every optional homebrew component by SHA-256 into the private support stage,
// prepares IZlauncher, and on Apple Silicon macOS prepares the portable Python
// and shader runtime.
func (m *Manager) InstallHomebrew(ctx context.Context) error {
	m.mu.Lock()
	if m.installing {
		m.mu.Unlock()
		return locales.Key("dynHbInstallRunning")
	}
	if m.active != nil || m.savekitOwner {
		m.mu.Unlock()
		return locales.Key("dynHbStopActiveTool")
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
		return locales.Key("dynToolHostUnavailable")
	}
	ref, err := o.Manifest.ReferenceInfo()
	if err != nil {
		return err
	}
	var allowedComponents []manifest.Component
	for _, id := range OptionalHomebrewComponents {
		c, err := o.Manifest.ToolComponent(id)
		if err != nil {
			return err
		}
		if !c.Distribution.Permitted() {
			return locales.DistributionReason(id, c.Distribution.Reason)
		}
		allowedComponents = append(allowedComponents, c)
	}

	cacheDir := filepath.Join(o.Support, "cache")
	archivePath, err := engine.FetchReferenceArchive(ctx, cacheDir, o.Manifest)
	if err != nil {
		return err
	}
	extractDir, err := os.MkdirTemp(cacheDir, ".homebrew-src-")
	if err != nil {
		return err
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
		return err
	}

	stageOptions := StageOptions{
		Support:    o.Support,
		Version:    o.Version,
		SourceRoot: extractDir,
		Bundled:    o.Bundled,
		Manifest:   o.Manifest,
		Components: OptionalHomebrewComponents,
	}
	toolRoot, staged, stageErr := StageTools(stageOptions)
	if stageErr != nil {
		return stageErr
	}
	if slices.Contains(staged, "izlauncher") {
		if err := StageIZLauncher(o.Node, filepath.Join(toolRoot, "tools", "izlauncher")); err != nil {
			return err
		}
	}

	runtimeRoot := ""
	var runtimeNote locales.Text
	if slices.Contains(staged, "homebrew-runtime") {
		if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
			staged = slices.DeleteFunc(staged, func(id string) bool { return id == "homebrew-runtime" })
			runtimeNote = locales.Key("dynHbRuntimeDarwinOnly")
		} else {
			runtimeRoot, err = StageHomebrewRuntimeContext(ctx, stageOptions, toolRoot, staged)
			if err != nil {
				staged = slices.DeleteFunc(staged, func(id string) bool { return id == "homebrew-runtime" })
				runtimeNote = locales.Key("dynHbRuntimeNeedsTools")
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
	if runtimeRoot == "" {
		merged = slices.DeleteFunc(merged, func(id string) bool { return id == "homebrew-runtime" })
	}
	m.options.ToolRoot = toolRoot
	m.options.Staged = merged
	m.options.HomebrewRuntime = runtimeRoot
	m.homebrewRuntimeNote = runtimeNote
	m.mu.Unlock()

	if !runtimeNote.IsZero() {
		return runtimeNote
	}
	return nil
}
