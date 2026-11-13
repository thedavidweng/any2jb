// Package parity compares what any2jb sends to the console with what the
// pinned mac2jb reference sends for the same inputs, without hardware.
package parity

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Report is one 8-byte HID input report and the virtual time it is written.
type Report struct {
	AtNS  uint64
	Bytes [8]byte
}

// Transcript is the ordered report stream an input transport writes.
type Transcript []Report

// State keeps only reports that change the key state. A boot keyboard report
// is the whole state, so repeating the previous report, including the neutral
// state the device starts in, changes nothing on the console.
func (t Transcript) State() Transcript {
	var out Transcript
	var previous [8]byte
	for _, r := range t {
		if r.Bytes != previous {
			out = append(out, r)
			previous = r.Bytes
		}
	}
	return out
}

func (t Transcript) String() string {
	var b strings.Builder
	for _, r := range t {
		fmt.Fprintf(&b, "%d %s\n", r.AtNS, hex.EncodeToString(r.Bytes[:]))
	}
	return b.String()
}

// Digest identifies a transcript without storing the typed text.
func (t Transcript) Digest() string {
	sum := sha256.Sum256([]byte(t.String()))
	return hex.EncodeToString(sum[:])
}

// Diff describes the first difference, or returns "" when both are equal.
func Diff(want, got Transcript) string {
	for i := 0; i < len(want) || i < len(got); i++ {
		if i < len(want) && i < len(got) && want[i] == got[i] {
			continue
		}
		line := func(t Transcript) string {
			if i >= len(t) {
				return "<end>"
			}
			return strings.TrimSpace(Transcript{t[i]}.String())
		}
		return fmt.Sprintf("report %d of %d/%d: mac2jb %s, any2jb %s", i, len(want), len(got), line(want), line(got))
	}
	return ""
}

// ParseCapture reads the "<ns> <16 hex digits>" lines the mock writes.
func ParseCapture(data []byte) (Transcript, error) {
	var t Transcript
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		at, report, ok := strings.Cut(scanner.Text(), " ")
		raw, err := hex.DecodeString(report)
		if !ok || err != nil || len(raw) != 8 {
			return nil, fmt.Errorf("invalid capture line %q", scanner.Text())
		}
		ns, err := strconv.ParseUint(at, 10, 64)
		if err != nil {
			return nil, err
		}
		var r Report
		r.AtNS = ns
		copy(r.Bytes[:], raw)
		t = append(t, r)
	}
	return t, scanner.Err()
}

//go:embed mock_libusbd.h
var mockHeader []byte

// Helpers are the pinned mac2jb sender, commands and keyboard helpers built
// from the reference checkout with only USB and the clock replaced.
type Helpers struct{ Sender, Commands, Keyboard string }

// BuildHelpers compiles the GUI builds (-DMAC2JB_GUI=1) that mac2jb ships.
func BuildHelpers(ctx context.Context, reference, dir string) (Helpers, error) {
	if runtime.GOOS == "windows" {
		return Helpers{}, errors.New("the mac2jb helpers need POSIX headers; Windows compares committed digests only")
	}
	header := filepath.Join(dir, "mock_libusbd.h")
	if err := os.WriteFile(header, mockHeader, 0o600); err != nil {
		return Helpers{}, err
	}
	compiler := []string{"cc"}
	if runtime.GOOS == "darwin" {
		compiler = []string{"xcrun", "clang"}
	}
	h := Helpers{filepath.Join(dir, "sender"), filepath.Join(dir, "commands"), filepath.Join(dir, "keyboard")}
	for source, output := range map[string]string{"sender.c": h.Sender, "commands.c": h.Commands, "keyboard.c": h.Keyboard} {
		args := append(compiler[1:], "-std=c11", "-D_POSIX_C_SOURCE=200809L", "-DMAC2JB_GUI=1", "-w",
			"-I", reference, "-I", filepath.Join(reference, "vendor/libusbd/include"),
			"-include", header, filepath.Join(reference, source), "-o", output)
		if out, err := exec.CommandContext(ctx, compiler[0], args...).CombinedOutput(); err != nil {
			return Helpers{}, fmt.Errorf("build %s: %v: %s", source, err, out)
		}
	}
	return h, nil
}

// Run executes a mocked helper and returns the reports it wrote.
func Run(ctx context.Context, helper string, args, env []string) (Transcript, error) {
	file, err := os.CreateTemp("", "any2jb-parity-*")
	if err != nil {
		return nil, err
	}
	capture := file.Name()
	file.Close()
	defer os.Remove(capture)
	cmd := exec.CommandContext(ctx, helper, args...)
	cmd.Dir = filepath.Dir(capture)
	cmd.Env = append(append(os.Environ(), env...), "PARITY_CAPTURE="+capture)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%s %v: %v: %s", filepath.Base(helper), args, err, out)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		return nil, err
	}
	return ParseCapture(data)
}
