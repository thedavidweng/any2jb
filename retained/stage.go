package retained

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/thedavidweng/any2jb/manifest"
)

// StageOptions is the explicit local developer stage of pinned tool sources.
// A packaged app supplies no SourceRoot, so nothing is staged and every tool
// keeps the canonical manifest's unavailable reason.
type StageOptions struct {
	Support, Version, SourceRoot string
	Manifest                     *manifest.Manifest
	Components                   []string
}

const stageRecord = `{"source":"developer-staged","redistributable":false}`

// StageTools verifies each requested tool source in the developer root, copies
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
	if err := os.WriteFile(filepath.Join(root, "stage.json"), []byte(stageRecord), 0o600); err != nil {
		return "", nil, err
	}
	var staged []string
	var failures error
	for _, id := range o.Components {
		dest := filepath.Join(root, "tools", id)
		if err := stageComponent(o, dest, id); err != nil {
			failures = errors.Join(failures, fmt.Errorf("%s: %w", id, err), os.RemoveAll(dest))
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
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	files, err := o.Manifest.ToolSourceFiles(o.SourceRoot, id)
	if err != nil {
		return err
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
