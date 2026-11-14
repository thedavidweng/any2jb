package devtools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/thedavidweng/any2jb/internal/plugins"
)

// NUPAction is one nup.cli operation.
type NUPAction string

const (
	NUPPack    NUPAction = "pack"
	NUPInfo    NUPAction = "info"
	NUPList    NUPAction = "list"
	NUPVerify  NUPAction = "verify"
	NUPExtract NUPAction = "extract"
)

// NUPRequest is one NUP operation. Input is an absolute dump folder (pack) or
// .nup archive; Output is the new archive (pack) or extraction folder (extract).
type NUPRequest struct {
	Action NUPAction
	Input  string
	Output string
}

// NUPResult is the tool's output and, after a pack or extract, the new path.
type NUPResult struct {
	Output string
	Path   string
}

// RunNUP packs, lists, verifies or extracts one NUP archive with the verified
// nup modules. A pack is verified before it is linked into place.
func (c Config) RunNUP(ctx context.Context, req NUPRequest) (NUPResult, error) {
	moduleRoot, bundled, err := c.nupModules()
	if err != nil {
		return NUPResult{}, err
	}
	python, env, err := c.nupPython(moduleRoot, bundled)
	if err != nil {
		return NUPResult{}, err
	}
	run := func(args ...string) (string, error) {
		cmd := exec.Command(python, append([]string{"-B", "-m", "nup.cli"}, args...)...)
		cmd.Dir = c.ToolRoot
		cmd.Env = env
		return runSupervised(ctx, cmd, nil)
	}
	switch req.Action {
	case NUPPack:
		output, err := nupOutputArchive(req.Output)
		if err != nil {
			return NUPResult{}, err
		}
		source, err := nupSourceFolder(req.Input)
		if err != nil {
			return NUPResult{}, err
		}
		suffix, err := randomSuffix()
		if err != nil {
			return NUPResult{}, err
		}
		temporary := output + ".mac2jb-" + suffix + ".partial"
		defer os.Remove(temporary)
		packed, err := run("pack", source, temporary, "--quiet")
		if err != nil {
			return NUPResult{}, err
		}
		verified, err := run("verify", temporary)
		if err != nil {
			return NUPResult{}, err
		}
		if err := os.Link(temporary, output); err != nil {
			if errors.Is(err, os.ErrExist) {
				return NUPResult{}, errors.New("the NUP destination already exists")
			}
			return NUPResult{}, err
		}
		return NUPResult{Output: strings.TrimSpace(packed + "\n" + verified), Path: output}, nil
	case NUPInfo, NUPList, NUPVerify:
		archive, err := nupArchive(req.Input)
		if err != nil {
			return NUPResult{}, err
		}
		output, err := run(string(req.Action), archive)
		return NUPResult{Output: output}, err
	case NUPExtract:
		archive, err := nupArchive(req.Input)
		if err != nil {
			return NUPResult{}, err
		}
		target, err := nupExtractionFolder(req.Output)
		if err != nil {
			return NUPResult{}, err
		}
		suffix, err := randomSuffix()
		if err != nil {
			return NUPResult{}, err
		}
		temporary := target + ".mac2jb-" + suffix + ".partial"
		defer os.RemoveAll(temporary)
		output, err := run("extract", archive, temporary)
		if err != nil {
			return NUPResult{}, err
		}
		if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			return NUPResult{}, errors.New("the extraction folder already exists")
		}
		if err := os.Rename(temporary, target); err != nil {
			return NUPResult{}, err
		}
		return NUPResult{Output: output, Path: target}, nil
	default:
		return NUPResult{}, fmt.Errorf("unknown NUP operation %q; choose pack, info, list, verify or extract", req.Action)
	}
}

// nupModules returns the verified nup module parent and the optional bundled
// site-packages directory.
func (c Config) nupModules() (moduleRoot, bundled string, err error) {
	if _, err := c.pluginDir("nup"); err != nil {
		return "", "", err
	}
	if c.NUPModuleRoot != "" {
		info, err := os.Lstat(c.NUPModuleRoot)
		if err != nil || !info.IsDir() {
			return "", "", &PluginMissingError{ID: "nup", Cause: fmt.Errorf("the nup module folder %s is missing", c.NUPModuleRoot)}
		}
		return c.NUPModuleRoot, "", nil
	}
	moduleRoot, bundled, err = plugins.NUPModuleRoot(c.ToolRoot, c.Manifest)
	if err != nil {
		return "", "", &PluginMissingError{ID: "nup", Cause: err}
	}
	return moduleRoot, bundled, nil
}

// nupPython resolves the interpreter and builds an environment whose
// PYTHONPATH holds the nup modules plus the bundled zstandard wheel when the
// interpreter is the CPython 3.13 the wheel was built for.
func (c Config) nupPython(moduleRoot, bundled string) (string, []string, error) {
	python, err := ResolvePython(c.HomebrewPython)
	if err != nil {
		return "", nil, fmt.Errorf("nup needs a Python interpreter: %w", err)
	}
	path := moduleRoot
	if bundled != "" && bundledWheelMatches(c.HomebrewPython, python) {
		path += string(os.PathListSeparator) + bundled
	}
	env := PythonEnv(c.HomebrewPython, python, "PYTHONPATH="+path)
	if err := RequireImports(python, env, "zstandard"); err != nil {
		return "", nil, err
	}
	return python, env, nil
}

// bundledWheelMatches reports whether python is the CPython 3.13 on
// darwin/arm64 that the bundled zstandard wheel targets.
func bundledWheelMatches(homebrewPython, python string) bool {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return false
	}
	cmd := exec.Command(python, "-B", "-c",
		"import sys; print(sys.implementation.name); print('%d.%d' % sys.version_info[:2])")
	cmd.Env = PythonEnv(homebrewPython, python)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	lines := strings.Fields(string(output))
	return len(lines) == 2 && lines[0] == "cpython" && lines[1] == "3.13"
}

// nupArchive validates an existing .nup file.
func nupArchive(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".nup") {
		return "", errors.New("choose an existing .nup file")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("the selected .nup file is missing or unsafe")
	}
	return path, nil
}

// nupOutputArchive validates a new .nup destination.
func nupOutputArchive(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".nup") {
		return "", errors.New("choose a .nup destination")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("the NUP destination already exists")
	}
	if info, err := os.Lstat(filepath.Dir(path)); err != nil || !info.IsDir() {
		return "", errors.New("the destination folder is missing")
	}
	return path, nil
}

// nupSourceFolder validates an extracted dump folder.
func nupSourceFolder(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("choose a dump folder")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("the selected dump folder is missing or unsafe")
	}
	return path, nil
}

// nupExtractionFolder validates a new extraction folder.
func nupExtractionFolder(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("choose a new extraction folder")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("the extraction folder already exists")
	}
	if info, err := os.Lstat(filepath.Dir(path)); err != nil || !info.IsDir() {
		return "", errors.New("the extraction parent folder is missing")
	}
	return path, nil
}
