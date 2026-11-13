//go:build linux

package readiness

import (
	"context"
	"os"
	"runtime"
	"syscall"
)

func observePlatform(_ context.Context, o *Observation) {
	o.USBSupported = runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
	ObserveLinux(LinuxProbeRoots{
		ProcNetRoute: "/proc/net/route",
		ProcMounts:   "/proc/mounts",
		UDCRoot:      "/sys/class/udc",
		ConfigRoot:   "/sys/kernel/config",
		ModuleRoot:   "/sys/module",
		PkexecPath:   "/usr/bin/pkexec",
		ModprobeDirs: []string{"/usr/sbin", "/sbin", "/usr/bin", "/bin"},
		EUID:         os.Geteuid(),
		WritableCheck: func(dir string) bool {
			// Probe non-destructively via access(W_OK|X_OK) using syscall.Access
			// without creating files during read-only inspection.
			const wOK, xOK = 0x2, 0x1
			return syscall.Access(dir, wOK|xOK) == nil
		},
	}, o)
}
