package retained

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/thedavidweng/any2jb/internal/manifest"
)

// StageOptions configures the verified private stage of pinned tool sources.
// A call without SourceRoot stages nothing.
type StageOptions struct {
	Support, Version, SourceRoot string
	Bundled                      bool
	Manifest                     *manifest.Manifest
	Components                   []string
}

const (
	stageRecord        = `{"source":"developer-staged","redistributable":false}`
	bundledStageRecord = `{"source":"bundled","redistributable":true}`
)

// StageTools verifies each requested tool source in SourceRoot, copies
// exactly its recorded files into a private stage, and verifies the copy again.
// It returns the stage root, the components that verified, and the failures of
// the others; a partial stage leaves the rest unavailable rather than guessed.
func StageTools(o StageOptions) (string, []string, error) {
	if o.SourceRoot == "" || len(o.Components) == 0 {
		return "", nil, nil
	}
	if o.Manifest == nil || o.Version == "" || filepath.Base(o.Version) != o.Version {
		return "", nil, errors.New("tool staging requires a canonical manifest and app version")
	}
	root := filepath.Join(o.Support, "tools", o.Version)
	if err := os.MkdirAll(filepath.Join(root, "tools"), 0o700); err != nil {
		return "", nil, err
	}
	record := stageRecord
	if o.Bundled {
		record = bundledStageRecord
	}
	if err := os.WriteFile(filepath.Join(root, "stage.json"), []byte(record), 0o600); err != nil {
		return "", nil, err
	}
	var staged []string
	var failures error
	for _, id := range o.Components {
		c, err := o.Manifest.ToolComponent(id)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		dest := filepath.Join(root, "tools", id)
		if c.Source.StageAs != "" {
			dest = filepath.Join(root, "tools", filepath.FromSlash(c.Source.StageAs))
		}
		if err := stageComponent(o, dest, id); err != nil {
			// A shared destination keeps its other components; only an own
			// destination is removed on failure.
			if c.Source.StageAs == "" {
				failures = errors.Join(failures, fmt.Errorf("%s: %w", id, err), os.RemoveAll(dest))
			} else {
				failures = errors.Join(failures, fmt.Errorf("%s: %w", id, err))
			}
			continue
		}
		staged = append(staged, id)
	}
	return root, staged, failures
}

func stageComponent(o StageOptions, dest, id string) error {
	if err := o.Manifest.VerifyToolSource(dest, id); err == nil {
		return nil
	}
	files, err := o.Manifest.ToolSourceFiles(o.SourceRoot, id)
	if err != nil {
		return err
	}
	c, err := o.Manifest.ToolComponent(id)
	if err != nil {
		return err
	}
	if c.Source.StageAs == "" {
		if err := os.RemoveAll(dest); err != nil {
			return err
		}
	} else {
		// A staged-as component shares its destination: remove only the files
		// it records before rewriting them.
		for _, name := range files {
			if err := os.Remove(filepath.Join(dest, filepath.FromSlash(name))); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(o.SourceRoot, filepath.FromSlash(name)))
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
	// The copy is what a tool process will require, so verify it, not the source.
	return o.Manifest.VerifyToolSource(dest, id)
}

// VerifyStagedComponents re-verifies previously staged optional tool components
// in toolRoot without touching the network or modifying the stage.
func VerifyStagedComponents(toolRoot string, m *manifest.Manifest, ids []string) []string {
	if toolRoot == "" || m == nil {
		return nil
	}
	var verified []string
	for _, id := range ids {
		c, err := m.ToolComponent(id)
		if err != nil || !c.Distribution.Permitted() {
			continue
		}
		dest := filepath.Join(toolRoot, "tools", id)
		if c.Source.StageAs != "" {
			dest = filepath.Join(toolRoot, "tools", filepath.FromSlash(c.Source.StageAs))
		}
		if m.VerifyToolSource(dest, id) == nil {
			verified = append(verified, id)
		}
	}
	return verified
}
