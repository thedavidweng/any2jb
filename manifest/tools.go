package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
)

// ToolSource records one pinned read-only tool implementation tree. The
// component's own path and digest identify the inventory that lists the tree;
// Extra records additional files outside it, including provenance and adapters.
// Recording a tree is inventory, not redistribution permission.
type ToolSource struct {
	Directory string            `json:"directory"`
	Format    string            `json:"format"`
	Files     int               `json:"files"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// ToolComponent returns a component that records a tool source tree.
func (m *Manifest) ToolComponent(id string) (Component, error) {
	for _, c := range m.Components {
		if c.ID == id && c.Source != nil {
			return c, nil
		}
	}
	return Component{}, fmt.Errorf("unrecorded tool source %q", id)
}

// ToolSourceFiles verifies the inventory, every recorded file and every extra
// artifact, refuses unrecorded files in the tree, and returns the exact
// root-relative paths a developer stage may copy.
func (m *Manifest) ToolSourceFiles(root, id string) ([]string, error) {
	c, err := m.ToolComponent(id)
	if err != nil {
		return nil, err
	}
	inventory, err := verifyFile(root, id, c.Path, c.SHA256)
	if err != nil {
		return nil, err
	}
	if !fs.ValidPath(c.Source.Directory) || strings.Contains(c.Source.Directory, `\`) {
		return nil, &IntegrityError{Component: id, Path: c.Source.Directory, Err: errors.New("invalid tool source directory")}
	}
	files := []string{c.Path}
	if c.Source.Format == "selected-files" {
		// No whole-tree inventory exists; the component path is the recorded
		// notice and Extra names every file the stage may copy. The tree keeps
		// its other files (write entrypoints excluded from a read-only stage),
		// so only recorded names are verified, never walked.
		if len(c.Source.Extra) != c.Source.Files {
			return nil, &IntegrityError{Component: id, Path: c.Path, Err: errors.New("recorded file count differs from the selected file set")}
		}
	} else {
		recorded, err := c.Source.parse(inventory)
		if err != nil {
			return nil, &IntegrityError{Component: id, Path: c.Path, Err: err}
		}
		if len(recorded) != c.Source.Files {
			return nil, &IntegrityError{Component: id, Path: c.Path, Err: errors.New("inventory size differs from the recorded file count")}
		}
		for name, digest := range recorded {
			if !fs.ValidPath(name) || strings.ContainsAny(name, "\\\r") {
				return nil, &IntegrityError{Component: id, Path: c.Path, Err: fmt.Errorf("invalid inventory entry %q", name)}
			}
			full := path.Join(c.Source.Directory, name)
			if _, err := verifyFile(root, id, full, digest); err != nil {
				return nil, err
			}
			files = append(files, full)
		}
	}
	for name, digest := range c.Source.Extra {
		if !fs.ValidPath(name) || strings.ContainsAny(name, "\\\r") {
			return nil, &IntegrityError{Component: id, Path: c.Path, Err: fmt.Errorf("invalid recorded file %q", name)}
		}
		if _, err := verifyFile(root, id, name, digest); err != nil {
			return nil, err
		}
		files = append(files, name)
	}
	slices.Sort(files)
	files = slices.Compact(files)
	if c.Source.Format != "selected-files" {
		if err := refuseUnrecorded(root, c, files); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// VerifyToolSource re-checks a staged copy without producing a staging list.
func (m *Manifest) VerifyToolSource(root, id string) error {
	_, err := m.ToolSourceFiles(root, id)
	return err
}

func (s *ToolSource) parse(inventory []byte) (map[string]string, error) {
	recorded := map[string]string{}
	switch s.Format {
	case "sha256sums":
		for _, line := range strings.Split(strings.TrimSuffix(string(inventory), "\n"), "\n") {
			digest, name, ok := strings.Cut(line, "  ")
			if _, duplicate := recorded[name]; !ok || duplicate {
				return nil, errors.New("invalid or duplicate SHA256SUMS entry")
			}
			recorded[name] = digest
		}
	case "source-manifest":
		var document struct{ Files map[string]string }
		if err := json.Unmarshal(inventory, &document); err != nil || len(document.Files) == 0 {
			return nil, errors.New("invalid source manifest inventory")
		}
		recorded = document.Files
	default:
		return nil, fmt.Errorf("unsupported inventory format %q", s.Format)
	}
	return recorded, nil
}

func refuseUnrecorded(root string, c Component, files []string) error {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return &IntegrityError{Component: c.ID, Path: c.Source.Directory, Err: err}
	}
	defer dir.Close()
	err = fs.WalkDir(dir.FS(), c.Source.Directory, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular tool source file: %s", name)
		}
		if !slices.Contains(files, name) {
			return fmt.Errorf("unrecorded tool source file: %s", name)
		}
		return nil
	})
	if err != nil {
		return &IntegrityError{Component: c.ID, Path: c.Source.Directory, Err: err}
	}
	return nil
}
