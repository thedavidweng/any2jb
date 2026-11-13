package retained

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
)

// VerifyHomebrewRuntime checks a prepared portable runtime against the
// manifest the pinned prepare script wrote: the exact recorded file set, every
// digest, and the recorded lock and font-adapter digests. Symlinks, extra
// files and drift fail; the runtime is what homebrew children execute.
func VerifyHomebrewRuntime(dir, lockSHA256, adapterSHA256 string) error {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	var document struct {
		LockSHA256        string `json:"lockSha256"`
		FontAdapterSHA256 string `json:"fontAdapterSha256"`
		Files             []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(data, &document); err != nil || len(document.Files) == 0 {
		return errors.New("invalid homebrew runtime manifest")
	}
	if document.LockSHA256 != lockSHA256 || document.FontAdapterSHA256 != adapterSHA256 {
		return errors.New("the homebrew runtime was prepared from different pinned inputs")
	}
	expected := map[string]string{}
	for _, file := range document.Files {
		if !fs.ValidPath(file.Path) || expected[file.Path] != "" {
			return fmt.Errorf("invalid homebrew runtime entry %q", file.Path)
		}
		expected[file.Path] = file.SHA256
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	seen := map[string]bool{}
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular homebrew runtime file: %s", name)
		}
		if name == "manifest.json" {
			return nil
		}
		digest, ok := expected[name]
		if !ok {
			return fmt.Errorf("unrecorded homebrew runtime file: %s", name)
		}
		handle, err := root.Open(name)
		if err != nil {
			return err
		}
		defer handle.Close()
		sum := sha256.New()
		if _, err := io.Copy(sum, handle); err != nil {
			return err
		}
		if hex.EncodeToString(sum.Sum(nil)) != digest {
			return fmt.Errorf("changed homebrew runtime file: %s", name)
		}
		seen[name] = true
		return nil
	})
	if err != nil {
		return err
	}
	for name := range expected {
		if !seen[name] {
			return fmt.Errorf("missing homebrew runtime file: %s", name)
		}
	}
	return nil
}

// CheckHomebrewRuntime verifies an already-prepared portable homebrew runtime
// against the staged homebrew-runtime lock and font adapter without invoking
// python3 or touching the network. It returns "" when absent or unverified.
func CheckHomebrewRuntime(toolRoot string) string {
	out, lock, adapter, err := homebrewRuntimePaths(toolRoot)
	if err != nil {
		return ""
	}
	if VerifyHomebrewRuntime(out, lock, adapter) != nil {
		return ""
	}
	return out
}

func homebrewRuntimePaths(toolRoot string) (out, lock, adapter string, err error) {
	if toolRoot == "" {
		return "", "", "", errors.New("missing tool root")
	}
	inputs := filepath.Join(toolRoot, "tools", "homebrew-runtime")
	digest := func(name string) (string, error) {
		data, err := os.ReadFile(filepath.Join(inputs, filepath.FromSlash(name)))
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]), nil
	}
	lock, err = digest("packaging/homebrew-runtime.lock.json")
	if err != nil {
		return "", "", "", err
	}
	adapter, err = digest("packaging/homebrew/sitecustomize.py")
	if err != nil {
		return "", "", "", err
	}
	versionRoot := filepath.Dir(toolRoot)
	out = filepath.Join(versionRoot, "homebrew-runtime", "homebrew")
	return out, lock, adapter, nil
}

// StageHomebrewRuntime prepares the pinned portable runtime from the staged
// homebrew-runtime inputs, unless the prepared output already verifies against
// them. It returns the prepared root, or "" without a source stage.
func StageHomebrewRuntime(o StageOptions, toolRoot string, staged []string) (string, error) {
	return StageHomebrewRuntimeContext(context.Background(), o, toolRoot, staged)
}

// StageHomebrewRuntimeContext prepares the pinned portable runtime with
// cancellation support for on-demand installation.
func StageHomebrewRuntimeContext(ctx context.Context, o StageOptions, toolRoot string, staged []string) (string, error) {
	if o.SourceRoot == "" || !slices.Contains(staged, "homebrew-runtime") {
		return "", nil
	}
	out, lock, adapter, err := homebrewRuntimePaths(toolRoot)
	if err != nil {
		return "", err
	}
	if VerifyHomebrewRuntime(out, lock, adapter) == nil {
		return out, nil
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return "", errors.New("python3 is required to prepare the staged homebrew runtime")
	}
	if err := os.RemoveAll(out); err != nil {
		return "", err
	}
	inputs := filepath.Join(toolRoot, "tools", "homebrew-runtime")
	versionRoot := filepath.Dir(toolRoot)
	downloads := filepath.Join(versionRoot, "homebrew-runtime", "downloads")
	if err := os.MkdirAll(downloads, 0o700); err != nil {
		return "", err
	}
	prepare := exec.CommandContext(ctx, python, filepath.Join(inputs, "scripts", "prepare-homebrew-runtime.py"),
		"--output", out, "--downloads", downloads)
	if err := prepare.Run(); err != nil {
		return "", fmt.Errorf("the staged homebrew runtime preparation failed: %w", err)
	}
	if err := VerifyHomebrewRuntime(out, lock, adapter); err != nil {
		return "", err
	}
	return out, nil
}

// CheckIZLauncher verifies an already-prepared IZlauncher runtime without
// rebuilding it.
func CheckIZLauncher(node, dest string) error {
	if node == "" || dest == "" {
		return errors.New("missing node or IZlauncher destination")
	}
	script := filepath.Join(dest, "scripts", "prepare-izlauncher.cjs")
	return exec.Command(node, script, "--check", dest).Run()
}

// StageIZLauncher builds the pinned prepared IZlauncher runtime from the
// staged, verified sources. It reruns the staged prepare script only when the
// staged check fails, and refuses a runtime that still does not verify. The
// homebrew supervisor verifies the same runtime again at every launch.
func StageIZLauncher(node, dest string) error {
	script := filepath.Join(dest, "scripts", "prepare-izlauncher.cjs")
	run := func(flag string) error {
		command := exec.Command(node, script, flag, dest)
		return command.Run()
	}
	if run("--check") == nil {
		return nil
	}
	if err := os.RemoveAll(filepath.Join(dest, "homebrew")); err != nil {
		return err
	}
	if err := run("--prepare"); err != nil {
		return fmt.Errorf("the staged IZlauncher prepare failed: %w", err)
	}
	if err := run("--check"); err != nil {
		return fmt.Errorf("the staged IZlauncher runtime did not verify: %w", err)
	}
	return nil
}
