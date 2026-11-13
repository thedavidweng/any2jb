package usbgadget

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/thedavidweng/any2jb/internal/session"
)

const (
	HelperFlag = "--internal-linux-usb-helper"
	PkexecPath = "/usr/bin/pkexec"
)

func defaultSleepMS(ctx context.Context, ms int) error {
	if ms <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// VerifySelfExecutable resolves and checks the running any2jb binary so narrow
// helper launches never execute relative paths or symlinks.
func VerifySelfExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(resolved) {
		return "", errors.New("executable path is not absolute")
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("executable is not a regular executable file")
	}
	return resolved, nil
}

func buildHelperCommand(needsPrivilege bool, subArgs ...string) (*exec.Cmd, error) {
	exe, err := VerifySelfExecutable()
	if err != nil {
		return nil, err
	}
	fullArgs := append([]string{HelperFlag}, subArgs...)
	if !needsPrivilege || configFSGadgetWritable() {
		return exec.Command(exe, fullArgs...), nil
	}
	info, err := os.Stat(PkexecPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil, errors.New("Polkit (pkexec) is unavailable and ConfigFS is not directly writable")
	}
	return exec.Command(PkexecPath, append([]string{exe}, fullArgs...)...), nil
}

func KeyboardHelperCommand(controller string, enumerationSeconds int) (*exec.Cmd, error) {
	return buildHelperCommand(true,
		"keyboard",
		"--controller", controller,
		"--enumeration-timeout-seconds", strconv.Itoa(enumerationSeconds),
	)
}

func CivSwitchHelperCommand(payloadPath, controller string, dryRun bool) (*exec.Cmd, error) {
	args := []string{
		"civswitch",
		"--payload", payloadPath,
		"--controller", controller,
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	return buildHelperCommand(!dryRun, args...)
}

func parseIntRange(text string, low, high int) (int, error) {
	n, err := strconv.Atoi(text)
	if err != nil || n < low || n > high {
		return 0, errors.New("option outside bounds")
	}
	return n, nil
}

// RunHelper executes the narrowly scoped Linux USB HID helper mode. It never
// accepts shell commands, arbitrary ConfigFS paths, or network parameters.
func RunHelper(args []string, in io.Reader, out io.Writer) int {
	return RunHelperWithRoots(DefaultRoots(), defaultSleepMS, args, in, out)
}

func RunHelperWithRoots(roots Roots, sleepMS func(context.Context, int) error, args []string, in io.Reader, out io.Writer) int {
	if len(args) == 0 {
		return 2
	}
	switch args[0] {
	case "keyboard":
		return runKeyboardHelper(roots, args[1:], in, out)
	case "civswitch":
		return runCivSwitchHelper(roots, sleepMS, args[1:], in, out)
	default:
		return 2
	}
}

type reportPacket struct {
	report [8]byte
	err    error
}

func runKeyboardHelper(roots Roots, args []string, in io.Reader, out io.Writer) int {
	controller := os.Getenv("MAC2JB_CONTROLLER")
	timeoutSec := 4
	if raw := os.Getenv("MAC2JB_ENUMERATION_TIMEOUT_SECONDS"); raw != "" {
		n, err := parseIntRange(raw, 1, 120)
		if err != nil {
			return 2
		}
		timeoutSec = n
	}
	validateOnly := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--validate":
			validateOnly = true
		case "--controller":
			if i+1 >= len(args) {
				return 2
			}
			i++
			controller = args[i]
		case "--enumeration-timeout-seconds":
			if i+1 >= len(args) {
				return 2
			}
			i++
			n, err := parseIntRange(args[i], 1, 120)
			if err != nil {
				return 2
			}
			timeoutSec = n
		default:
			return 2
		}
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	packets := make(chan reportPacket, 16)
	go func() {
		defer close(packets)
		for {
			var r [8]byte
			_, err := io.ReadFull(in, r[:])
			packets <- reportPacket{report: r, err: err}
			if err != nil {
				return
			}
		}
	}()

	if validateOnly {
		count := 0
		for pkt := range packets {
			if errors.Is(pkt.err, io.EOF) {
				fmt.Fprintf(out, "Validated %d keyboard reports; no USB initialized.\n", count)
				return 0
			}
			if pkt.err != nil || !session.ValidKeyboardReport(pkt.report) {
				return 1
			}
			count++
		}
		return 0
	}

	if !ValidUDCName(controller) || strings.HasPrefix(controller, "usb-drd") {
		return 2
	}

	openCtx, cancelOpen := context.WithCancel(context.Background())
	// If stdin closes or a signal arrives during enumeration wait (even across
	// a pkexec boundary), cancel enumeration immediately and tear down ConfigFS.
	preReadyPacket := make(chan reportPacket, 1)
	stopWatch := make(chan struct{})
	watchDone := make(chan struct{})
	interrupted := false
	go func() {
		defer close(watchDone)
		select {
		case <-stopWatch:
		case <-sigCh:
			interrupted = true
			cancelOpen()
		case pkt, ok := <-packets:
			if !ok || pkt.err != nil {
				interrupted = true
				cancelOpen()
				return
			}
			preReadyPacket <- pkt
		}
	}()

	g, err := Open(openCtx, roots, KeyboardSpec(controller), time.Duration(timeoutSec)*time.Second)
	close(stopWatch)
	<-watchDone
	cancelOpen()
	if err != nil {
		if interrupted {
			return 130
		}
		return 1
	}
	fmt.Fprintln(out, "MAC2JB_KEYBOARD_READY")

	exitCode := 0
	count := 0
	processPacket := func(pkt reportPacket) (done bool) {
		if errors.Is(pkt.err, io.EOF) {
			return true
		}
		if pkt.err != nil || !session.ValidKeyboardReport(pkt.report) {
			exitCode = 1
			return true
		}
		writeCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		err := g.WriteReport(writeCtx, 0, pkt.report, 250*time.Millisecond)
		cancel()
		if err != nil {
			exitCode = 1
			return true
		}
		count++
		fmt.Fprintf(out, "MAC2JB_KEYBOARD_SENT %d\n", count)
		return false
	}

	select {
	case pkt := <-preReadyPacket:
		if processPacket(pkt) {
			goto release
		}
	default:
	}

	for {
		select {
		case <-sigCh:
			interrupted = true
			goto release
		case pkt, ok := <-packets:
			if !ok || processPacket(pkt) {
				goto release
			}
		}
	}

release:
	released, closeErr := g.ReleaseAndClose()
	if released && closeErr == nil {
		fmt.Fprintln(out, "MAC2JB_KEYBOARD_RELEASED")
	} else {
		exitCode = 1
	}
	if interrupted && released && closeErr == nil {
		return 130
	}
	return exitCode
}

func runCivSwitchHelper(roots Roots, sleepMS func(context.Context, int) error, args []string, in io.Reader, out io.Writer) int {
	var payloadPath string
	controller := os.Getenv("MAC2JB_CONTROLLER")
	timeoutSec := 4
	if raw := os.Getenv("MAC2JB_ENUMERATION_TIMEOUT_SECONDS"); raw != "" {
		n, err := parseIntRange(raw, 1, 120)
		if err != nil {
			return 2
		}
		timeoutSec = n
	}
	dryRun := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--payload":
			if i+1 >= len(args) {
				return 2
			}
			i++
			payloadPath = args[i]
		case "--controller":
			if i+1 >= len(args) {
				return 2
			}
			i++
			controller = args[i]
		case "--enumeration-timeout-seconds":
			if i+1 >= len(args) {
				return 2
			}
			i++
			n, err := parseIntRange(args[i], 1, 120)
			if err != nil {
				return 2
			}
			timeoutSec = n
		case "--dry-run":
			dryRun = true
		default:
			return 2
		}
	}
	if payloadPath == "" || !filepath.IsAbs(payloadPath) || !ValidUDCName(controller) || strings.HasPrefix(controller, "usb-drd") {
		return 2
	}

	emitFailure := func(msg string) {
		fmt.Fprintf(out, "MAC2JB_CIVSWITCH {\"type\":\"failed\",\"message\":%q}\n", msg)
	}

	payload, err := ParseCivSwitchPayload(payloadPath)
	if err != nil {
		emitFailure("CivSwitch chat payload is invalid or unreadable")
		return 2
	}
	if dryRun {
		fmt.Fprintf(out, "MAC2JB_CIVSWITCH {\"type\":\"dry-run\",\"total\":%d,\"characters\":%d,\"estimatedMs\":%d}\n",
			len(payload.Lines), payload.Characters, payload.EstimatedMS)
		return 0
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-ctx.Done():
		case <-sigCh:
			cancel()
		}
	}()
	if in != nil {
		go func() {
			_, _ = io.Copy(io.Discard, in)
			cancel()
		}()
	}

	g, err := Open(ctx, roots, CivSwitchSpec(controller), time.Duration(timeoutSec)*time.Second)
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(out, `MAC2JB_CIVSWITCH {"type":"cancelled"}`)
			return 130
		}
		emitFailure("USB gamepad and keyboard enumeration failed")
		return 1
	}

	fmt.Fprintf(out, "MAC2JB_CIVSWITCH {\"type\":\"ready\",\"controller\":%q,\"total\":%d}\n", controller, len(payload.Lines))
	finished, cancelled := RunCivSwitchMacro(ctx, payload,
		func(wctx context.Context, iface int, report [8]byte) error {
			return g.WriteReport(wctx, iface, report, 500*time.Millisecond)
		},
		sleepMS,
		func(line string) { fmt.Fprintln(out, line) },
	)
	released, closeErr := g.ReleaseAndClose()
	if !released || closeErr != nil {
		emitFailure("Composite keyboard and gamepad release was not confirmed")
		return 1
	}
	if finished && !cancelled {
		fmt.Fprintf(out, "MAC2JB_CIVSWITCH {\"type\":\"complete\",\"total\":%d,\"completed\":%d}\n", len(payload.Lines), len(payload.Lines))
		return 0
	}
	if cancelled || ctx.Err() != nil {
		fmt.Fprintln(out, `MAC2JB_CIVSWITCH {"type":"cancelled"}`)
		return 130
	}
	emitFailure("CivSwitch USB macro stopped before all lines were sent")
	return 1
}
