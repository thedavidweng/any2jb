package engine_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/manifest"
)

func TestDeveloperStagingRequiresExplicitOptions(t *testing.T) {
	o := fixture(t)
	t.Setenv("ANY2JB_REFERENCE_ROOT", "/not-an-approved-implicit-source")
	if _, err := engine.Stage(o); err != nil {
		t.Fatal(err)
	}
	o.DeveloperProfileID = "pepsipu"
	if _, err := engine.Stage(o); err == nil {
		t.Fatal("developer mode accepted without explicit source")
	}
	o.DeveloperProfileID, o.DeveloperSourceRoot = "", t.TempDir()
	if _, err := engine.Stage(o); err == nil {
		t.Fatal("developer mode accepted without selected profile")
	}
}

func TestPinnedDeveloperStaging(t *testing.T) {
	source := os.Getenv("ANY2JB_REFERENCE_ROOT")
	if source == "" {
		t.Skip("set ANY2JB_REFERENCE_ROOT to the pinned mac2jb clone")
	}
	canonical, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range canonical.Profiles {
		t.Run(profile.ID, func(t *testing.T) {
			o := fixture(t)
			o.Manifest.Profiles = canonical.Profiles
			o.DeveloperSourceRoot = source
			o.DeveloperProfileID = profile.ID
			root, err := engine.Stage(o)
			if err != nil {
				t.Fatal(err)
			}
			if err := o.Manifest.VerifyProfile(root, profile.ID); err != nil {
				t.Fatal(err)
			}
			record, err := engine.ReadRecord(root)
			if err != nil {
				t.Fatal(err)
			}
			if record.Source != "developer-staged" || record.Redistributable || record.ProfileID != profile.ID ||
				record.Revision != profile.Revision || record.SourceSHA256 != profile.SourceSHA256 {
				t.Fatalf("missing developer provenance: %+v", record)
			}
			data, err := os.ReadFile(filepath.Join(root, "profiles.json"))
			if err != nil {
				t.Fatal(err)
			}
			var staged manifest.Manifest
			if err := json.Unmarshal(data, &staged); err != nil {
				t.Fatal(err)
			}
			p, err := staged.Profile(profile.ID)
			if err != nil || p.Distribution.Available() {
				t.Fatal("developer staging changed distribution decision")
			}
			if _, err := os.Stat(filepath.Join(root, "scripts/gui-host.cjs")); !os.IsNotExist(err) {
				t.Fatal("copied GUI orchestration")
			}
			// A developer-stage record never authorizes packaging its engine.
			bundle := t.TempDir()
			bootstrap, err := os.ReadFile(filepath.Join(root, profile.Bootstrap))
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(bundle, profile.Bootstrap)
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, bootstrap, 0o600); err != nil {
				t.Fatal(err)
			}
			gate := &manifest.Manifest{Profiles: canonical.Profiles}
			if err := gate.CheckPackage(bundle); err == nil || !strings.Contains(err.Error(), "package contains host") {
				t.Fatalf("developer engine bypassed package gate: %v", err)
			}
			again, err := engine.Stage(o)
			if err != nil || again != root {
				t.Fatalf("reuse failed: %v", err)
			}
			if err := os.Chmod(filepath.Join(root, profile.Bootstrap), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, profile.Bootstrap), []byte("tampered"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := engine.Stage(o); err == nil {
				t.Fatal("tampered developer stage accepted")
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("tampered developer stage not discarded")
			}
			// A source/manifest mismatch must fail before any stage is activated.
			o.Manifest.Profiles = append([]manifest.Profile(nil), canonical.Profiles...)
			for i := range o.Manifest.Profiles {
				if o.Manifest.Profiles[i].ID == profile.ID {
					o.Manifest.Profiles[i].BootstrapSHA256 = ""
				}
			}
			if _, err := engine.Stage(o); err == nil {
				t.Fatal("unverified developer source accepted")
			}
			o.Manifest.Profiles = canonical.Profiles
			o.DeveloperSourceRoot, o.DeveloperProfileID = "", ""
			bundled, err := engine.Stage(o)
			if err != nil || bundled == root {
				t.Fatalf("developer and bundled stage not isolated: %v", err)
			}
		})
	}
}
