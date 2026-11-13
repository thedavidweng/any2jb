package readiness

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/thedavidweng/any2jb/session"
)

const OwnGadgetPrefix = "any2jb-"

var reservedCheckIDs = []string{
	"network", "sip", "driver-present", "driver-loaded", "driver-restart",
	"usb-controllers", "usb-platform", "usb-runtime", "runtime", "engine",
	"input-adapter", "linux-udc", "linux-configfs", "linux-gadget-access",
}

// ValidLinuxUDCName accepts sysfs UDC directory names while rejecting path
// traversal, reserved check IDs, and ambiguous placeholders such as "auto".
func ValidLinuxUDCName(name string) bool {
	if name == "" || len(name) > 128 || name == "." || name == ".." || name == "auto" {
		return false
	}
	if slices.Contains(reservedCheckIDs, name) {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' || c == ':') {
			return false
		}
	}
	return true
}

// ParseProcNetRoute extracts the active IPv4 default-route interface from
// Linux /proc/net/route text without invoking external binaries.
func ParseProcNetRoute(text string) string {
	bestIface := ""
	bestVirtual := true
	bestMetric := int64(1<<62 - 1)
	for i, line := range strings.Split(text, "\n") {
		if i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		iface, dest := fields[0], fields[1]
		if iface == "lo" || dest != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(fields[3], 16, 64)
		if err != nil || flags&0x1 == 0 {
			continue
		}
		metric, err := strconv.ParseInt(fields[6], 10, 64)
		if err != nil {
			metric = 0
		}
		virt := isVirtualInterface(iface)
		if bestIface == "" || (bestVirtual && !virt) || (bestVirtual == virt && metric < bestMetric) {
			bestIface, bestVirtual, bestMetric = iface, virt, metric
		}
	}
	return bestIface
}

func parseConfigFSMounted(mountsText, configRoot string) bool {
	clean := filepath.Clean(configRoot)
	for _, line := range strings.Split(mountsText, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && filepath.Clean(fields[1]) == clean && fields[2] == "configfs" {
			return true
		}
	}
	return false
}

// LinuxProbeRoots parameterizes Linux sysfs/procfs/configfs paths so unit tests
// exercise real filesystem trees without touching host kernel state.
type LinuxProbeRoots struct {
	ProcNetRoute  string
	ProcMounts    string
	UDCRoot       string
	ConfigRoot    string
	ModuleRoot    string
	PkexecPath    string
	ModprobeDirs  []string
	EUID          int
	WritableCheck func(string) bool
}

func executableRegularFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// ObserveLinux populates Observation with Linux default route, UDC hardware,
// ConfigFS readiness, narrow privilege availability, and active UDC conflicts.
func ObserveLinux(roots LinuxProbeRoots, o *Observation) {
	if routeData, err := os.ReadFile(roots.ProcNetRoute); err == nil {
		o.DefaultInterface = ParseProcNetRoute(string(routeData))
	}
	g := &LinuxGadget{BusyControllers: map[string]string{}}
	o.Linux = g
	if !o.USBSupported {
		return
	}
	entries, err := os.ReadDir(roots.UDCRoot)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			o.ControllerError = err
		}
	} else {
		for _, entry := range entries {
			if ValidLinuxUDCName(entry.Name()) {
				o.Controllers = append(o.Controllers, entry.Name())
			}
		}
		slices.Sort(o.Controllers)
	}

	if mounts, err := os.ReadFile(roots.ProcMounts); err == nil {
		g.ConfigFSMounted = parseConfigFSMounted(string(mounts), roots.ConfigRoot)
	}
	gadgetRoot := filepath.Join(roots.ConfigRoot, "usb_gadget")
	if info, err := os.Stat(gadgetRoot); err == nil && info.IsDir() {
		g.ConfigFSMounted = true
		g.ConfigFSReady = true
	}
	if roots.ModuleRoot != "" {
		if info, err := os.Stat(filepath.Join(roots.ModuleRoot, "usb_f_hid")); err == nil && info.IsDir() {
			g.HIDModuleReady = true
		}
	}
	g.PolkitAvailable = executableRegularFile(roots.PkexecPath)
	hasModprobe := false
	for _, dir := range roots.ModprobeDirs {
		if executableRegularFile(filepath.Join(dir, "modprobe")) {
			hasModprobe = true
			break
		}
	}
	if roots.EUID == 0 {
		g.DirectWriteAccess = g.ConfigFSReady
	} else if g.ConfigFSReady && roots.WritableCheck != nil {
		g.DirectWriteAccess = roots.WritableCheck(gadgetRoot)
	}
	g.CanLoadModules = !g.ConfigFSReady && hasModprobe && (roots.EUID == 0 || g.PolkitAvailable)

	// Detect UDCs already bound to another program's ConfigFS or legacy gadget.
	ownBound := map[string]bool{}
	if gadgetEntries, err := os.ReadDir(gadgetRoot); err == nil {
		for _, entry := range gadgetEntries {
			if !entry.IsDir() {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(gadgetRoot, entry.Name(), "UDC"))
			if err != nil {
				continue
			}
			bound := strings.TrimSpace(string(raw))
			if bound == "" {
				continue
			}
			if strings.HasPrefix(entry.Name(), OwnGadgetPrefix) {
				ownBound[bound] = true
			} else {
				g.BusyControllers[bound] = entry.Name()
			}
		}
	}
	for _, udc := range o.Controllers {
		if g.BusyControllers[udc] != "" || ownBound[udc] {
			continue
		}
		if raw, err := os.ReadFile(filepath.Join(roots.UDCRoot, udc, "function")); err == nil {
			if fn := strings.TrimSpace(string(raw)); fn != "" && !strings.HasPrefix(fn, OwnGadgetPrefix) {
				g.BusyControllers[udc] = fn
			}
		}
	}
}

func linuxUSBChecks(o Observation, check func(string, string, string, session.CheckState) session.Check) []session.Check {
	g := o.Linux
	udcCheck := check("linux-udc",
		"No USB device controller (UDC) is exposed in /sys/class/udc on this Linux system.",
		"Choose guided manual input or an external Pico HID. Ordinary PC USB-C host ports cannot emulate a USB keyboard.",
		session.RequiresAttention)
	if o.ControllerError != nil {
		udcCheck.Message = "Linux /sys/class/udc could not be inspected."
		udcCheck.NextAction = "Check sysfs access and refresh readiness, or choose guided manual input."
	} else if len(o.Controllers) > 0 {
		udcCheck.State = session.CheckVerified
		udcCheck.Message = "Linux USB device controller hardware exposed in /sys/class/udc."
		udcCheck.NextAction = ""
	}

	cfgCheck := check("linux-configfs",
		"Linux ConfigFS USB gadget subsystem (/sys/kernel/config/usb_gadget) is not active.",
		"Load libcomposite and usb_f_hid or mount configfs, then refresh readiness; or choose guided manual input.",
		session.RequiresAttention)
	switch {
	case o.ControllerError == nil && len(o.Controllers) == 0:
		cfgCheck.Message = "Linux ConfigFS USB gadget setup does not apply without UDC hardware."
		cfgCheck.NextAction = "Choose guided manual input or an external Pico HID."
	case g.ConfigFSReady:
		cfgCheck.State = session.CheckVerified
		cfgCheck.Message = "Linux ConfigFS usb_gadget is mounted and ready."
		cfgCheck.NextAction = ""
	case g.CanLoadModules:
		cfgCheck.Message = "Linux ConfigFS usb_gadget is not active yet (libcomposite / usb_f_hid not loaded)."
		cfgCheck.NextAction = "Load Linux USB gadget kernel modules with administrator approval, then refresh readiness; or choose guided manual input."
	}

	freeControllers := 0
	var singleBusyUDC, singleBusyOwner string
	for _, udc := range o.Controllers {
		if owner := g.BusyControllers[udc]; owner != "" {
			singleBusyUDC, singleBusyOwner = udc, owner
		} else {
			freeControllers++
		}
	}

	accessCheck := check("linux-gadget-access",
		"Narrow USB gadget privilege helper (pkexec) is unavailable and ConfigFS is not writable by the current user.",
		"Install Polkit (pkexec) for per-session gadget setup, grant narrow ConfigFS/hidg permissions, or choose guided manual input.",
		session.RequiresAttention)
	switch {
	case o.ControllerError == nil && len(o.Controllers) == 0:
		accessCheck.Message = "USB gadget permissions are not needed without a UDC."
		accessCheck.NextAction = "Choose guided manual input or an external Pico HID."
	case len(o.Controllers) > 0 && freeControllers == 0 && singleBusyOwner != "":
		accessCheck.Message = "UDC " + singleBusyUDC + " is already bound to another gadget (" + singleBusyOwner + "); any2jb will not clobber it."
		accessCheck.NextAction = "Unbind the existing gadget yourself if intended, then refresh readiness; or choose guided manual input."
	case g.DirectWriteAccess:
		accessCheck.State = session.CheckVerified
		accessCheck.Message = "Current user has write access to Linux ConfigFS usb_gadget."
		accessCheck.NextAction = ""
	case g.PolkitAvailable || g.CanLoadModules:
		accessCheck.State = session.CheckVerified
		accessCheck.Message = "Polkit (pkexec) is available for narrowly scoped USB gadget setup and teardown."
		accessCheck.NextAction = ""
	}

	controllers := check("usb-controllers",
		"No USB device controller (UDC) was discovered in /sys/class/udc.",
		"Choose guided manual input or an external Pico HID.",
		session.RequiresAttention)
	switch {
	case o.ControllerError != nil:
		controllers.Message = "Linux UDC discovery could not be completed."
		controllers.NextAction = "Check /sys/class/udc access and refresh readiness, or choose guided manual input."
	case len(o.Controllers) > 1:
		controllers.Message = "Multiple Linux USB device controllers were discovered; the target UDC must be selected."
		controllers.NextAction = "Select the connected UDC in USB Keyboard settings and verify enumeration before sending input, or choose guided manual input."
	case len(o.Controllers) == 1 && g.BusyControllers[o.Controllers[0]] != "":
		owner := g.BusyControllers[o.Controllers[0]]
		controllers.Message = o.Controllers[0] + " is already bound to " + owner + "; any2jb will not overwrite it."
		controllers.NextAction = "Unbind " + owner + " yourself if intended, or choose guided manual input."
	case len(o.Controllers) == 1:
		controllers.State = session.Available
		controllers.Message = "One Linux UDC (" + o.Controllers[0] + ") is available. Keyboard enumeration and console input are not verified."
		controllers.NextAction = "Verify keyboard enumeration on the connected console before using USB input, or choose guided manual input."
	}

	checks := []session.Check{udcCheck, cfgCheck, accessCheck, controllers}
	if o.ControllerError == nil {
		for _, id := range o.Controllers {
			if owner := g.BusyControllers[id]; owner != "" {
				c := check(id, id+": bound to existing gadget "+owner+"; unavailable to avoid clobbering.",
					"Unbind "+owner+" yourself if intended, or select another free UDC.", session.RequiresAttention)
				c.Controller = id
				checks = append(checks, c)
				continue
			}
			c := check(id, id+": Linux UDC controller, available but not verified.",
				"Connect a USB data cable between the UDC port and the console; verify keyboard enumeration before sending input.",
				session.Available)
			c.Controller = id
			checks = append(checks, c)
		}
	}
	return checks
}
