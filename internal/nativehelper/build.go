// Package nativehelper is the one pinned libusbd/helper build path.
package nativehelper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/thedavidweng/any2jb/internal/manifest"
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
// Every C source and libusbd header is verified by SHA-256 against the canonical
// manifest before copying into stage, so neither git metadata nor untracked files
// in root can influence the compiled outputs.
func Build(ctx context.Context, out io.Writer, root, stage string, m *manifest.Manifest) error {
	if m == nil || !filepath.IsAbs(root) || !filepath.IsAbs(stage) {
		return errors.New("native build requires a manifest and explicit absolute source/staging paths")
	}
	for _, id := range []string{"sender.c", "commands.c", "keyboard.c", "civswitch-hid.c"} {
		if err := m.Verify(root, id); err != nil {
			return err
		}
	}
	libFiles, err := m.ToolSourceFiles(root, "libusbd-source")
	if err != nil {
		return err
	}
	sourceRoot, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer sourceRoot.Close()
	for _, rel := range libFiles {
		data, err := sourceRoot.ReadFile(rel)
		if err != nil {
			return err
		}
		dest := filepath.Join(stage, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		file, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		if err := errors.Join(writeErr, file.Close()); err != nil {
			return err
		}
	}
	lib := filepath.Join(stage, "vendor", "libusbd")
	if err := command(ctx, out, lib, "xcrun",
		"clang", "-std=gnu11", "-O2", "-Wall", "-fstack-protector-all",
		"-target", "arm64-apple-macos14.0",
		"-I", "include", "-I", "src",
		"-framework", "CoreFoundation", "-framework", "IOKit",
		"-dynamiclib", "-Wl,-install_name,@rpath/libusbd.dylib", "-Wl,-undefined,dynamic_lookup", "-lpthread",
		"-o", "libusbd.dylib",
		"src/libusbd.c", "src/plat/macos/impl.c", "src/plat/macos/alt_IOUSBDeviceControllerLib.c",
	); err != nil {
		return fmt.Errorf("libusbd build: %w", err)
	}
	for _, tool := range []struct{ name, source string }{
		{"sender", "sender.c"}, {"commands", "commands.c"}, {"keyboard", "keyboard.c"}, {"civswitch-hid", "civswitch-hid.c"},
	} {
		data, err := sourceRoot.ReadFile(tool.source)
		if err != nil {
			return err
		}
		stagedSource := filepath.Join(stage, tool.source)
		if err := os.WriteFile(stagedSource, data, 0o600); err != nil {
			return err
		}
		args := []string{
			"clang", "-std=c11", "-D_POSIX_C_SOURCE=200809L", "-O2", "-Wall", "-Wextra",
			"-target", "arm64-apple-macos14.0", "-Iinclude",
			filepath.Join("..", "..", tool.source), "-L.", "-lusbd", "-Wl,-rpath,@executable_path/vendor/libusbd",
			"-framework", "CoreFoundation", "-framework", "IOKit", "-o", filepath.Join("..", "..", tool.name),
		}
		if tool.name != "civswitch-hid" {
			args = append(args, "-DMAC2JB_GUI=1")
		}
		if err := command(ctx, out, lib, "xcrun", args...); err != nil {
			return fmt.Errorf("%s compile: %w", tool.name, err)
		}
	}
	return nil
}
