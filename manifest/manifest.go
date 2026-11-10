// Package manifest owns the pinned host contracts and runtime integrity boundary.
package manifest

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed profiles.json
var canonical []byte

type Manifest struct {
	Format     string      `json:"format"`
	Default    string      `json:"default"`
	Profiles   []Profile   `json:"profiles"`
	Components []Component `json:"components"`
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
	Distribution  Distribution `json:"distribution"`
	ID            string       `json:"id"`
	Upstream      string       `json:"upstream"`
	Revision      string       `json:"revision"`
	Path          string       `json:"path"`
	SHA256        string       `json:"sha256"`
	LicenseStatus string       `json:"licenseStatus"`
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
