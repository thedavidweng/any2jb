// Package engine owns immutable runtime staging and private session workspaces.
package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/thedavidweng/any2jb/internal/manifest"
)

type Options struct {
	// Assets is the packaged resource root or source resources directory.
	// Support is the app support path.
	Assets, Support, Version string
	Manifest                 *manifest.Manifest
	// DeveloperSourceRoot and DeveloperProfileID opt into local engine staging.
	// Packaged startup must leave both empty. Stage never reads environment.
	DeveloperSourceRoot, DeveloperProfileID string
}

func resolveRuntimeTarget(r manifest.Runtime) (string, manifest.RuntimeTarget, error) {
	platform := r.Platform
	if len(r.Targets) > 0 {
		platform = runtime.GOOS + "-" + runtime.GOARCH
	}
	target, ok := r.Target(platform)
	if !ok || r.NodeVersion != "24.22.1" {
		return "", manifest.RuntimeTarget{}, errors.New("runtime requires darwin-arm64, linux-amd64, or linux-arm64 Node 24.22.1")
	}
	return platform, target, nil
}

func isRuntimeBinary(r manifest.Runtime, c manifest.Component) bool {
	if r.IsNodeComponent(c.ID) {
		return true
	}
	for _, h := range r.Helpers {
		if c.ID == h {
			return true
		}
	}
	return strings.HasSuffix(c.Path, ".dylib") || strings.HasSuffix(c.Path, ".so")
}

func assetRelPath(root string, c manifest.Component, allowOverlay bool) string {
	candidates := []string{c.Path}
	if c.PackagedGzip != "" {
		candidates = append(candidates, c.PackagedGzip)
	}
	if allowOverlay && c.ResourcePlatform != "" {
		candidates = append(candidates, c.ResourcePlatform+"/"+c.Path)
		if c.PackagedGzip != "" {
			candidates = append(candidates, c.ResourcePlatform+"/"+c.PackagedGzip)
		}
	}
	for _, rel := range candidates {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			return rel
		}
	}
	return c.Path
}

// Stage verifies an existing version or atomically installs one. No child
// process is executed here. Digest identity pins Node's version; Mach-O and ELF
// headers independently check the architecture of Node and every native helper.
func Stage(o Options) (string, error) {
	if o.Version == "" || o.Version == "." || o.Version == ".." ||
		strings.ContainsAny(o.Version, `/\`) {
		return "", errors.New("invalid app version")
	}
	if o.Manifest == nil {
		return "", errors.New("missing runtime manifest")
	}
	profile, record, err := developerSource(o)
	if err != nil {
		return "", err
	}
	r := o.Manifest.Runtime
	platform, targetRuntime, err := resolveRuntimeTarget(r)
	if err != nil {
		return "", err
	}
	ids := []string{targetRuntime.Node, r.Bridge}
	if r.ToolHost != "" {
		ids = append(ids, r.ToolHost)
	}
	var components []manifest.Component
	for _, id := range ids {
		found := false
		for _, c := range o.Manifest.Components {
			if c.ID != id {
				continue
			}
			if !c.Distribution.Available() {
				return "", fmt.Errorf("runtime %s unavailable: %s", id, c.Distribution.Reason)
			}
			if !fs.ValidPath(c.Path) || !strings.HasPrefix(c.Path, "runtime/") {
				return "", fmt.Errorf("invalid runtime path for %s", id)
			}
			components = append(components, c)
			found = true
			break
		}
		if !found {
			return "", fmt.Errorf("missing runtime component %s", id)
		}
	}
	// USB sidecars are optional for guided manual launch. An actually bundled
	// sidecar is still integrity-checked; VerifyUSB gates transport availability.
	for _, c := range o.Manifest.Components {
		if r.IsNodeComponent(c.ID) || c.ID == r.Bridge || c.ID == r.ToolHost ||
			(c.ResourcePlatform != "" && c.ResourcePlatform != platform) ||
			!c.Distribution.Available() || !strings.HasPrefix(c.Path, "runtime/") {
			continue
		}
		components = append(components, c)
	}
	parent := filepath.Join(o.Support, "runtime")
	if profile != nil {
		parent = filepath.Join(parent, "developer", o.Version)
	}
	if err := privateDir(o.Support); err != nil {
		return "", err
	}
	if err := privateDir(parent); err != nil {
		return "", err
	}
	dest := filepath.Join(parent, o.Version)
	if profile != nil {
		dest = filepath.Join(parent, profile.ID)
	}
	var bundledProfiles []manifest.Profile
	if profile == nil {
		for _, p := range o.Manifest.Profiles {
			if p.Distribution.Available() {
				bundledProfiles = append(bundledProfiles, p)
			}
		}
	}
	if _, err := os.Lstat(dest); err == nil {
		err := verify(dest, platform, o.Manifest, components, false)
		if err == nil && profile != nil {
			err = o.Manifest.VerifyProfile(dest, profile.ID)
		}
		for _, p := range bundledProfiles {
			if err != nil {
				break
			}
			err = o.Manifest.VerifyProfile(dest, p.ID)
		}
		if err == nil {
			err = verifySealed(dest, o.Manifest, record)
		}
		if err != nil {
			return "", errors.Join(err, discard(dest))
		}
		return dest, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := verify(o.Assets, platform, o.Manifest, components, true); err != nil {
		return "", err
	}
	for _, p := range bundledProfiles {
		if err := o.Manifest.VerifyProfile(o.Assets, p.ID); err != nil {
			return "", err
		}
	}
	tmp, err := os.MkdirTemp(parent, ".stage-")
	if err != nil {
		return "", err
	}
	defer discard(tmp)
	for _, c := range components {
		rel := assetRelPath(o.Assets, c, true)
		data, err := o.Manifest.ReadAt(o.Assets, c.ID, rel)
		if err != nil {
			return "", err
		}
		target := filepath.Join(tmp, filepath.FromSlash(c.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
		mode := fs.FileMode(0o400)
		if isRuntimeBinary(r, c) {
			mode = 0o500
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return "", err
		}
	}
	if profile != nil {
		if err := copyDeveloperProfile(o.DeveloperSourceRoot, tmp, *profile); err != nil {
			return "", err
		}
		if err := o.Manifest.VerifyProfile(tmp, profile.ID); err != nil {
			return "", err
		}
	}
	for _, p := range bundledProfiles {
		if err := copyDeveloperProfile(o.Assets, tmp, p); err != nil {
			return "", err
		}
		if err := o.Manifest.VerifyProfile(tmp, p.ID); err != nil {
			return "", err
		}
	}
	document, err := json.Marshal(o.Manifest)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmp, "profiles.json"), document, 0o400); err != nil {
		return "", err
	}
	stagedRecord, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmp, "stage.json"), stagedRecord, 0o400); err != nil {
		return "", err
	}
	if err := verify(tmp, platform, o.Manifest, components, false); err != nil {
		return "", err
	}
	if err := filepath.WalkDir(tmp, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.Chmod(p, 0o500)
		}
		return nil
	}); err != nil {
		return "", err
	}
	if err := verifySealed(tmp, o.Manifest, record); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func verifySealed(root string, m *manifest.Manifest, record StagedRecord) error {
	wantRecord, err := json.Marshal(record)
	if err != nil {
		return err
	}
	gotRecord, err := os.ReadFile(filepath.Join(root, "stage.json"))
	if err != nil {
		return err
	}
	if !bytes.Equal(gotRecord, wantRecord) {
		return errors.New("staged engine provenance mismatch")
	}
	want, err := json.Marshal(m)
	if err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Join(root, "profiles.json"))
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("staged manifest is not a regular file")
	}
	got, err := os.ReadFile(filepath.Join(root, "profiles.json"))
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return errors.New("staged manifest differs from canonical manifest")
	}
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0o222 != 0 {
			return fmt.Errorf("writable staged runtime path %s", p)
		}
		if d.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular staged runtime path %s", p)
		}
		return nil
	})
}

func verify(root, platform string, m *manifest.Manifest, components []manifest.Component, allowOverlay bool) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("runtime root is not a real directory")
	}
	allowed := make([]string, 0, len(components))
	for _, c := range components {
		rel := assetRelPath(root, c, allowOverlay)
		allowed = append(allowed, rel)
		data, err := m.ReadAt(root, c.ID, rel)
		if err != nil {
			return err
		}
		if !isRuntimeBinary(m.Runtime, c) {
			continue
		}
		if err := manifest.VerifyBinaryArchitecture(platform, data); err != nil {
			return fmt.Errorf("%s: %w", c.ID, err)
		}
	}
	// Packaged resources may contain the app icon and notices outside runtime/.
	dirs := []string{filepath.Join(root, "runtime")}
	if allowOverlay && platform != "" {
		overlayDir := filepath.Join(root, platform, "runtime")
		if _, err := os.Lstat(overlayDir); err == nil {
			dirs = append(dirs, overlayDir)
		}
	}
	for _, base := range dirs {
		if _, err := os.Lstat(base); os.IsNotExist(err) {
			continue
		}
		if err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			slash := filepath.ToSlash(rel)
			for _, want := range allowed {
				if slash == want {
					return nil
				}
			}
			return fmt.Errorf("unrecorded runtime file %s", rel)
		}); err != nil {
			return err
		}
	}
	return nil
}

// VerifyUSB is the separate USB-transport gate. Missing or unbundled helpers
// never prevent staging the core runtime for guided manual launch.
func VerifyUSB(root string, m *manifest.Manifest) error {
	if len(m.Runtime.Helpers) != 4 {
		return errors.New("USB transport requires four helpers")
	}
	for _, id := range m.Runtime.Helpers {
		d, err := m.DistributionFor(id)
		if err != nil {
			return err
		}
		if !d.Available() {
			return fmt.Errorf("USB helper %s unavailable: %s", id, d.Reason)
		}
		if err := m.Verify(root, id); err != nil {
			return err
		}
	}
	return nil
}

func privateDir(p string) error {
	if err := os.MkdirAll(p, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("not a private directory: %s", p)
	}
	return os.Chmod(p, 0o700)
}

// NewWorkspace always exclusively creates a fresh run; it never loads stale
// pairing or process metadata. The caller owns cleanup after stopping children.
func NewWorkspace(support string) (string, error) {
	if err := privateDir(support); err != nil {
		return "", err
	}
	runs := filepath.Join(support, "runs")
	if err := privateDir(runs); err != nil {
		return "", err
	}
	return os.MkdirTemp(runs, "run-")
}

func discard(root string) error {
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return nil
	}
	if err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.Chmod(p, 0o700)
		}
		return nil
	}); err != nil {
		return err
	}
	return os.RemoveAll(root)
}
