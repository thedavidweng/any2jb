//go:build darwin

package driver

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

func administrator(ctx context.Context, installer string) error {
	if runtime.GOARCH != "arm64" {
		return errors.New("driver installation requires Apple Silicon macOS")
	}
	// AppleScript receives only a fixed executable and a shell-quoted path.
	_, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", administratorScript(installer)).Output()
	if err != nil {
		return errors.New("administrator authorization or driver installation failed")
	}
	return nil
}

func administratorScript(installer string) string {
	command := "/bin/bash '" + strings.ReplaceAll(installer, "'", "'\"'\"'") + "'"
	command = strings.ReplaceAll(strings.ReplaceAll(command, `\`, `\\`), `"`, `\"`)
	return `do shell script "` + command + `" with administrator privileges`
}
