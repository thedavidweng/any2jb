package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckRejectsUnexplainedSkip(t *testing.T) {
	var output bytes.Buffer
	err := run(context.Background(), []string{"-reference-root", t.TempDir(), "-skip-payload-reason", " "}, &output)
	if err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("unexplained skip accepted: %v\n%s", err, output.String())
	}
}

func TestCheckRequiresExplicitSource(t *testing.T) {
	var output bytes.Buffer
	if err := run(context.Background(), nil, &output); err == nil {
		t.Fatal("check accepted missing pinned source root")
	}
}

// zigScript writes an executable that prints version on `version` and succeeds,
// or the given stderr text and a failure status when version is "".
func zigScript(t *testing.T, dir, name, version, stderr string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	body := "#!/bin/sh\n"
	if version == "" {
		body += "echo '" + stderr + "' >&2\nexit 1\n"
	} else {
		body += "echo " + version + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestZigExecutableResolvesPreparedMiseInstall(t *testing.T) {
	bin := t.TempDir()
	install := filepath.Join(t.TempDir(), "installs", "zig", "0.15.2")
	if err := os.MkdirAll(filepath.Join(install, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	zigScript(t, filepath.Join(install, "bin"), "zig", "0.15.2", "")
	// `mise where zig@0.15.2` prints the install directory.
	if err := os.WriteFile(filepath.Join(bin, "mise"), []byte("#!/bin/sh\nprintf '%s\\n' "+install+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	path, err := zigExecutable(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(install, "bin", "zig") {
		t.Fatalf("zig = %q, want the prepared mise install", path)
	}
}

func TestZigExecutableFallsBackToPATH(t *testing.T) {
	bin := t.TempDir()
	zigScript(t, bin, "zig", "0.15.2", "")
	zigScript(t, bin, "mise", "", "mise ERROR No version is set for shim: zig")
	t.Setenv("PATH", bin)
	path, err := zigExecutable(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if path != "zig" {
		t.Fatalf("zig = %q, want the PATH executable", path)
	}
}

func TestZigExecutableUsesExplicitPath(t *testing.T) {
	bin := t.TempDir()
	explicit := zigScript(t, bin, "zig-0.15.2", "0.15.2", "")
	zigScript(t, bin, "zig", "0.14.1", "")
	path, err := zigExecutable(context.Background(), explicit)
	if err != nil {
		t.Fatal(err)
	}
	if path != explicit {
		t.Fatalf("zig = %q, want the explicit path", path)
	}
}

func TestZigExecutableRejectsWrongOrUnusableCompilers(t *testing.T) {
	for _, probe := range []struct {
		name   string
		setup  func(t *testing.T, bin string)
		match  string
		reject string
	}{
		{
			name: "wrong version on PATH",
			setup: func(t *testing.T, bin string) {
				zigScript(t, bin, "zig", "0.14.1", "")
				zigScript(t, bin, "mise", "", "no version set")
			},
			match:  "0.14.1",
			reject: "0.15.2",
		},
		{
			name: "unusable mise shim",
			setup: func(t *testing.T, bin string) {
				zigScript(t, bin, "zig", "", "mise ERROR No version is set for shim: zig")
				zigScript(t, bin, "mise", "", "mise ERROR No version is set for shim: zig")
			},
			match:  "No version is set for shim: zig",
			reject: "0.15.2",
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			bin := t.TempDir()
			probe.setup(t, bin)
			t.Setenv("PATH", bin)
			path, err := zigExecutable(context.Background(), "")
			if err == nil {
				t.Fatalf("accepted %q, want a Zig %s failure", path, probe.reject)
			}
			if !strings.Contains(err.Error(), probe.match) || !strings.Contains(err.Error(), probe.reject) {
				t.Fatalf("unclear Zig failure %q, want it to name %q and %q", err, probe.reject, probe.match)
			}
		})
	}
}
