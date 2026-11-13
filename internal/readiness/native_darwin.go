//go:build darwin

package readiness

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	DriverPath = "/Library/Extensions/usbgadget.kext"
	// Shared with the pinned mac2jb installer; ticket 10 must use the same marker.
	RestartMarkerPath = "/Library/Extensions/.mac2jb-usbgadget-restart-required"
)

func capture(ctx context.Context, executable string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, executable, args...).Output()
	return string(out), err
}

func observePlatform(ctx context.Context, o *Observation) {
	route, err := capture(ctx, "/sbin/route", "-n", "get", "default")
	if err == nil {
		fields := strings.Fields(route)
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "interface:" {
				o.DefaultInterface = fields[i+1]
			}
		}
	}
	o.USBSupported = runtime.GOARCH == "arm64"
	if !o.USBSupported {
		return
	}
	security, securityErr := capture(ctx, "/usr/bin/csrutil", "status")
	if securityErr == nil {
		o.SIPStatus = parseSIPStatus(security)
	}
	info, err := os.Lstat(DriverPath)
	if err == nil {
		o.Driver.Present = info.IsDir()
		if !info.IsDir() {
			o.Driver.PresentError = errors.New("kext path is not a directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		o.Driver.PresentError = err
	}
	loaded, err := capture(ctx, "/usr/bin/kmutil", "showloaded", "--list-only")
	o.Driver.LoadError, o.Driver.Loaded = err, err == nil && parseLoaded(loaded)
	marker, err := os.Stat(RestartMarkerPath)
	if err == nil {
		boot, bootErr := capture(ctx, "/usr/sbin/sysctl", "-n", "kern.boottime")
		o.Driver.RestartError = bootErr
		if bootErr == nil {
			match := regexp.MustCompile(`sec = ([0-9]+),`).FindStringSubmatch(boot)
			if match == nil {
				o.Driver.RestartError = errors.New("invalid boot time")
			} else {
				seconds, err := strconv.ParseInt(match[1], 10, 64)
				o.Driver.RestartError = err
				o.Driver.RestartPending = err == nil && restartPending(marker.ModTime(), time.Unix(seconds, 0))
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		o.Driver.RestartError = err
	}
	registry, err := capture(ctx, "/usr/sbin/ioreg", "-w0", "-p", "IOService", "-c", "AppleARMIODevice", "-r", "-d", "3")
	o.ControllerError = err
	if err == nil {
		o.Controllers = parseControllers(registry)
	}
}

func parseSIPStatus(text string) string {
	switch strings.TrimSpace(text) {
	case "System Integrity Protection status: enabled.":
		return "enabled"
	case "System Integrity Protection status: disabled.":
		return "disabled"
	default:
		return "unknown"
	}
}

func parseLoaded(text string) bool {
	return slices.Contains(strings.Fields(text), "gay.bowser.usbgadget")
}

func restartPending(marker, boot time.Time) bool { return !marker.Before(boot) }

func parseControllers(text string) []string {
	node := regexp.MustCompile(`^([ |]*)\+-o (\S+)`)
	controller := regexp.MustCompile(`^(usb-drd(?:[0-9]|1[0-5]))@`)
	found := map[string]bool{}
	current := ""
	for _, line := range strings.Split(text, "\n") {
		match := node.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if match[1] == "" {
			current = ""
			if id := controller.FindStringSubmatch(match[2]); id != nil {
				current = id[1]
			}
		} else if current != "" && match[2] == "gay_bowser_usbgadget" {
			found[current] = true
		}
	}
	var ids []string
	for id := range found {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		x, _ := strconv.Atoi(strings.TrimPrefix(a, "usb-drd"))
		y, _ := strconv.Atoi(strings.TrimPrefix(b, "usb-drd"))
		return x - y
	})
	return ids
}
