// Package engine owns immutable runtime staging and private session workspaces.
package engine

import (
	"bytes"
	"debug/macho"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/thedavidweng/any2jb/manifest"
)

type Options struct {
	// Assets is the packaged resource root, injected because go test/run do not
	// merge MyGo's platform resource overlays. Support is the app support path.
	Assets, Support, Version string
	Manifest                 *manifest.Manifest
}

// Stage verifies an existing version or atomically installs one. No child
// process is executed here. Digest identity pins Node's version; Mach-O headers
// independently check the architecture of Node and every native helper.
func Stage(o Options) (string, error) {
	if o.Version == "" || o.Version == "." || o.Version == ".." ||
		strings.ContainsAny(o.Version, `/\`) {
		return "", errors.New("invalid app version")
	}
	if o.Manifest == nil {
		return "", errors.New("missing runtime manifest")
	}
	r := o.Manifest.Runtime
	if r.Platform != "darwin-arm64" || r.NodeVersion != "24.22.1" {
		return "", errors.New("runtime requires darwin-arm64 Node 24.22.1")
	}
	ids := []string{r.Node, r.Bridge}
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
		if c.ID == r.Node || c.ID == r.Bridge || !c.Distribution.Available() || !strings.HasPrefix(c.Path, "runtime/") {
			continue
		}
		components = append(components, c)
	}
	parent := filepath.Join(o.Support, "runtime")
	if err := privateDir(o.Support); err != nil {
		return "", err
	}
	if err := privateDir(parent); err != nil {
		return "", err
	}
	dest := filepath.Join(parent, o.Version)
	if _, err := os.Lstat(dest); err == nil {
		err := verify(dest, o.Manifest, components)
		if err == nil {
			err = verifySealed(dest, o.Manifest)
		}
		if err != nil {
			return "", errors.Join(err, discard(dest))
		}
		return dest, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := verify(o.Assets, o.Manifest, components); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(parent, ".stage-")
	if err != nil {
		return "", err
	}
	defer discard(tmp)
	for _, c := range components {
		data, err := os.ReadFile(filepath.Join(o.Assets, filepath.FromSlash(c.Path)))
		if err != nil {
			return "", err
		}
		target := filepath.Join(tmp, filepath.FromSlash(c.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
		mode := fs.FileMode(0o500)
		if c.ID == r.Bridge {
			mode = 0o400
		}
		if err := os.WriteFile(target, data, mode); err != nil {
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
	if err := verify(tmp, o.Manifest, components); err != nil {
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
	if err := verifySealed(tmp, o.Manifest); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func verifySealed(root string, m *manifest.Manifest) error {
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

func verify(root string, m *manifest.Manifest, components []manifest.Component) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("runtime root is not a real directory")
	}
	for _, c := range components {
		if err := m.Verify(root, c.ID); err != nil {
			return err
		}
		if c.ID == m.Runtime.Bridge {
			continue
		}
		f, err := macho.Open(filepath.Join(root, filepath.FromSlash(c.Path)))
		if err != nil {
			return fmt.Errorf("%s: %w", c.ID, err)
		}
		cpu := f.Cpu
		f.Close()
		if cpu != macho.CpuArm64 {
			return fmt.Errorf("%s: expected arm64 Mach-O", c.ID)
		}
	}
	// Packaged resources may contain the app icon and notices outside runtime/.
	base := filepath.Join(root, "runtime")
	return filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
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
		for _, c := range components {
			if filepath.ToSlash(rel) == c.Path {
				return nil
			}
		}
		return fmt.Errorf("unrecorded runtime file %s", rel)
	})
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
