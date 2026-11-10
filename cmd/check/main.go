// Command check runs offline Go and pinned upstream regression checks.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/thedavidweng/any2jb/manifest"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type suite struct {
	out    io.Writer
	failed bool
}

func (s *suite) check(name string, action func() error) {
	if err := action(); err != nil {
		s.failed = true
		fmt.Fprintf(s.out, "FAIL %s: %v\n", name, err)
	} else {
		fmt.Fprintf(s.out, "PASS %s\n", name)
	}
}

func (s *suite) skip(name, reason string) {
	if strings.TrimSpace(reason) == "" {
		s.check(name, func() error { return fmt.Errorf("skip requires a recorded reason") })
		return
	}
	fmt.Fprintf(s.out, "SKIP %s: %s\n", name, reason)
}

func command(ctx context.Context, out io.Writer, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = time.Second
	return cmd.Run()
}

func run(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(out)
	reference := flags.String("reference-root", "", "explicit pinned mac2jb checkout (read-only)")
	assets := flags.String("runtime-assets", "resources/darwin-arm64", "merged runtime assets containing the verified pinned Node")
	skipPayload := flags.String("skip-payload-reason", "", "record why Zig 0.15.2 payload rebuild cannot run")
	zig := flags.String("zig", "zig", "Zig 0.15.2 executable")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *reference == "" {
		return fmt.Errorf("-reference-root is required; run from the any2jb repository")
	}
	skipRequested := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "skip-payload-reason" {
			skipRequested = true
		}
	})
	if skipRequested && strings.TrimSpace(*skipPayload) == "" {
		return fmt.Errorf("payload skip requires a recorded reason")
	}
	root, err := filepath.Abs(*reference)
	if err != nil {
		return err
	}
	runtimeAssets, err := filepath.Abs(*assets)
	if err != nil {
		return err
	}
	m, err := manifest.Load()
	if err != nil {
		return err
	}
	s := &suite{out: out}
	s.check("Go tests + real engine TCP protocol (not hardware)", func() error {
		cmd := exec.CommandContext(ctx, "go", "test", "-v", "./...")
		cmd.Env = append(os.Environ(), "ANY2JB_REFERENCE_ROOT="+root, "ANY2JB_RUNTIME_ASSETS="+runtimeAssets)
		cmd.Stdout, cmd.Stderr = out, out
		cmd.WaitDelay = time.Second
		return cmd.Run()
	})
	s.check("Node manifest/bridge tests", func() error {
		return command(ctx, out, "", "node", "--test", "engine/bridge.test.cjs", "manifest/profiles.test.cjs")
	})
	pepsipuVerified := false
	for _, p := range m.Profiles {
		s.check(p.ID+" pinned SHA256SUMS/source/bootstrap", func() error {
			err := m.VerifyProfile(root, p.ID)
			if p.ID == "pepsipu" {
				pepsipuVerified = err == nil
			}
			return err
		})
	}
	pepsipu, _ := m.Profile("pepsipu")
	engineRoot := filepath.Join(root, filepath.FromSlash(pepsipu.SourceDirectory))
	if !pepsipuVerified {
		s.skip("pepsipu upstream tests + payload rebuild", "pinned engine integrity failed; refusing to execute it")
	} else {
		s.check("pepsipu upstream tests", func() error {
			return command(ctx, out, engineRoot, "node", "--test", "test.cjs")
		})
		if skipRequested {
			s.skip("pepsipu payload rebuild --check", *skipPayload)
		} else {
			s.check("pepsipu payload rebuild --check", func() error {
				return command(ctx, out, engineRoot, "node", "engine/build-payload.cjs", "--check", "--zig", *zig)
			})
		}
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		s.skip("libusbd + native helper dry-runs", "requires Darwin arm64; current platform is "+runtime.GOOS+"/"+runtime.GOARCH)
	} else {
		s.check("libusbd + four native helper offline checks", func() error {
			return nativeChecks(ctx, out, root, m)
		})
	}
	s.skip("console/USB hardware qualification", "requires a real console and Apple Silicon device-mode USB rig; no hardware operations run")
	if s.failed {
		return fmt.Errorf("regression checks failed (see FAIL suites above)")
	}
	return nil
}

func nativeChecks(ctx context.Context, out io.Writer, root string, m *manifest.Manifest) error {
	var reference struct{ Revision string }
	if err := json.Unmarshal(m.Reference, &reference); err != nil {
		return err
	}
	revision, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(revision)) != reference.Revision {
		return fmt.Errorf("native sources require the canonical pinned mac2jb revision")
	}
	if err := command(ctx, out, root, "git", "diff", "--exit-code", "HEAD", "--", "vendor/libusbd"); err != nil {
		return fmt.Errorf("libusbd source differs from pinned checkout: %w", err)
	}
	for _, id := range []string{"sender.c", "commands.c", "keyboard.c", "civswitch-hid.c"} {
		if err := m.Verify(root, id); err != nil {
			return err
		}
	}
	stage, err := os.MkdirTemp("", "any2jb-native-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	// Compile in a disposable copy. Never modify the pinned reference checkout.
	lib := filepath.Join(stage, "vendor", "libusbd")
	if err := os.CopyFS(lib, os.DirFS(filepath.Join(root, "vendor", "libusbd"))); err != nil {
		return err
	}
	if err := command(ctx, out, lib, "make", "-B"); err != nil {
		return fmt.Errorf("libusbd build: %w", err)
	}
	for _, tool := range []struct{ name, source string }{
		{"sender", "sender.c"}, {"commands", "commands.c"}, {"keyboard", "keyboard.c"}, {"civswitch-hid", "civswitch-hid.c"},
	} {
		args := []string{"clang", "-std=c11", "-D_POSIX_C_SOURCE=200809L", "-O2", "-Wall", "-Wextra",
			"-target", "arm64-apple-macos14.0", "-I" + filepath.Join(lib, "include"),
			filepath.Join(root, tool.source), "-L" + lib, "-lusbd", "-Wl,-rpath,@executable_path/vendor/libusbd",
			"-framework", "CoreFoundation", "-framework", "IOKit", "-o", filepath.Join(stage, tool.name)}
		if tool.name != "civswitch-hid" {
			args = append(args, "-DMAC2JB_GUI=1")
		}
		if err := command(ctx, out, stage, "xcrun", args...); err != nil {
			return fmt.Errorf("%s compile: %w", tool.name, err)
		}
	}
	payload := filepath.Join(stage, "input.txt")
	if err := os.WriteFile(payload, []byte("PRINT 1\n"), 0600); err != nil {
		return err
	}
	for _, check := range []struct {
		name string
		args []string
	}{
		{"sender", []string{filepath.Join(root, "assets/bootstrap.bas"), "--dry-run"}},
		{"commands", []string{"--dry-run"}},
		{"civswitch-hid", []string{"--payload", payload, "--dry-run"}},
	} {
		if err := command(ctx, out, stage, filepath.Join(stage, check.name), check.args...); err != nil {
			return fmt.Errorf("%s dry-run: %w", check.name, err)
		}
	}
	// keyboard has --validate, not --dry-run; exercise actual report parsing.
	cmd := exec.CommandContext(ctx, filepath.Join(stage, "keyboard"), "--validate")
	cmd.Stdin = bytes.NewReader([]byte{0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("keyboard validation: %w", err)
	}
	cmd = exec.CommandContext(ctx, filepath.Join(stage, "keyboard"), "--validate")
	cmd.Stdin = bytes.NewReader([]byte{0})
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err == nil {
		return fmt.Errorf("keyboard accepted a partial report")
	}
	return nil
}
