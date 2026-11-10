package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/thedavidweng/any2jb/manifest"
)

// StagedRecord describes local engine provenance, not redistribution permission.
// A developer record never changes the canonical manifest's distribution gate.
type StagedRecord struct {
	Source          string `json:"source"`
	Redistributable bool   `json:"redistributable"`
	ProfileID       string `json:"profileID,omitempty"`
	Upstream        string `json:"upstream,omitempty"`
	Revision        string `json:"revision,omitempty"`
	SourceSHA256    string `json:"sourceSha256,omitempty"`
}

// ReadRecord reads diagnostics metadata. Call Stage to verify the runtime;
// reading a record alone does not authenticate files or authorize execution.
func ReadRecord(root string) (StagedRecord, error) {
	var record StagedRecord
	dir, err := os.OpenRoot(root)
	if err != nil {
		return record, err
	}
	defer dir.Close()
	info, err := dir.Lstat("stage.json")
	if err != nil {
		return record, err
	}
	if !info.Mode().IsRegular() {
		return record, errors.New("stage record is not a regular file")
	}
	data, err := dir.ReadFile("stage.json")
	if err != nil {
		return record, err
	}
	err = json.Unmarshal(data, &record)
	return record, err
}

func developerSource(o Options) (*manifest.Profile, StagedRecord, error) {
	record := StagedRecord{Source: "bundled", Redistributable: true}
	if o.DeveloperSourceRoot == "" && o.DeveloperProfileID == "" {
		return nil, record, nil
	}
	if o.DeveloperSourceRoot == "" || o.DeveloperProfileID == "" {
		return nil, record, errors.New("developer staging requires an explicit source root and profile ID")
	}
	p, err := o.Manifest.Profile(o.DeveloperProfileID)
	if err != nil {
		return nil, record, err
	}
	if p.ID == "." || p.ID == ".." || filepath.Base(p.ID) != p.ID {
		return nil, record, errors.New("invalid developer profile ID")
	}
	if err := o.Manifest.VerifyProfile(o.DeveloperSourceRoot, p.ID); err != nil {
		return nil, record, err
	}
	record = StagedRecord{Source: "developer-staged", Redistributable: false, ProfileID: p.ID,
		Upstream: p.Upstream, Revision: p.Revision, SourceSHA256: p.SourceSHA256}
	return &p, record, nil
}

func copyDeveloperProfile(source, dest string, p manifest.Profile) error {
	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer root.Close()
	names := make(map[string]bool)
	err = fs.WalkDir(root.FS(), p.SourceDirectory, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("non-regular developer source: %s", name)
		}
		names[name] = true
		return nil
	})
	if err != nil {
		return err
	}
	for _, name := range []string{p.Bootstrap, p.DistributionManifest, p.UpstreamManifest, p.LocalPatch} {
		if name != "" {
			names[name] = true
		}
	}
	for name := range names {
		// VerifyProfile has already checked these paths and bytes. Verify the
		// complete copy again before activation, including checksum inventories.
		if !fs.ValidPath(name) {
			return fmt.Errorf("invalid developer source path %q", name)
		}
		data, err := root.ReadFile(name)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o400); err != nil {
			return err
		}
	}
	return nil
}
