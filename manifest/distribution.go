package manifest

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

var supportedResourcePlatforms = []string{"darwin-arm64", "linux-amd64", "linux-arm64"}

var knownOverlayPrefixes = []string{
	"darwin-arm64/", "darwin-amd64/", "darwin-universal/", "darwin/",
	"linux-amd64/", "linux-arm64/", "linux/",
	"windows-amd64/", "windows-arm64/", "windows/",
}

func stripOverlayPrefix(name string) string {
	for _, prefix := range knownOverlayPrefixes {
		if trimmed, ok := strings.CutPrefix(name, prefix); ok {
			return trimmed
		}
	}
	return name
}

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
	// MyGo merges platform directories into the packaged resource root.
	// This gate checks the source resources tree before that merge.
	source := *m
	source.Components = append([]Component(nil), m.Components...)
	for i := range source.Components {
		c := &source.Components[i]
		if c.ResourcePlatform != "" {
			if !slices.Contains(supportedResourcePlatforms, c.ResourcePlatform) {
				return fmt.Errorf("unsupported resource platform %q", c.ResourcePlatform)
			}
			c.Path = c.ResourcePlatform + "/" + c.Path
		}
	}
	return source.checkPackage(root, func(string) bool { return false })
}

// CheckPackageMerged checks an assembled macOS app's Contents/Resources, where
// MyGo has already merged platform directories and added its own bundle files.
func (m *Manifest) CheckPackageMerged(root string) error {
	return m.CheckPackageMergedForPlatform(root, "darwin-arm64")
}

// CheckPackageMergedForPlatform checks an assembled platform output directory
// where MyGo has merged platform overlays for target platform.
func (m *Manifest) CheckPackageMergedForPlatform(root, platform string) error {
	if !slices.Contains(supportedResourcePlatforms, platform) {
		return fmt.Errorf("unsupported resource platform %q", platform)
	}
	merged := *m
	merged.Components = nil
	for _, c := range m.Components {
		if c.ResourcePlatform != "" && c.ResourcePlatform != platform {
			continue
		}
		merged.Components = append(merged.Components, c)
	}
	return merged.checkPackage(root, func(name string) bool {
		if strings.HasPrefix(platform, "darwin-") {
			dir, base := path.Split(name)
			return name == "AppIcon.icns" || base == "InfoPlist.strings" &&
				strings.Count(dir, "/") == 1 && strings.HasSuffix(dir, ".lproj/")
		}
		if strings.HasPrefix(platform, "linux-") {
			switch name {
			case "any2jb", "any2jb.desktop", "any2jb.png", "any2jb-256x256.png", "any2jb-512x512.png", "install.sh", "PKGBUILD":
				return true
			}
			return strings.HasPrefix(name, "any2jb-") && strings.HasSuffix(name, ".tar.gz") ||
				strings.HasPrefix(name, "any2jb_") && strings.HasSuffix(name, ".deb")
		}
		return false
	})
}

func (m *Manifest) checkPackage(root string, generated func(string) bool) error {
	// A bundled component must record its notice, and a notice that names a
	// packaged file must ship it. The reference is the packaged path, which no
	// platform prefix changes.
	for _, c := range m.Components {
		if !c.Distribution.Bundled {
			continue
		}
		notice := strings.TrimSpace(c.Distribution.Notice)
		if notice == "" {
			return fmt.Errorf("bundled component %s has no third-party notice", c.ID)
		}
		for _, name := range strings.Fields(notice) {
			for _, n := range m.Components {
				if n.Path == name && !n.Distribution.Bundled {
					return fmt.Errorf("bundled component %s names notice file %s, which is not bundled", c.ID, name)
				}
			}
		}
	}
	for _, c := range m.Components {
		if c.Distribution.Bundled {
			if !c.Distribution.Available() {
				return fmt.Errorf("component %s is bundled but %s", c.ID, c.Distribution.Decision)
			}
			data, err := verifyFile(root, c.ID, c.Path, c.SHA256)
			if err != nil {
				return err
			}
			if m.Runtime.IsNodeComponent(c.ID) || slices.Contains(m.Runtime.Helpers, c.ID) {
				platform := c.ResourcePlatform
				if platform == "" {
					platform = m.Runtime.Platform
				}
				if err := VerifyBinaryArchitecture(platform, data); err != nil {
					return fmt.Errorf("%s: %w", c.ID, err)
				}
				info, err := os.Stat(filepath.Join(root, filepath.FromSlash(c.Path)))
				if err != nil {
					return err
				}
				if info.Mode().Perm()&0o111 == 0 {
					return fmt.Errorf("%s: packaged executable bits missing", c.ID)
				}
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
		profileName := stripOverlayPrefix(name)
		for _, p := range m.Profiles {
			if !p.Distribution.Available() && (profileName == p.Bootstrap || profileName == p.SourceDirectory || strings.HasPrefix(profileName, p.SourceDirectory+"/")) {
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
		if generated(name) {
			return nil
		}
		return fmt.Errorf("package contains unrecorded resource %q", name)
	})
}
