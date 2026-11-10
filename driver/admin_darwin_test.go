//go:build darwin

package driver

import "testing"

func TestAdministratorCanOnlyLaunchQuotedInstaller(t *testing.T) {
	// Merely construct the command. Never execute osascript or the installer.
	got := administratorScript("/tmp/driver's \"source\"/scripts/install-driver.sh")
	want := `do shell script "/bin/bash '/tmp/driver'\"'\"'s \"source\"/scripts/install-driver.sh'" with administrator privileges`
	if got != want {
		t.Fatalf("unsafe administrator request: %s", got)
	}
}
