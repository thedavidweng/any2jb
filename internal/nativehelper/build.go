// Package nativehelper is the one pinned libusbd/helper build path.
package nativehelper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/thedavidweng/any2jb/manifest"
)

func command(ctx context.Context, out io.Writer, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.WaitDelay = time.Second
	return cmd.Run()
}

// Build compiles into an explicitly owned disposable directory; it never runs USB.
func Build(ctx context.Context, out io.Writer, root, stage string, m *manifest.Manifest) error {
	if m == nil || !filepath.IsAbs(root) || !filepath.IsAbs(stage) {
		return errors.New("native build requires a manifest and explicit absolute source/staging paths")
	}
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
	return nil
}
