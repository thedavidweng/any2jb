package manifest

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// Distribution records an evidence-based packaging decision, not legal clearance.
type Distribution struct {
	Decision string `json:"decision"`
	Bundled  bool   `json:"bundled"`
	Reason   string `json:"reason"`
	Notice   string `json:"notice"`
}

// Available is the distribution capability gate, independent of host features.
func (d Distribution) Available() bool { return d.Decision == "may-ship" && d.Bundled }

// DistributionFor supplies the reason to show for an unavailable UI entry.
func (m *Manifest) DistributionFor(id string) (Distribution, error) {
	for _, p := range m.Profiles {
		if p.ID == id {
			return p.Distribution, nil
		}
	}
	for _, c := range m.Components {
		if c.ID == id {
			return c.Distribution, nil
		}
	}
	return Distribution{}, fmt.Errorf("unrecorded distribution component %q", id)
}

// CheckPackage checks every file in a staged resource root against the canonical
// allowlist. Unknown files, denied components, symlinks and changed bytes fail.
// It does not check the app executable, signing, or legal obligations.
func (m *Manifest) CheckPackage(root string) error {
	for _, c := range m.Components {
		if c.Distribution.Bundled {
			if !c.Distribution.Available() {
				return fmt.Errorf("component %s is bundled but %s", c.ID, c.Distribution.Decision)
			}
			if _, err := verifyFile(root, c.ID, c.Path, c.SHA256); err != nil {
				return err
			}
		}
	}
	return filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		for _, p := range m.Profiles {
			if !p.Distribution.Available() && (name == p.Bootstrap || name == p.SourceDirectory || strings.HasPrefix(name, p.SourceDirectory+"/")) {
				return fmt.Errorf("package contains host %s: %s (%s)", p.ID, p.Distribution.Decision, p.Distribution.Reason)
			}
		}
		for _, c := range m.Components {
			if c.Path != name {
				continue
			}
			if !c.Distribution.Available() {
				return fmt.Errorf("package contains %s: %s (%s)", c.ID, c.Distribution.Decision, c.Distribution.Reason)
			}
			_, err := verifyFile(root, c.ID, name, c.SHA256)
			return err
		}
		return fmt.Errorf("package contains unrecorded resource %q", name)
	})
}
