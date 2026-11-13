package usbgadget_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/usbgadget"
)

type fakeEndpoint struct {
	mu          sync.Mutex
	failInitial int
	writes      [][8]byte
	closed      bool
	onWrite     func([8]byte)
}

func (e *fakeEndpoint) Write(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return 0, syscall.EBADF
	}
	if len(p) != 8 {
		return 0, syscall.EINVAL
	}
	if e.failInitial > 0 {
		e.failInitial--
		return 0, syscall.ESHUTDOWN
	}
	var r [8]byte
	copy(r[:], p)
	e.writes = append(e.writes, r)
	if e.onWrite != nil {
		e.onWrite(r)
	}
	return 8, nil
}

func (e *fakeEndpoint) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	return nil
}

func (e *fakeEndpoint) snapshot() ([][8]byte, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([][8]byte, len(e.writes))
	copy(out, e.writes)
	return out, e.closed
}

func fakeGadgetRoots(t *testing.T, controller string, count int, failInitial int) (usbgadget.Roots, []*fakeEndpoint) {
	t.Helper()
	base := t.TempDir()
	cfg := filepath.Join(base, "config")
	udc := filepath.Join(base, "udc")
	hidgClass := filepath.Join(base, "class-hidg")
	dev := filepath.Join(base, "dev")
	for _, dir := range []string{
		filepath.Join(cfg, "usb_gadget"),
		filepath.Join(udc, controller),
		hidgClass,
		dev,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	eps := make([]*fakeEndpoint, count)
	byPath := map[string]*fakeEndpoint{}
	for i := 0; i < count; i++ {
		devName := fmt.Sprintf("hidg%d", i)
		devPath := filepath.Join(dev, devName)
		if err := os.WriteFile(devPath, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(hidgClass, devName), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hidgClass, devName, "dev"), []byte(fmt.Sprintf("241:%d\n", i)), 0o644); err != nil {
			t.Fatal(err)
		}
		ep := &fakeEndpoint{failInitial: failInitial}
		eps[i] = ep
		byPath[devPath] = ep
	}
	return usbgadget.Roots{
		ConfigRoot:    cfg,
		UDCRoot:       udc,
		HidgClassRoot: hidgClass,
		DevRoot:       dev,
		PollInterval:  2 * time.Millisecond,
		OpenEndpoint: func(path string) (usbgadget.Endpoint, error) {
			ep, ok := byPath[path]
			if !ok {
				return nil, os.ErrNotExist
			}
			return ep, nil
		},
	}, eps
}

func TestConfigFSKeyboardLifecycleDescriptorsAndTeardown(t *testing.T) {
	roots, eps := fakeGadgetRoots(t, "fe980000.usb", 1, 2)
	ctx := context.Background()
	g, err := usbgadget.Open(ctx, roots, usbgadget.KeyboardSpec("fe980000.usb"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	gadgetDir := filepath.Join(roots.ConfigRoot, "usb_gadget", usbgadget.GadgetDirName)
	checkFile := func(rel, want string) {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(gadgetDir, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if strings.TrimSpace(string(raw)) != want {
			t.Fatalf("%s = %q, want %q", rel, strings.TrimSpace(string(raw)), want)
		}
	}
	checkFile("idVendor", "0x1d6b")
	checkFile("idProduct", "0x1234")
	checkFile("bcdDevice", "0x0100")
	checkFile("bcdUSB", "0x0200")
	checkFile("strings/0x409/manufacturer", "mac2jb")
	checkFile("strings/0x409/product", "USB HID Keyboard")
	checkFile("strings/0x409/serialnumber", "MAC2JBKEYBOARD")
	checkFile("UDC", "fe980000.usb")
	checkFile("functions/hid.usb0/protocol", "1")
	checkFile("functions/hid.usb0/subclass", "1")
	checkFile("functions/hid.usb0/report_length", "8")
	desc, err := os.ReadFile(filepath.Join(gadgetDir, "functions/hid.usb0/report_desc"))
	if err != nil || !bytes.Equal(desc, usbgadget.BootKeyboardReportDescriptor) || len(desc) != 65 {
		t.Fatalf("unexpected boot keyboard report_desc (%d bytes)", len(desc))
	}

	if err := g.WriteReport(ctx, 0, [8]byte{0, 0, 4}, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	released, err := g.ReleaseAndClose()
	if err != nil || !released {
		t.Fatalf("ReleaseAndClose = (%v, %v), want (true, nil)", released, err)
	}
	if _, err := os.Stat(gadgetDir); !os.IsNotExist(err) {
		t.Fatalf("ConfigFS gadget directory was not cleaned up: %v", err)
	}
	writes, closed := eps[0].snapshot()
	if !closed || len(writes) != 3 || writes[0] != [8]byte{} || writes[1] != [8]byte{0, 0, 4} || writes[2] != [8]byte{} {
		t.Fatalf("unexpected HID writes (closed=%v): %v", closed, writes)
	}
}

func TestConfigFSRefusesToClobberForeignGadget(t *testing.T) {
	roots, _ := fakeGadgetRoots(t, "fe980000.usb", 1, 0)
	foreignDir := filepath.Join(roots.ConfigRoot, "usb_gadget", "other-gadget")
	if err := os.MkdirAll(foreignDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreignDir, "UDC"), []byte("fe980000.usb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := usbgadget.Open(context.Background(), roots, usbgadget.KeyboardSpec("fe980000.usb"), time.Second); err == nil {
		t.Fatal("clobbered foreign ConfigFS gadget")
	}
	raw, err := os.ReadFile(filepath.Join(foreignDir, "UDC"))
	if err != nil || strings.TrimSpace(string(raw)) != "fe980000.usb" {
		t.Fatal("foreign gadget UDC binding was disturbed")
	}
}

func TestKeyboardHelperStreamAndStdinCancelDuringEnumeration(t *testing.T) {
	roots, eps := fakeGadgetRoots(t, "dwc3.0.auto", 1, 1)
	var in bytes.Buffer
	r1 := [8]byte{0, 0, 4}
	r2 := [8]byte{}
	in.Write(r1[:])
	in.Write(r2[:])
	var out bytes.Buffer
	code := usbgadget.RunHelperWithRoots(roots, nil, []string{"keyboard", "--controller", "dwc3.0.auto", "--enumeration-timeout-seconds", "2"}, &in, &out)
	if code != 0 {
		t.Fatalf("helper exit code = %d, output:\n%s", code, out.String())
	}
	wantOut := "MAC2JB_KEYBOARD_READY\nMAC2JB_KEYBOARD_SENT 1\nMAC2JB_KEYBOARD_SENT 2\nMAC2JB_KEYBOARD_RELEASED\n"
	if out.String() != wantOut {
		t.Fatalf("helper output =\n%s\nwant:\n%s", out.String(), wantOut)
	}
	writes, closed := eps[0].snapshot()
	if !closed || len(writes) != 4 || writes[len(writes)-1] != [8]byte{} {
		t.Fatalf("unexpected endpoint writes: %v (closed=%v)", writes, closed)
	}

	// Simulate unprivileged parent cancelling during enumeration (never enumerates) by closing stdin pipe.
	stuckRoots, _ := fakeGadgetRoots(t, "dwc3.0.auto", 1, 1_000_000)
	pr, pw := io.Pipe()
	go func() {
		time.Sleep(15 * time.Millisecond)
		_ = pw.Close()
	}()
	var cancelOut bytes.Buffer
	code = usbgadget.RunHelperWithRoots(stuckRoots, nil, []string{"keyboard", "--controller", "dwc3.0.auto", "--enumeration-timeout-seconds", "10"}, pr, &cancelOut)
	if code != 130 {
		t.Fatalf("cancelled helper exit = %d, want 130", code)
	}
	if _, err := os.Stat(filepath.Join(stuckRoots.ConfigRoot, "usb_gadget", usbgadget.GadgetDirName)); !os.IsNotExist(err) {
		t.Fatalf("cancelled helper left stale ConfigFS gadget: %v", err)
	}
}

func TestCivSwitchDifferentialParityAgainstMac2jbReference(t *testing.T) {
	sample := "hello WORLD 1230!@#$%^&*()\nrepeat aa BB 11 -- == [] \\\\ ;; '' ,, .. // ``\r\n\t_ + {} | : \" < > ? ~\n"
	payloadFile := filepath.Join(t.TempDir(), "payload.txt")
	if err := os.WriteFile(payloadFile, []byte(sample), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := usbgadget.ParseCivSwitchPayload(payloadFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(parsed.Lines))
	}

	roots, eps := fakeGadgetRoots(t, "1000480000.usb", 2, 0)
	var clockMS int64
	type timedReport struct {
		atMS  int64
		iface int
		hex   string
	}
	var gotReports []timedReport
	var recMu sync.Mutex
	for iface := range eps {
		idx := iface
		eps[iface].onWrite = func(r [8]byte) {
			recMu.Lock()
			gotReports = append(gotReports, timedReport{atMS: clockMS, iface: idx, hex: hex.EncodeToString(r[:])})
			recMu.Unlock()
		}
	}
	sleepVirtual := func(ctx context.Context, ms int) error {
		recMu.Lock()
		clockMS += int64(ms)
		recMu.Unlock()
		return ctx.Err()
	}

	// Keep stdin open during full run so stdin-EOF watcher does not cancel.
	pr, pw := io.Pipe()
	defer pw.Close()
	var out bytes.Buffer
	code := usbgadget.RunHelperWithRoots(roots, sleepVirtual, []string{
		"civswitch", "--payload", payloadFile, "--controller", "1000480000.usb",
	}, pr, &out)
	if code != 0 {
		t.Fatalf("civswitch helper exit = %d, out:\n%s", code, out.String())
	}
	if clockMS != parsed.EstimatedMS {
		t.Fatalf("virtual elapsed %d ms != estimatedMs %d ms", clockMS, parsed.EstimatedMS)
	}

	reference := os.Getenv("ANY2JB_REFERENCE_ROOT")
	if reference == "" || runtime.GOOS == "windows" {
		return
	}
	// Compile pinned mac2jb civswitch-hid.c with mocked libusbd + virtual clock and compare every report and event!
	dir := t.TempDir()
	mockH := filepath.Join(dir, "mock_civ_libusbd.h")
	mockCode := `#include <libusbd.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <time.h>

static unsigned long long civ_ms;
static uint8_t civ_next_iface;
static FILE *civ_cap;

static int civ_write(libusbd_ctx_t *ctx, uint8_t iface, uint64_t ep, const void *data, uint32_t len, uint64_t timeout) {
    (void)ctx; (void)ep; (void)timeout;
    if (len != 8) return LIBUSBD_NOT_ENUMERATED;
    if (!civ_cap) {
        const char *p = getenv("CIV_CAPTURE");
        if (!p || !(civ_cap = fopen(p, "a"))) abort();
    }
    const uint8_t *b = data;
    fprintf(civ_cap, "%llu %u %02x%02x%02x%02x%02x%02x%02x%02x\n",
        civ_ms, (unsigned)iface, b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7]);
    fflush(civ_cap);
    return 8;
}
static int civ_clock(clockid_t id, struct timespec *v) {
    (void)id;
    v->tv_sec = (time_t)(civ_ms / 1000ULL);
    v->tv_nsec = (long)((civ_ms % 1000ULL) * 1000000ULL);
    return 0;
}
static int civ_nanosleep(const struct timespec *v, struct timespec *rem) {
    (void)rem;
    civ_ms += (unsigned long long)v->tv_sec * 1000ULL + (unsigned long long)(v->tv_nsec / 1000000L);
    return 0;
}
static int civ_add_ep(libusbd_ctx_t *ctx, uint8_t iface, int t, int d, int s, int iv, int u, uint64_t *ep) {
    (void)ctx; (void)iface; (void)t; (void)d; (void)s; (void)iv; (void)u;
    if (ep) *ep = iface;
    return 0;
}
#define libusbd_init(ctx) (*(ctx) = (libusbd_ctx_t *)1, 0)
#define libusbd_free(...) (0)
#define libusbd_set_vid(...) (0)
#define libusbd_set_pid(...) (0)
#define libusbd_set_version(...) (0)
#define libusbd_set_class(...) (0)
#define libusbd_set_subclass(...) (0)
#define libusbd_set_protocol(...) (0)
#define libusbd_set_manufacturer_str(...) (0)
#define libusbd_set_product_str(...) (0)
#define libusbd_set_serial_str(...) (0)
#define libusbd_iface_alloc(ctx, iface) (*(iface) = civ_next_iface++, 0)
#define libusbd_config_finalize(...) (0)
#define libusbd_iface_set_class_cmd_callback(...) (0)
#define libusbd_iface_set_class(...) (0)
#define libusbd_iface_set_subclass(...) (0)
#define libusbd_iface_set_protocol(...) (0)
#define libusbd_iface_standard_desc(...) (0)
#define libusbd_iface_nonstandard_desc(...) (0)
#define libusbd_iface_add_endpoint civ_add_ep
#define libusbd_iface_finalize(...) (0)
#define libusbd_ep_write civ_write
#define clock_gettime civ_clock
#define nanosleep civ_nanosleep
`
	if err := os.WriteFile(mockH, []byte(mockCode), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "civswitch-hid")
	compiler := []string{"cc"}
	if runtime.GOOS == "darwin" {
		compiler = []string{"xcrun", "clang"}
	}
	buildArgs := append(compiler[1:], "-std=c11", "-D_POSIX_C_SOURCE=200809L", "-w",
		"-I", filepath.Join(reference, "vendor/libusbd/include"),
		"-include", mockH, filepath.Join(reference, "civswitch-hid.c"), "-o", bin)
	if out, err := exec.Command(compiler[0], buildArgs...).CombinedOutput(); err != nil {
		t.Fatalf("compile civswitch-hid.c: %v: %s", err, out)
	}

	// 1. Check all 256 single-byte payloads match key_for_ascii acceptance.
	byteFile := filepath.Join(dir, "byte.txt")
	for b := 0; b < 256; b++ {
		if b == '\n' || b == '\r' {
			continue
		}
		if err := os.WriteFile(byteFile, []byte{byte(b), '\n'}, 0o600); err != nil {
			t.Fatal(err)
		}
		cOK := exec.Command(bin, "--payload", byteFile, "--controller", "usb-drd0", "--dry-run").Run() == nil
		_, gErr := usbgadget.ParseCivSwitchPayload(byteFile)
		if cOK != (gErr == nil) {
			t.Fatalf("byte 0x%02x: civswitch-hid.c=%v, Go=%v", b, cOK, gErr == nil)
		}
	}

	// 2. Check real 87-line upstream civswitch payload dry-run JSON matches byte-for-byte.
	realPayload := filepath.Join(reference, "tools/civswitch/chat_payload_100.txt")
	if _, err := os.Stat(realPayload); err == nil {
		cDry, err := exec.Command(bin, "--payload", realPayload, "--controller", "usb-drd0", "--dry-run").Output()
		if err != nil {
			t.Fatal(err)
		}
		var gDry bytes.Buffer
		if code := usbgadget.RunHelperWithRoots(roots, sleepVirtual, []string{"civswitch", "--payload", realPayload, "--controller", "1000480000.usb", "--dry-run"}, nil, &gDry); code != 0 {
			t.Fatalf("Go dry-run exit %d", code)
		}
		if string(cDry) != gDry.String() {
			t.Fatalf("real payload dry-run mismatch:\nmac2jb: %sany2jb: %s", string(cDry), gDry.String())
		}
	}

	// 3. Check full report-by-report, interface-by-interface, ms-by-ms transcript against civswitch-hid.c!
	capFile := filepath.Join(dir, "civ.cap")
	cmd := exec.Command(bin, "--payload", payloadFile, "--controller", "usb-drd0")
	cmd.Env = append(os.Environ(), "CIV_CAPTURE="+capFile)
	cOut, err := cmd.Output()
	if err != nil {
		t.Fatalf("run civswitch-hid: %v", err)
	}
	wantEvents := strings.ReplaceAll(string(cOut), `"controller":"usb-drd0"`, `"controller":"1000480000.usb"`)
	if out.String() != wantEvents {
		t.Fatalf("CivSwitch events mismatch:\nmac2jb:\n%s\nany2jb:\n%s", wantEvents, out.String())
	}
	rawCap, err := os.ReadFile(capFile)
	if err != nil {
		t.Fatal(err)
	}
	wantLines := strings.Split(strings.TrimSpace(string(rawCap)), "\n")
	if len(wantLines) != len(gotReports) {
		t.Fatalf("report count mismatch: mac2jb=%d any2jb=%d", len(wantLines), len(gotReports))
	}
	for i, want := range wantLines {
		got := fmt.Sprintf("%d %d %s", gotReports[i].atMS, gotReports[i].iface, gotReports[i].hex)
		if got != want {
			t.Fatalf("report %d mismatch: mac2jb=%q any2jb=%q", i, want, got)
		}
	}
}
