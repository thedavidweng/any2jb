// Package manifest owns the pinned host contracts and runtime integrity boundary.
package manifest

import (
	"bytes"
	"debug/elf"
	"debug/macho"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

//go:embed profiles.json*
var embeddedFS embed.FS

const builtDarwinDigestsName = "profiles.json.built"

var compiledDarwinComponentIDs = map[string]bool{
	"libusbd-dylib": true,
	"sender":        true,
	"commands":      true,
	"keyboard":      true,
	"civswitch-hid": true,
}

type Manifest struct {
	Reference  json.RawMessage `json:"reference,omitempty"`
	Runtime    Runtime         `json:"runtime"`
	Format     string          `json:"format"`
	Default    string          `json:"default"`
	Profiles   []Profile       `json:"profiles"`
	Components []Component     `json:"components"`
}

// ReferenceSource pins the upstream reference repository and archive used to
// stage verified bundled assets and optional on-demand homebrew components.
type ReferenceSource struct {
	Upstream      string `json:"upstream"`
	Revision      string `json:"revision"`
	Archive       string `json:"archive"`
	ArchiveSHA256 string `json:"archiveSha256"`
}

// ReferenceInfo decodes the canonical reference block from the manifest.
func (m *Manifest) ReferenceInfo() (ReferenceSource, error) {
	var ref ReferenceSource
	if len(m.Reference) == 0 {
		return ref, fmt.Errorf("missing reference block in manifest")
	}
	if err := json.Unmarshal(m.Reference, &ref); err != nil {
		return ref, err
	}
	if ref.Upstream == "" || ref.Revision == "" {
		return ref, fmt.Errorf("incomplete reference block in manifest")
	}
	return ref, nil
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
		if f, err := macho.NewFile(bytes.NewReader(data)); err == nil {
			if f.Cpu != macho.CpuArm64 {
				return fmt.Errorf("expected darwin-arm64 Mach-O")
			}
			return nil
		}
		fat, err := macho.NewFatFile(bytes.NewReader(data))
		if err != nil {
			return err
		}
		for _, arch := range fat.Arches {
			if arch.Cpu == macho.CpuArm64 {
				return nil
			}
		}
		return fmt.Errorf("expected darwin-arm64 slice in universal Mach-O")
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
	PackagedGzip     string        `json:"packagedGzip,omitempty"`
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

// Load reads the canonical manifest embedded at build time. When
// prepare-runtime compiled Darwin C helpers on the current Xcode toolchain,
// profiles.json.built embeds those verified Mach-O digests alongside profiles.json.
func Load() (*Manifest, error) {
	canonical, err := embeddedFS.ReadFile("profiles.json")
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(canonical, &m); err != nil {
		return nil, err
	}
	built, err := embeddedFS.ReadFile(builtDarwinDigestsName)
	if err == nil {
		var digests map[string]string
		if err := json.Unmarshal(built, &digests); err != nil {
			return nil, fmt.Errorf("invalid embedded %s: %w", builtDarwinDigestsName, err)
		}
		if err := m.ApplyCompiledDarwinDigests(digests); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return &m, nil
}

// ApplyCompiledDarwinDigests records SHA-256 digests produced from the
// manifest-verified C sources for the five Darwin USB sidecar binaries.
// Overrides for any other component ID or malformed hex digests are rejected.
func (m *Manifest) ApplyCompiledDarwinDigests(digests map[string]string) error {
	for id, digest := range digests {
		if !compiledDarwinComponentIDs[id] {
			return fmt.Errorf("compiled digest override disallowed for component %q", id)
		}
		if len(digest) != 64 || strings.ToLower(digest) != digest {
			return fmt.Errorf("invalid sha256 digest for compiled component %q", id)
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return fmt.Errorf("invalid sha256 hex for compiled component %q: %w", id, err)
		}
		updated := false
		for i := range m.Components {
			if m.Components[i].ID == id {
				m.Components[i].SHA256 = digest
				updated = true
				break
			}
		}
		if !updated {
			return fmt.Errorf("unknown compiled component %q", id)
		}
	}
	return nil
}

// WriteCompiledDarwinDigests atomically writes the allowlisted compiled Darwin
// Mach-O digests for Go embed inclusion during packaging.
func WriteCompiledDarwinDigests(target string, digests map[string]string) error {
	check := &Manifest{Components: make([]Component, 0, len(compiledDarwinComponentIDs))}
	for id := range compiledDarwinComponentIDs {
		check.Components = append(check.Components, Component{ID: id})
	}
	if err := check.ApplyCompiledDarwinDigests(digests); err != nil {
		return err
	}
	data, err := json.MarshalIndent(digests, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(target), ".profiles-built-")
	if err != nil {
		return err
	}
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
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
