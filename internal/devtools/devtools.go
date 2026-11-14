// Package devtools runs the standalone utility plugins: NXFormat (.nx
// archives), NSP-Box (forwarder .nsp packages), NUP (dump archives), DNSwitch
// (captive-portal DNS and HTTP) and the bundled browser probes.
//
// Every utility runs only from the verified private tool stage
// (<Support>/tools/<Version>), re-checks its manifest component before each
// launch, and reports a missing or tampered plugin as *PluginMissingError so
// the GUI and CLI can point at `plugins install <id>`.
package devtools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/plugins"
)

// Config is the resolved private runtime the utility plugins run in. ToolRoot,
// Support and Manifest are required; Node and HomebrewPython are the staged
// runtimes and stay empty when the app has none.
type Config struct {
	// ToolRoot is the verified private stage, <Support>/tools/<Version>.
	ToolRoot string
	// Support owns the private DNSwitch and browser-probe state.
	Support string
	// Manifest is the canonical manifest that records every plugin tree.
	Manifest *manifest.Manifest
	// Node is the staged Node binary used by the three Node utilities.
	Node string
	// HomebrewPython is the staged portable Python interpreter, when the host
	// platform ships one. Empty falls back to the host python3.
	HomebrewPython string
	// NXConvertPath and NUPModuleRoot are explicit seams for callers that
	// resolve a verified upstream tree themselves. Production leaves them
	// empty and uses the pinned upstream artifacts.
	NXConvertPath string
	NUPModuleRoot string
}

// PluginMissingError reports a utility whose verified plugin is not staged.
// Cause keeps the integrity failure without hiding it from the user.
type PluginMissingError struct {
	ID    string
	Cause error
}

func (e *PluginMissingError) Error() string {
	message := fmt.Sprintf("the %s plugin is not installed; install it with `any2jb plugins install %s`", e.ID, e.ID)
	if e.Cause != nil {
		return fmt.Sprintf("%s (integrity check failed: %v)", message, e.Cause)
	}
	return message
}

// Unwrap exposes the integrity error behind a missing plugin.
func (e *PluginMissingError) Unwrap() error { return e.Cause }

// decodeJSON decodes exactly one JSON value and rejects trailing data, so a
// truncated or concatenated frame never passes as a valid one.
func decodeJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

// decodeLastJSON decodes the last non-empty line of captured output.
func decodeLastJSON(output string, target any) error {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if line := strings.TrimSpace(lines[index]); line != "" {
			return decodeJSON([]byte(line), target)
		}
	}
	return errors.New("no JSON output")
}

// pluginDir verifies the plugin's manifest component and returns the staged
// plugin root, <ToolRoot>/tools/<id>.
func (c Config) pluginDir(id string) (string, error) {
	if c.Manifest == nil {
		return "", errors.New("the utility plugins need the canonical manifest")
	}
	if c.ToolRoot == "" {
		return "", &PluginMissingError{ID: id}
	}
	dir := filepath.Join(c.ToolRoot, "tools", id)
	if err := c.Manifest.Verify(dir, id); err != nil {
		return "", &PluginMissingError{ID: id, Cause: err}
	}
	return dir, nil
}

// pluginFile verifies the plugin stage and returns one regular file below it.
func (c Config) pluginFile(id, rel string) (string, error) {
	dir, err := c.pluginDir(id)
	if err != nil {
		return "", err
	}
	file := filepath.Join(dir, filepath.FromSlash(rel))
	if info, err := os.Lstat(file); err != nil || !info.Mode().IsRegular() {
		return "", &PluginMissingError{ID: id, Cause: fmt.Errorf("%s is missing from the staged plugin", rel)}
	}
	return file, nil
}

// nodeBinary returns the staged Node interpreter.
func (c Config) nodeBinary() (string, error) {
	if c.Node == "" {
		return "", errors.New("the staged Node runtime is not available; reinstall the app runtime")
	}
	info, err := os.Stat(c.Node)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("the staged Node runtime %s is not executable", c.Node)
	}
	return c.Node, nil
}

// privateDir creates (0700), tightens and resolves one private state directory
// below Support. The resolved path is what a child sees, because DNSwitch
// refuses a directory whose real path differs from its argument.
func (c Config) privateDir(name string) (string, error) {
	if c.Support == "" {
		return "", errors.New("the private support directory is not available")
	}
	dir := filepath.Join(c.Support, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

// nxconvert resolves and verifies the NXConvert executable.
func (c Config) nxconvert() (string, error) {
	if _, err := c.pluginDir("nxformat"); err != nil {
		return "", err
	}
	if c.NXConvertPath != "" {
		info, err := os.Stat(c.NXConvertPath)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return "", &PluginMissingError{ID: "nxformat", Cause: fmt.Errorf("the nxconvert binary %s is not executable", c.NXConvertPath)}
		}
		return c.NXConvertPath, nil
	}
	binary, err := plugins.NXConvertBinaryPath(c.ToolRoot, "")
	if err != nil {
		return "", &PluginMissingError{ID: "nxformat", Cause: err}
	}
	return binary, nil
}

var pythonModuleName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

// ResolvePython returns the interpreter a utility runs under: the staged
// portable runtime when it exists and is executable, otherwise the host
// python3. Every required import must succeed on that interpreter.
func ResolvePython(homebrewPython string, requiredImports ...string) (string, error) {
	python := ""
	var stagedErr error
	if homebrewPython != "" {
		info, err := os.Stat(homebrewPython)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			python = homebrewPython
		} else {
			stagedErr = err
			if stagedErr == nil {
				stagedErr = errors.New("not an executable file")
			}
		}
	}
	if python == "" {
		host, err := exec.LookPath("python3")
		if err != nil {
			if stagedErr != nil {
				return "", fmt.Errorf("neither the portable Python runtime nor python3 is available: %w", stagedErr)
			}
			return "", errors.New("python3 is not installed")
		}
		python = host
	}
	if err := RequireImports(python, PythonEnv(homebrewPython, python), requiredImports...); err != nil {
		return "", err
	}
	return python, nil
}

// PythonEnv builds a utility's child environment: PYTHONHOME and PYTHONPATH
// never leak from the parent and .pyc files are never written. The staged
// portable runtime never reads user site packages; the host python3 keeps them,
// because a Linux zstandard wheel is normally installed there.
func PythonEnv(homebrewPython, python string, extra ...string) []string {
	portable := homebrewPython != "" && homebrewPython == python
	base := os.Environ()
	size := len(base) + len(extra) + 2
	if portable {
		size++
	}
	env := make([]string, 0, size)
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if name == "PYTHONHOME" || name == "PYTHONPATH" {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "PYTHONDONTWRITEBYTECODE=1")
	if portable {
		env = append(env, "PYTHONNOUSERSITE=1")
	}
	return append(env, extra...)
}

// RequireImports fails unless every module imports on python with env, naming
// how to install the ones that do not.
func RequireImports(python string, env []string, modules ...string) error {
	for _, name := range modules {
		if !pythonModuleName.MatchString(name) {
			return fmt.Errorf("invalid Python module name %q", name)
		}
		cmd := exec.Command(python, "-B", "-c", "import "+name)
		cmd.Env = env
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s is missing the %s module: %s", filepath.Base(python), name, pythonHint(name))
		}
	}
	return nil
}

func pythonHint(module string) string {
	switch module {
	case "PIL":
		return "install Pillow for this interpreter (python3 -m pip install Pillow), or reinstall the nsp-box plugin"
	case "zstandard":
		return "install zstandard for this interpreter (python3 -m pip install zstandard), or reinstall the nup plugin"
	default:
		return "install it for this interpreter (python3 -m pip install " + module + ")"
	}
}
