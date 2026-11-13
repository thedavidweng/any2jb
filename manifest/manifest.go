// Package manifest owns the pinned host contracts and runtime integrity boundary.
package manifest

import (
	"bytes"
	"debug/elf"
	"debug/macho"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
)

//go:embed profiles.json
var canonical []byte

type Manifest struct {
	Reference  json.RawMessage `json:"reference,omitempty"`
	Runtime    Runtime         `json:"runtime"`
	Format     string          `json:"format"`
	Default    string          `json:"default"`
	Profiles   []Profile       `json:"profiles"`
	Components []Component     `json:"components"`
}

// RuntimeTarget pins the official Node sidecar archive and component for one platform.
type RuntimeTarget struct {
	Node              string `json:"node"`
	NodeArchive       string `json:"nodeArchive"`
	NodeArchiveSHA256 string `json:"nodeArchiveSha256"`
}

// Runtime pins the release targets and their required standalone sidecars.
type Runtime struct {
	Targets           map[string]RuntimeTarget `json:"targets,omitempty"`
	Platform          string                   `json:"platform"`
	NodeVersion       string                   `json:"nodeVersion"`
	Node              string                   `json:"node"`
	Bridge            string                   `json:"bridge"`
	ToolHost          string                   `json:"toolHost"`
	Helpers           []string                 `json:"helpers"`
	NodeArchive       string                   `json:"nodeArchive"`
	NodeArchiveSHA256 string                   `json:"nodeArchiveSha256"`
}

// Target resolves the Node sidecar entry for platform, falling back to the
// single-target fields used by synthetic unit-test manifests.
func (r Runtime) Target(platform string) (RuntimeTarget, bool) {
	if len(r.Targets) > 0 {
		t, ok := r.Targets[platform]
		return t, ok
	}
	if r.Platform != "" && r.Platform == platform {
		return RuntimeTarget{
			Node:              r.Node,
			NodeArchive:       r.NodeArchive,
			NodeArchiveSHA256: r.NodeArchiveSHA256,
		}, true
	}
	return RuntimeTarget{}, false
}

// Platforms returns the deterministic list of pinned runtime target names.
func (r Runtime) Platforms() []string {
	if len(r.Targets) > 0 {
		names := make([]string, 0, len(r.Targets))
		for name := range r.Targets {
			names = append(names, name)
		}
		slices.Sort(names)
		return names
	}
	if r.Platform != "" {
		return []string{r.Platform}
	}
	return nil
}

// IsNodeComponent reports whether componentID is a pinned Node binary for any target.
func (r Runtime) IsNodeComponent(componentID string) bool {
	if componentID != "" && componentID == r.Node {
		return true
	}
	for _, t := range r.Targets {
		if componentID != "" && componentID == t.Node {
			return true
		}
	}
	return false
}

// VerifyBinaryArchitecture checks that native executable bytes match platform.
func VerifyBinaryArchitecture(platform string, data []byte) error {
	switch platform {
	case "darwin-arm64":
		f, err := macho.NewFile(bytes.NewReader(data))
		if err != nil {
			return err
		}
		if f.Cpu != macho.CpuArm64 {
			return fmt.Errorf("expected darwin-arm64 Mach-O")
		}
		return nil
	case "linux-amd64", "linux-arm64":
		f, err := elf.NewFile(bytes.NewReader(data))
		if err != nil {
			return err
		}
		want := elf.EM_X86_64
		if platform == "linux-arm64" {
			want = elf.EM_AARCH64
		}
		if f.Class != elf.ELFCLASS64 || f.Machine != want {
			return fmt.Errorf("expected %s 64-bit ELF", platform)
		}
		return nil
	default:
		return fmt.Errorf("unsupported binary platform %q", platform)
	}
}

type Profile struct {
	Distribution               Distribution `json:"distribution"`
	ID                         string       `json:"id"`
	Label                      string       `json:"label"`
	Upstream                   string       `json:"upstream"`
	Revision                   string       `json:"revision"`
	SourceDirectory            string       `json:"sourceDirectory"`
	SourceSHA256               string       `json:"sourceSha256"`
	Bootstrap                  string       `json:"bootstrap"`
	BootstrapSHA256            string       `json:"bootstrapSha256"`
	DistributionManifest       string       `json:"distributionManifest"`
	DistributionManifestSHA256 string       `json:"distributionManifestSha256"`
	UpstreamManifest           string       `json:"upstreamManifest"`
	UpstreamManifestSHA256     string       `json:"upstreamManifestSha256"`
	LocalPatch                 string       `json:"localPatch"`
	LocalPatchSHA256           string       `json:"localPatchSha256"`
	LicenseStatus              string       `json:"licenseStatus"`
	DistributionStatus         string       `json:"distributionStatus"`
	Engine                     string       `json:"engine"`
	CustomVersion              string       `json:"customVersion"`
	SaveName                   string       `json:"saveName"`
	StartArgumentCount         int          `json:"startArgumentCount"`
	StartPort                  int          `json:"startPort"`
	SupportsHeldRPC            bool         `json:"supportsHeldRPC"`
	SupportsDemo               bool         `json:"supportsDemo"`
	SupportsSaveKit            bool         `json:"supportsSaveKit"`
	SupportsNativeHBLauncher   bool         `json:"supportsNativeHBLauncher"`
	SupportsModloader          bool         `json:"supportsModloader"`
	SupportedSmileBASIC        []string     `json:"supportedSmileBASIC"`
}

type Component struct {
	Launcher         *LauncherSave `json:"launcher,omitempty"`
	ResourcePlatform string        `json:"resourcePlatform,omitempty"`
	Source           *ToolSource   `json:"source,omitempty"`
	Distribution     Distribution  `json:"distribution"`
	ID               string        `json:"id"`
	Upstream         string        `json:"upstream"`
	Revision         string        `json:"revision"`
	Path             string        `json:"path"`
	SHA256           string        `json:"sha256"`
	LicenseStatus    string        `json:"licenseStatus"`
}

// LauncherSave pins the supported target and archive layout for a save installer.
type LauncherSave struct {
	TitleID               string   `json:"titleId"`
	Versions              []string `json:"versions,omitempty"`
	PhysicalCompatibility bool     `json:"physicalCompatibility,omitempty"`
	FullGame              bool     `json:"fullGame,omitempty"`
	Files                 int      `json:"files"`
	Core                  []string `json:"core"`
}

// Load reads the canonical manifest embedded at build time. The Node bridge
// reads the same profiles.json file, staged unchanged alongside its reader.
func Load() (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(canonical, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

type UnknownProfileError struct{ ID string }

func (e *UnknownProfileError) Error() string { return fmt.Sprintf("unknown host profile %q", e.ID) }

func (m *Manifest) Profile(id string) (Profile, error) {
	for _, p := range m.Profiles {
		if p.ID == id {
			return p, nil
		}
	}
	return Profile{}, &UnknownProfileError{ID: id}
}
