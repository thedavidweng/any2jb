// Package artemis drives the pinned Artemis HTTP tool and the experimental
// Artemis Seedsow filesystem host exactly as mac2jb v0.8.2 does: the same
// request shapes, the same consent boundaries, and the same verified staging.
//
// The plugins themselves are on-demand downloads. Nothing here executes a
// plugin file that the canonical manifest did not record: the tool script, the
// inventory that defines the launcher games and the filesystem host module are
// each checked against the digest the manifest pins before use.
package artemis

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/thedavidweng/any2jb/internal/manifest"
)

// Plugin IDs of the on-demand plugins this package runs.
const (
	SaveKitPlugin   = "savekit"
	ArtemisFSPlugin = "artemis-fs"
)

// PluginMissingError reports an on-demand plugin that is not installed and
// verified under the tool root. GUI and CLI turn it into "install this plugin"
// instead of a generic failure.
type PluginMissingError struct {
	ID  string
	Err error
}

func (e *PluginMissingError) Error() string {
	return "the " + e.ID + " plugin is not installed and verified: " + e.Err.Error()
}

func (e *PluginMissingError) Unwrap() error { return e.Err }

// PluginRoot verifies the installed plugin under toolRoot and returns its
// private stage, which holds the pinned files at their recorded paths.
func PluginRoot(toolRoot string, m *manifest.Manifest, id string) (string, error) {
	if m == nil || !filepath.IsAbs(toolRoot) {
		return "", errors.New("plugin verification needs the canonical manifest and an absolute tool root")
	}
	if id != SaveKitPlugin && id != ArtemisFSPlugin {
		return "", fmt.Errorf("unsupported Artemis plugin %q", id)
	}
	root := filepath.Join(toolRoot, "tools", id)
	if err := m.Verify(root, id); err != nil {
		return "", &PluginMissingError{ID: id, Err: err}
	}
	return root, nil
}

// readRecorded reads one pinned plugin file and refuses bytes the manifest did
// not record. It is the only way this package reads plugin content.
func readRecorded(root string, m *manifest.Manifest, id, relPath string) ([]byte, error) {
	if !fs.ValidPath(relPath) || strings.Contains(relPath, `\`) {
		return nil, fmt.Errorf("invalid plugin path %q", relPath)
	}
	want, ok := recordedDigest(m, id, relPath)
	if !ok {
		return nil, &PluginMissingError{ID: id, Err: fmt.Errorf("%s is not recorded in the manifest", relPath)}
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relPath)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, &PluginMissingError{ID: id, Err: err}
		}
		return nil, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return nil, fmt.Errorf("the %s plugin file %s does not match its recorded digest", id, relPath)
	}
	return data, nil
}

// recordedDigest finds the manifest digest for one plugin file, whether the tree
// is described by an embedded inventory or by explicit extra records.
func recordedDigest(m *manifest.Manifest, id, relPath string) (string, bool) {
	c, err := m.ToolComponent(id)
	if err != nil || c.Source == nil {
		return "", false
	}
	if digest, ok := c.Source.Embedded[relPath]; ok {
		return digest, true
	}
	digest, ok := c.Source.Extra[relPath]
	return digest, ok
}
