package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/thedavidweng/any2jb/manifest"
)

// SelfTest checks packaged bytes and executes only Node's version query and
// the bridge's metadata-only unknown-profile rejection. It never creates a
// session, launches an engine, opens a network listener or accesses USB.
func SelfTest(ctx context.Context, assets, version string, m *manifest.Manifest) error {
	for _, c := range m.Components {
		if !c.Distribution.Bundled {
			continue
		}
		if !c.Distribution.Available() {
			return fmt.Errorf("bundled component %s is not permitted", c.ID)
		}
		if err := m.Verify(assets, c.ID); err != nil {
			return err
		}
		if c.ID == m.Runtime.Node {
			info, err := os.Stat(filepath.Join(assets, c.Path))
			if err != nil {
				return err
			}
			if info.Mode().Perm()&0o111 == 0 {
				return errors.New("packaged Node is not executable")
			}
		}
	}
	support, err := os.MkdirTemp("", "any2jb-self-test-")
	if err != nil {
		return err
	}
	defer discard(support)
	root, err := Stage(Options{Assets: assets, Support: support, Version: version, Manifest: m})
	if err != nil {
		return err
	}
	node := filepath.Join(root, "runtime/node")
	output, err := exec.CommandContext(ctx, node, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("bundled Node version check failed: %w", err)
	}
	if string(output) != "v"+m.Runtime.NodeVersion+"\n" {
		return errors.New("unexpected bundled Node version response")
	}
	// Two metadata arguments cannot enter the bridge's six-argument launch
	// path. Use an unknown ID even if a future release permits some engines.
	output, err = exec.CommandContext(ctx, node, filepath.Join(root, "runtime/bridge.cjs"),
		filepath.Join(root, "profiles.json"), "__any2jb_smoke_unknown_profile__").CombinedOutput()
	var response struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if err == nil || json.Unmarshal(output, &response) != nil ||
		response.Type != "error" || response.Message != "unknown host profile" {
		return errors.New("bundled bridge metadata self-test failed")
	}
	return nil
}
