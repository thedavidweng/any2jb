package readiness

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/usbgadget"
	"github.com/thedavidweng/any2jb/locales"
)

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
			if usbgadget.ValidUDCName(entry.Name()) {
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
			if strings.HasPrefix(entry.Name(), usbgadget.OwnGadgetPrefix) {
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
			if fn := strings.TrimSpace(string(raw)); fn != "" && !strings.HasPrefix(fn, usbgadget.OwnGadgetPrefix) {
				g.BusyControllers[udc] = fn
			}
		}
	}
}

func linuxUSBChecks(o Observation) []session.Check {
	g := o.Linux
	udcCheck := makeUSBCheck(session.CheckLinuxUDC,
		locales.Key("dynLinuxNoUDC"), locales.Key("dynLinuxNoUDCAction"), session.RequiresAttention)
	if o.ControllerError != nil {
		setCheckText(&udcCheck, locales.Key("dynLinuxUDCFailed"), locales.Key("dynLinuxUDCFailedAction"))
	} else if len(o.Controllers) > 0 {
		udcCheck.State = session.CheckVerified
		setCheckText(&udcCheck, locales.Key("dynLinuxUDCReady"), locales.Text{})
	}

	cfgCheck := makeUSBCheck(session.CheckLinuxConfigFS,
		locales.Key("dynLinuxCfgInactive"), locales.Key("dynLinuxCfgInactiveAction"), session.RequiresAttention)
	switch {
	case o.ControllerError == nil && len(o.Controllers) == 0:
		setCheckText(&cfgCheck, locales.Key("dynLinuxCfgNoUDC"), locales.Key("dynLinuxCfgNoUDCAction"))
	case g.ConfigFSReady:
		cfgCheck.State = session.CheckVerified
		setCheckText(&cfgCheck, locales.Key("dynLinuxCfgReady"), locales.Text{})
	case g.CanLoadModules:
		setCheckText(&cfgCheck, locales.Key("dynLinuxCfgLoadable"), locales.Key("dynLinuxCfgLoadableAction"))
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

	accessCheck := makeUSBCheck(session.CheckLinuxGadgetAccess,
		locales.Key("dynLinuxAccessUnavailable"), locales.Key("dynLinuxAccessUnavailableAction"), session.RequiresAttention)
	switch {
	case o.ControllerError == nil && len(o.Controllers) == 0:
		setCheckText(&accessCheck, locales.Key("dynLinuxAccessNoUDC"), locales.Key("dynLinuxCfgNoUDCAction"))
	case len(o.Controllers) > 0 && freeControllers == 0 && singleBusyOwner != "":
		setCheckText(&accessCheck,
			locales.Format("dynLinuxAccessBusy", map[string]any{"udc": singleBusyUDC, "owner": singleBusyOwner}),
			locales.Key("dynLinuxAccessBusyAction"))
	case g.DirectWriteAccess:
		accessCheck.State = session.CheckVerified
		setCheckText(&accessCheck, locales.Key("dynLinuxAccessDirect"), locales.Text{})
	case g.PolkitAvailable || g.CanLoadModules:
		accessCheck.State = session.CheckVerified
		setCheckText(&accessCheck, locales.Key("dynLinuxAccessPolkit"), locales.Text{})
	}

	controllers := makeUSBCheck(session.CheckUSBControllers,
		locales.Key("dynLinuxControllersNone"), locales.Key("dynLinuxCfgNoUDCAction"), session.RequiresAttention)
	switch {
	case o.ControllerError != nil:
		setCheckText(&controllers, locales.Key("dynLinuxControllersFailed"), locales.Key("dynLinuxControllersFailedAction"))
	case len(o.Controllers) > 1:
		setCheckText(&controllers, locales.Key("dynLinuxControllersMultiple"), locales.Key("dynLinuxControllersMultipleAction"))
	case len(o.Controllers) == 1 && g.BusyControllers[o.Controllers[0]] != "":
		owner := g.BusyControllers[o.Controllers[0]]
		setCheckText(&controllers,
			locales.Format("dynLinuxControllersSingleBusy", map[string]any{"udc": o.Controllers[0], "owner": owner}),
			locales.Format("dynLinuxControllersSingleBusyAction", map[string]any{"owner": owner}))
	case len(o.Controllers) == 1:
		controllers.State = session.Available
		setCheckText(&controllers,
			locales.Format("dynLinuxControllersOne", map[string]any{"udc": o.Controllers[0]}),
			locales.Key("dynUsbVerifyEnumeration"))
	}

	checks := []session.Check{udcCheck, cfgCheck, accessCheck, controllers}
	if o.ControllerError == nil {
		for _, id := range o.Controllers {
			if owner := g.BusyControllers[id]; owner != "" {
				c := makeUSBCheck(id,
					locales.Format("dynLinuxControllerBusy", map[string]any{"controller": id, "owner": owner}),
					locales.Format("dynLinuxControllerBusyAction", map[string]any{"owner": owner}),
					session.RequiresAttention)
				c.Controller = id
				checks = append(checks, c)
				continue
			}
			c := makeUSBCheck(id,
				locales.Format("dynLinuxController", map[string]any{"controller": id}),
				locales.Key("dynLinuxConnectCable"),
				session.Available)
			c.Controller = id
			checks = append(checks, c)
		}
	}
	return checks
}
