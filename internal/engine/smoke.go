package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/thedavidweng/any2jb/internal/manifest"
)

// SelfTest checks packaged bytes and executes only Node's version query and
// the bridge's metadata-only unknown-profile rejection. It never creates a
// session, launches an engine, opens a network listener or accesses USB.
func SelfTest(ctx context.Context, assets, version string, m *manifest.Manifest) error {
	platform, targetRuntime, err := resolveRuntimeTarget(m.Runtime)
	if err != nil {
		return err
	}
	for _, c := range m.Components {
		if !c.Distribution.Bundled || (c.ResourcePlatform != "" && c.ResourcePlatform != platform) {
			continue
		}
		if !c.Distribution.Available() {
			return fmt.Errorf("bundled component %s is not permitted", c.ID)
		}
		if c.Source != nil {
			if err := m.VerifyToolSource(assets, c.ID); err != nil {
				return err
			}
			continue
		}
		rel := assetRelPath(assets, c, true)
		if _, err := m.ReadAt(assets, c.ID, rel); err != nil {
			return err
		}
		if c.ID == targetRuntime.Node {
			info, err := os.Stat(filepath.Join(assets, filepath.FromSlash(rel)))
			if err != nil {
				return err
			}
			if info.Mode().Perm()&0o111 == 0 {
				return errors.New("packaged Node is not executable")
			}
		}
	}
	for _, p := range m.Profiles {
		if p.Distribution.Available() {
			if err := m.VerifyProfile(assets, p.ID); err != nil {
				return err
			}
		}
	}
	support, err := os.MkdirTemp("", "any2jb-self-test-")
	if err != nil {
		return err
	}
	defer func() { _ = discard(support) }()
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
