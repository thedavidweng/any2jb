//go:build darwin

package driver

import (
	"context"
	"os/exec"
	"runtime"
	"strings"

	"github.com/thedavidweng/any2jb/locales"
)

func administrator(ctx context.Context, installer string) error {
	if runtime.GOARCH != "arm64" {
		return locales.Key("dynDrvRequiresAppleSilicon")
	}
	// AppleScript receives only a fixed executable and a shell-quoted path.
	_, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", administratorScript(installer)).Output()
	if err != nil {
		return locales.Key("dynDrvAdminFailed")
	}
	return nil
}

func administratorScript(installer string) string {
	command := "/bin/bash '" + strings.ReplaceAll(installer, "'", "'\"'\"'") + "'"
	command = strings.ReplaceAll(strings.ReplaceAll(command, `\`, `\\`), `"`, `\"`)
	return `do shell script "` + command + `" with administrator privileges`
}
