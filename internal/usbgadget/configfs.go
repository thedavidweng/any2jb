package usbgadget

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thedavidweng/any2jb/readiness"
)

const GadgetDirName = readiness.OwnGadgetPrefix + "hid"

// Endpoint abstracts an opened /dev/hidgN character device.
type Endpoint interface {
	io.WriteCloser
}

// Roots configures Linux sysfs/configfs/devfs locations for production or tests.
type Roots struct {
	ConfigRoot    string
	UDCRoot       string
	HidgClassRoot string
	DevRoot       string
	OpenEndpoint  func(path string) (Endpoint, error)
	LoadModules   func() error
	PollInterval  time.Duration
}

func DefaultRoots() Roots {
	return Roots{
		ConfigRoot:    "/sys/kernel/config",
		UDCRoot:       "/sys/class/udc",
		HidgClassRoot: "/sys/class/hidg",
		DevRoot:       "/dev",
		OpenEndpoint:  openHidgEndpoint,
		LoadModules:   loadKernelModules,
		PollInterval:  50 * time.Millisecond,
	}
}

func loadKernelModules() error {
	for _, bin := range []string{"/usr/sbin/modprobe", "/sbin/modprobe"} {
		info, err := os.Stat(bin)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		return exec.Command(bin, "libcomposite", "usb_f_hid").Run()
	}
	return errors.New("modprobe not found")
}

type Gadget struct {
	gadgetDir string
	spec      Spec
	endpoints []Endpoint
	ready     []bool
	once      sync.Once
	released  bool
	closeErr  error
}

func writeAttr(path, value string) error {
	return os.WriteFile(path, []byte(value+"\n"), 0o644)
}

func checkUDCConflict(gadgetRoot, udcRoot, controller string) error {
	if _, err := os.Stat(filepath.Join(udcRoot, controller)); err != nil {
		return fmt.Errorf("UDC %s is not available in %s: %w", controller, udcRoot, err)
	}
	ownBound := false
	if entries, err := os.ReadDir(gadgetRoot); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(gadgetRoot, entry.Name(), "UDC"))
			if err != nil {
				continue
			}
			bound := strings.TrimSpace(string(raw))
			if bound != controller {
				continue
			}
			if strings.HasPrefix(entry.Name(), readiness.OwnGadgetPrefix) {
				ownBound = true
				continue
			}
			return fmt.Errorf("UDC %s is already bound to foreign gadget %s; refusing to clobber", controller, entry.Name())
		}
	}
	if !ownBound {
		if raw, err := os.ReadFile(filepath.Join(udcRoot, controller, "function")); err == nil {
			if fn := strings.TrimSpace(string(raw)); fn != "" && !strings.HasPrefix(fn, readiness.OwnGadgetPrefix) {
				return fmt.Errorf("UDC %s is already active (%s); refusing to clobber", controller, fn)
			}
		}
	}
	return nil
}

func rmdirGroup(dir string) error {
	err := os.Remove(dir)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	// Real Linux ConfigFS removes kernel-owned attribute files on rmdir(2);
	// regular-filesystem test fixtures hold ordinary files that must be unlinked first.
	if entries, readErr := os.ReadDir(dir); readErr == nil {
		for _, e := range entries {
			if !e.IsDir() {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// teardownGadgetDir removes an any2jb ConfigFS gadget hierarchy in exact reverse
// kernel ConfigFS order: unbind UDC -> unlink config symlinks -> rmdir config
// strings & config -> rmdir functions -> rmdir gadget strings -> rmdir gadget.
func teardownGadgetDir(gadgetDir string) error {
	if info, err := os.Stat(gadgetDir); err != nil || !info.IsDir() {
		return nil
	}
	var errs []error
	udcPath := filepath.Join(gadgetDir, "UDC")
	if raw, err := os.ReadFile(udcPath); err == nil && strings.TrimSpace(string(raw)) != "" {
		if err := os.WriteFile(udcPath, []byte("\n"), 0o644); err != nil {
			errs = append(errs, err)
		}
	}
	cfgDir := filepath.Join(gadgetDir, "configs/c.1")
	if entries, err := os.ReadDir(cfgDir); err == nil {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "hid.") {
				if err := os.Remove(filepath.Join(cfgDir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
					errs = append(errs, err)
				}
			}
		}
	}
	if err := rmdirGroup(filepath.Join(gadgetDir, "configs/c.1/strings/0x409")); err != nil {
		errs = append(errs, err)
	}
	_ = rmdirGroup(filepath.Join(gadgetDir, "configs/c.1/strings"))
	if err := rmdirGroup(filepath.Join(gadgetDir, "configs/c.1")); err != nil {
		errs = append(errs, err)
	}
	fnRoot := filepath.Join(gadgetDir, "functions")
	if entries, err := os.ReadDir(fnRoot); err == nil {
		for _, entry := range entries {
			if err := rmdirGroup(filepath.Join(fnRoot, entry.Name())); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if err := rmdirGroup(filepath.Join(gadgetDir, "strings/0x409")); err != nil {
		errs = append(errs, err)
	}
	for _, rel := range []string{"functions", "strings", "configs"} {
		_ = rmdirGroup(filepath.Join(gadgetDir, rel))
	}
	if err := rmdirGroup(gadgetDir); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func resolveHIDGPath(roots Roots, fnDir string, index int) (string, error) {
	devNumbers := ""
	if raw, err := os.ReadFile(filepath.Join(fnDir, "dev")); err == nil {
		devNumbers = strings.TrimSpace(string(raw))
	}
	if devNumbers != "" && roots.HidgClassRoot != "" {
		if entries, err := os.ReadDir(roots.HidgClassRoot); err == nil {
			for _, entry := range entries {
				raw, err := os.ReadFile(filepath.Join(roots.HidgClassRoot, entry.Name(), "dev"))
				if err == nil && strings.TrimSpace(string(raw)) == devNumbers {
					candidate := filepath.Join(roots.DevRoot, entry.Name())
					if _, err := os.Stat(candidate); err == nil {
						return candidate, nil
					}
				}
			}
		}
	}
	fallback := filepath.Join(roots.DevRoot, fmt.Sprintf("hidg%d", index))
	if _, err := os.Stat(fallback); err == nil {
		return fallback, nil
	}
	return "", fmt.Errorf("hidg character device for function %d (%s) not ready", index, devNumbers)
}

// Open configures the Linux ConfigFS gadget, binds it to Spec.Controller after
// verifying no foreign gadget owns the UDC, opens /dev/hidgN, and waits up to
// enumerationTimeout for initial neutral report delivery on every interface.
func Open(ctx context.Context, roots Roots, spec Spec, enumerationTimeout time.Duration) (*Gadget, error) {
	if !readiness.ValidLinuxUDCName(spec.Controller) || strings.HasPrefix(spec.Controller, "usb-drd") {
		return nil, fmt.Errorf("invalid Linux UDC controller %q", spec.Controller)
	}
	if roots.OpenEndpoint == nil {
		roots.OpenEndpoint = DefaultRoots().OpenEndpoint
	}
	if roots.PollInterval <= 0 {
		roots.PollInterval = 50 * time.Millisecond
	}
	gadgetRoot := filepath.Join(roots.ConfigRoot, "usb_gadget")
	if info, err := os.Stat(gadgetRoot); err != nil || !info.IsDir() {
		if roots.LoadModules != nil {
			_ = roots.LoadModules()
		}
	}
	if info, err := os.Stat(gadgetRoot); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("Linux ConfigFS usb_gadget is unavailable at %s", gadgetRoot)
	}
	if err := checkUDCConflict(gadgetRoot, roots.UDCRoot, spec.Controller); err != nil {
		return nil, err
	}

	gadgetDir := filepath.Join(gadgetRoot, GadgetDirName)
	if err := teardownGadgetDir(gadgetDir); err != nil {
		return nil, fmt.Errorf("failed to clean stale any2jb gadget: %w", err)
	}

	g := &Gadget{
		gadgetDir: gadgetDir,
		spec:      spec,
		endpoints: make([]Endpoint, len(spec.Functions)),
		ready:     make([]bool, len(spec.Functions)),
	}
	ok := false
	defer func() {
		if !ok {
			_, _ = g.ReleaseAndClose()
		}
	}()

	if err := os.Mkdir(gadgetDir, 0o755); err != nil {
		return nil, err
	}
	for _, dir := range []string{
		filepath.Join(gadgetDir, "strings/0x409"),
		filepath.Join(gadgetDir, "configs/c.1/strings/0x409"),
		filepath.Join(gadgetDir, "functions"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	attrs := [][2]string{
		{"idVendor", spec.VendorID},
		{"idProduct", spec.ProductID},
		{"bcdDevice", spec.BCDDevice},
		{"bcdUSB", spec.BCDUSB},
		{"bDeviceClass", "0x00"},
		{"bDeviceSubClass", "0x00"},
		{"bDeviceProtocol", "0x00"},
		{"strings/0x409/manufacturer", spec.Manufacturer},
		{"strings/0x409/product", spec.Product},
		{"strings/0x409/serialnumber", spec.SerialNumber},
		{"configs/c.1/strings/0x409/configuration", spec.Configuration},
		{"configs/c.1/MaxPower", "250"},
	}
	for _, kv := range attrs {
		if err := writeAttr(filepath.Join(gadgetDir, kv[0]), kv[1]); err != nil {
			return nil, err
		}
	}
	fnDirs := make([]string, len(spec.Functions))
	for i, fn := range spec.Functions {
		fnDir := filepath.Join(gadgetDir, "functions", fn.Name)
		fnDirs[i] = fnDir
		if err := os.MkdirAll(fnDir, 0o755); err != nil {
			return nil, err
		}
		if err := writeAttr(filepath.Join(fnDir, "protocol"), strconv.Itoa(int(fn.Protocol))); err != nil {
			return nil, err
		}
		if err := writeAttr(filepath.Join(fnDir, "subclass"), strconv.Itoa(int(fn.Subclass))); err != nil {
			return nil, err
		}
		if err := writeAttr(filepath.Join(fnDir, "report_length"), strconv.Itoa(int(fn.ReportLength))); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(fnDir, "report_desc"), fn.ReportDesc, 0o644); err != nil {
			return nil, err
		}
		if err := os.Symlink(fnDir, filepath.Join(gadgetDir, "configs/c.1", fn.Name)); err != nil {
			return nil, err
		}
	}
	if err := writeAttr(filepath.Join(gadgetDir, "UDC"), spec.Controller); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(enumerationTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		allReady := true
		for i, fn := range spec.Functions {
			if g.endpoints[i] == nil {
				devPath, err := resolveHIDGPath(roots, fnDirs[i], i)
				if err != nil {
					allReady = false
					continue
				}
				ep, err := roots.OpenEndpoint(devPath)
				if err != nil {
					allReady = false
					continue
				}
				g.endpoints[i] = ep
			}
			if !g.ready[i] {
				n, err := g.endpoints[i].Write(fn.NeutralReport[:])
				if err == nil && n == 8 {
					g.ready[i] = true
				} else if err != nil && !isTemporaryHIDError(err) {
					return nil, err
				} else {
					allReady = false
				}
			}
		}
		if allReady {
			ok = true
			return g, nil
		}
		if !time.Now().Before(deadline) {
			return nil, errors.New("USB host did not enumerate HID gadget before timeout")
		}
		timer := time.NewTimer(roots.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (g *Gadget) WriteReport(ctx context.Context, iface int, report [8]byte, timeout time.Duration) error {
	if iface < 0 || iface >= len(g.endpoints) || g.endpoints[iface] == nil {
		return errors.New("HID interface is not open")
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := g.endpoints[iface].Write(report[:])
		if err == nil && n == 8 {
			return nil
		}
		if err != nil && !isTemporaryHIDError(err) {
			return err
		}
		if !time.Now().Before(deadline) {
			if err != nil {
				return err
			}
			return errors.New("short HID report write")
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// ReleaseAndClose sends neutral release reports on all enumerated interfaces
// (keyboard before gamepad for composite parity with civswitch-hid.c), closes
// /dev/hidgN endpoints, unbinds the UDC, and removes ConfigFS directories.
func (g *Gadget) ReleaseAndClose() (bool, error) {
	g.once.Do(func() {
		allReleased := true
		anyReady := false
		// Release in reverse interface order so composite releases keyboard (1)
		// then gamepad (0), matching civswitch-hid.c release_all.
		for i := len(g.endpoints) - 1; i >= 0; i-- {
			if g.endpoints[i] == nil || !g.ready[i] {
				continue
			}
			anyReady = true
			ifaceReleased := false
			for attempt := 0; attempt < 3; attempt++ {
				ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
				err := g.WriteReport(ctx, i, g.spec.Functions[i].NeutralReport, 80*time.Millisecond)
				cancel()
				if err == nil {
					ifaceReleased = true
					break
				}
			}
			if !ifaceReleased {
				allReleased = false
			}
		}
		var errs []error
		for i, ep := range g.endpoints {
			if ep != nil {
				if err := ep.Close(); err != nil {
					errs = append(errs, err)
				}
				g.endpoints[i] = nil
			}
		}
		if err := teardownGadgetDir(g.gadgetDir); err != nil {
			errs = append(errs, err)
		}
		g.released = anyReady && allReleased
		g.closeErr = errors.Join(errs...)
	})
	return g.released, g.closeErr
}
