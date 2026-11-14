package transport_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/parity"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/transport"
)

var updateParity = flag.Bool("update-parity", false, "rewrite testdata/mac2jb-parity.json from the pinned mac2jb reference")

const parityDigests = "testdata/mac2jb-parity.json"

// mac2jb's fast preset for stroke timing, with distinct command waits so a
// swapped wait cannot pass.
var fastTiming = transport.Timing{GapMS: 5, HoldMS: 10, EnterMS: 100, EnumerationSeconds: 4, LoadMS: 222, RunMS: 333, RemoveMS: 444}

type hidCase struct {
	name      string
	run       session.Run
	action    session.InputAction
	timing    transport.Timing
	text      string // typed bootstrap; empty for commands
	reference bool   // text comes from the reference checkout and is never committed
}

// asciiText exercises every byte both encoders accept, inherited indentation
// in both directions, a blank line, CRLF and a missing final newline.
func asciiText() string {
	var b strings.Builder
	for c := byte(' '); c < 0x7f; c++ {
		b.WriteByte(c)
	}
	b.WriteString("\n    four\n  two\n\ttab\n      six\n\n one\nCRLF\r\nend")
	return b.String()
}

func profileRun(p manifest.Profile, edition string) session.Run {
	return session.Run{ID: "parity", Edition: edition, LANIPv4: "192.0.2.1", Profile: p.SessionProfile()}
}

func startCommand(run session.Run) string {
	args := []string{strconv.Itoa(run.Profile.StartPort), "192", "0", "2", "1"}
	if run.Profile.StartArgumentCount == 9 {
		args = append(args, "4660", "22136", "39612", "281474976710655")
	}
	return "V=START(" + strings.Join(args, ",") + ")"
}

func hidCases(t *testing.T, m *manifest.Manifest, reference string) []hidCase {
	t.Helper()
	ascii := profileRun(m.Profiles[0], "full")
	cases := []hidCase{
		{name: "bootstrap/ascii", run: ascii, action: session.Bootstrap, timing: transport.DefaultTiming(), text: asciiText()},
		{name: "bootstrap/ascii/fast", run: ascii, action: session.Bootstrap, timing: fastTiming, text: asciiText()},
	}
	typed := map[string]bool{}
	for _, p := range m.Profiles {
		for _, edition := range p.SessionProfile().Editions {
			run := profileRun(p, edition)
			for _, action := range []session.InputAction{session.Launch, session.Save} {
				name := fmt.Sprintf("%s/%s/%s", action, p.ID, edition)
				cases = append(cases, hidCase{name: name, run: run, action: action, timing: transport.DefaultTiming()},
					hidCase{name: name + "/fast", run: run, action: action, timing: fastTiming})
			}
		}
		if reference == "" || typed[p.BootstrapSHA256] {
			continue
		}
		typed[p.BootstrapSHA256] = true
		data, err := os.ReadFile(filepath.Join(reference, filepath.FromSlash(p.Bootstrap)))
		if err != nil {
			t.Fatal(err)
		}
		run := profileRun(p, "full")
		for _, timing := range []struct {
			suffix string
			value  transport.Timing
		}{{"", transport.DefaultTiming()}, {"/fast", fastTiming}} {
			cases = append(cases, hidCase{
				name: "bootstrap/" + p.BootstrapSHA256[:12] + timing.suffix, run: run,
				action: session.Bootstrap, timing: timing.value, text: string(data), reference: true,
			})
		}
	}
	return cases
}

// mac2jbArgs mirrors scripts/gui-host.cjs hid(): the GUI types bootstrap text
// with mac2jb-send and commands as one selected list with the host's START.
func mac2jbArgs(c hidCase, bootstrap string) (string, []string, []string) {
	env := []string{
		"MAC2JB_AUTO_WORKER=1", "MAC2JB_CONTROLLER=usb-drd0", "MAC2JB_ENUMERATION_TIMEOUT_SECONDS=4",
		"MAC2JB_KEY_HOLD_MS=" + strconv.Itoa(c.timing.HoldMS), "MAC2JB_ENTER_SETTLE_MS=" + strconv.Itoa(c.timing.EnterMS),
		"MAC2JB_POLL_INTERVAL=10",
	}
	if c.action == session.Bootstrap {
		return "sender", []string{bootstrap, "--delay-ms", strconv.Itoa(c.timing.GapMS)}, env
	}
	p := c.run.Profile
	args := []string{"--auto"}
	if c.run.Edition == "demo" {
		args = append(args, "--demo")
	}
	args = append(args, "--delay-ms", strconv.Itoa(c.timing.GapMS), "--gui-events", "--enumeration-timeout", "4")
	if p.StartArgumentCount == 9 {
		args = append(args, "--start-arity", "9")
	}
	args = append(args, "--start-port", strconv.Itoa(p.StartPort), "--save-name", p.SaveName, "--load-name", p.SaveName,
		"--load-wait-ms", strconv.Itoa(c.timing.LoadMS), "--run-wait-ms", strconv.Itoa(c.timing.RunMS),
		"--remove-wait-ms", strconv.Itoa(c.timing.RemoveMS), "--load-confirm-wait-ms", "0")
	if c.action == session.Save {
		return "commands", append(args, "--save-bootstrap"), env
	}
	return "commands", append(args, "--commands", "load,run,remove,start", "--start-command", startCommand(c.run)), env
}

// any2jbTranscript drives the production Darwin transport. The helper is either
// mac2jb's keyboard.c with mocked USB (capture lines) or the Go fixture (raw
// reports). Each keyboard wait advances a virtual clock instead of sleeping;
// every report written before a wait is already acknowledged.
func any2jbTranscript(t *testing.T, c hidCase, keyboard string) parity.Transcript {
	t.Helper()
	capture := filepath.Join(t.TempDir(), "capture")
	if err := os.WriteFile(capture, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	factory, width := helperFactory(t, capture), int64(8)
	if keyboard != "" {
		width = int64(len("0 0000000000000000\n"))
		factory = func(string, ...string) *exec.Cmd {
			cmd := exec.Command(keyboard)
			cmd.Env = append(os.Environ(), "PARITY_CAPTURE="+capture)
			return cmd
		}
	}
	type wait struct {
		after int
		ns    uint64
	}
	var waits []wait
	defer transport.SetSleep(func(ctx context.Context, ms int) error {
		info, err := os.Stat(capture)
		if err != nil || info.Size()%width != 0 {
			return fmt.Errorf("capture: %v", err)
		}
		waits = append(waits, wait{int(info.Size() / width), uint64(ms) * uint64(time.Millisecond)})
		return ctx.Err()
	})()
	run := c.run
	sum := sha256.Sum256([]byte(c.text))
	run.Profile.BootstrapSHA256 = hex.EncodeToString(sum[:])
	options := transport.DarwinOptions{
		Controller: "usb-drd0", Timing: c.timing,
		Bootstrap: func(session.Run) (string, error) { return c.text, nil },
		Confirm:   func(context.Context, session.Run, transport.ConsoleStep) error { return nil },
	}
	host := session.PreparedHost{Listening: true, PairingValid: true, Opaque: engine.PairingDetails{RunID: run.ID, Command: startCommand(run)}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := transport.NewHelperDarwin(options, factory).Send(ctx, run, c.action, host, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var reports parity.Transcript
	if keyboard != "" {
		if reports, err = parity.ParseCapture(data); err != nil {
			t.Fatal(err)
		}
	} else {
		for i := 0; i+8 <= len(data); i += 8 {
			var r parity.Report
			copy(r.Bytes[:], data[i:])
			reports = append(reports, r)
		}
	}
	var now uint64
	for i := range reports {
		if reports[i].AtNS != 0 {
			t.Fatal("keyboard.c must forward reports without its own waits")
		}
		for len(waits) > 0 && waits[0].after <= i {
			now += waits[0].ns
			waits = waits[1:]
		}
		reports[i].AtNS = now
	}
	return reports
}

type parityFile struct {
	Note    string            `json:"note"`
	Digests map[string]string `json:"digests"`
}

func readParity(t *testing.T) parityFile {
	t.Helper()
	var f parityFile
	data, err := os.ReadFile(parityDigests)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err == nil {
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// TestHIDOutputMatchesMac2jb compares the key state any2jb sends with the
// pinned mac2jb GUI helpers for every profile, edition and action. With
// ANY2JB_REFERENCE_ROOT the mac2jb helpers are compiled with only USB and the
// clock mocked and act as the oracle; any2jb's reports pass through the same
// keyboard.c. Without it, any2jb is compared with the committed digests.
func TestHIDOutputMatchesMac2jb(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Darwin keyboard transport does not run on Windows")
	}
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	reference := os.Getenv("ANY2JB_REFERENCE_ROOT")
	committed := readParity(t)
	var helpers parity.Helpers
	if reference != "" {
		if helpers, err = parity.BuildHelpers(context.Background(), reference, t.TempDir()); err != nil {
			t.Fatal(err)
		}
	} else if *updateParity {
		t.Fatal("-update-parity requires ANY2JB_REFERENCE_ROOT")
	}
	oracle := map[string]string{}
	for _, c := range hidCases(t, m, reference) {
		t.Run(c.name, func(t *testing.T) {
			if reference == "" {
				got := any2jbTranscript(t, c, "").State().Digest()
				if want := committed.Digests["hid/"+c.name]; got != want {
					t.Fatalf("any2jb digest %s, committed mac2jb digest %q", got, want)
				}
				return
			}
			bootstrap := filepath.Join(t.TempDir(), "bootstrap.bas")
			if err := os.WriteFile(bootstrap, []byte(c.text), 0o600); err != nil {
				t.Fatal(err)
			}
			helper, args, env := mac2jbArgs(c, bootstrap)
			path := map[string]string{"sender": helpers.Sender, "commands": helpers.Commands}[helper]
			want, err := parity.Run(context.Background(), path, args, env)
			if err != nil {
				t.Fatal(err)
			}
			got := any2jbTranscript(t, c, helpers.Keyboard)
			if diff := parity.Diff(want.State(), got.State()); diff != "" {
				t.Fatal(diff)
			}
			t.Logf("%d state changes; raw reports mac2jb %d, any2jb %d", len(want.State()), len(want), len(got))
			oracle["hid/"+c.name] = want.State().Digest()
		})
	}
	if reference != "" {
		checkDigests(t, "hid/", committed, oracle)
	}
}

// checkDigests detects drift: the reference oracle must still produce the
// committed digests unless -update-parity rewrites them.
func checkDigests(t *testing.T, prefix string, committed parityFile, oracle map[string]string) {
	t.Helper()
	if t.Failed() {
		return
	}
	if *updateParity {
		f := readParity(t)
		if f.Digests == nil {
			f.Digests = map[string]string{}
		}
		for name := range f.Digests {
			if strings.HasPrefix(name, prefix) {
				delete(f.Digests, name)
			}
		}
		for name, digest := range oracle {
			f.Digests[name] = digest
		}
		f.Note = "SHA-256 of normalized mac2jb reference transcripts; see docs/mac2jb-parity.md. Regenerate with -update-parity."
		data, err := json.MarshalIndent(f, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(parityDigests), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(parityDigests, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	var names []string
	for name := range committed.Digests {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	for name := range oracle {
		names = append(names, name)
	}
	sort.Strings(names)
	for i, name := range names {
		if i > 0 && names[i-1] == name {
			continue
		}
		if committed.Digests[name] != oracle[name] {
			t.Errorf("%s: mac2jb reference digest %q, committed %q; review the change, then rerun with -update-parity", name, oracle[name], committed.Digests[name])
		}
	}
}

// TestHIDParityDetectsDifferences guards the comparison itself: one changed
// millisecond or key must fail it.
func TestHIDParityDetectsDifferences(t *testing.T) {
	reference := os.Getenv("ANY2JB_REFERENCE_ROOT")
	if reference == "" || runtime.GOOS == "windows" {
		t.Skip("needs ANY2JB_REFERENCE_ROOT and the compiled mac2jb helpers")
	}
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	helpers, err := parity.BuildHelpers(context.Background(), reference, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, _ := m.Profile("kurumuz")
	c := hidCase{run: profileRun(p, "full"), action: session.Launch, timing: fastTiming}
	_, args, env := mac2jbArgs(c, "")
	want, err := parity.Run(context.Background(), helpers.Commands, args, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(want.State()) < 100 {
		t.Fatalf("implausibly short launch transcript: %d state changes", len(want.State()))
	}
	slower := c
	slower.timing.RunMS++
	if parity.Diff(want.State(), any2jbTranscript(t, slower, helpers.Keyboard).State()) == "" {
		t.Fatal("a 1 ms wait difference was not detected")
	}
	other := c
	other.run.Profile.SaveName = "NS3"
	if parity.Diff(want.State(), any2jbTranscript(t, other, helpers.Keyboard).State()) == "" {
		t.Fatal("a different program name was not detected")
	}
}

// TestEncoderAcceptsExactlyMac2jbBytes checks all 256 byte values against
// mac2jb-send's own validation.
func TestEncoderAcceptsExactlyMac2jbBytes(t *testing.T) {
	reference := os.Getenv("ANY2JB_REFERENCE_ROOT")
	if reference == "" || runtime.GOOS == "windows" {
		t.Skip("needs ANY2JB_REFERENCE_ROOT and the compiled mac2jb helpers")
	}
	helpers, err := parity.BuildHelpers(context.Background(), reference, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "byte")
	for b := 0; b < 256; b++ {
		if err := os.WriteFile(file, []byte{byte(b)}, 0o600); err != nil {
			t.Fatal(err)
		}
		mac2jb := exec.Command(helpers.Sender, file, "--dry-run").Run() == nil
		_, err := transport.EncodeText(string([]byte{byte(b)}))
		if mac2jb != (err == nil) {
			t.Errorf("byte 0x%02x: mac2jb accepts %v, any2jb accepts %v", b, mac2jb, err == nil)
		}
	}
}

// manualPrompts runs the guided transport, acknowledging each prompt at once.
func manualPrompts(t *testing.T, run session.Run, action session.InputAction, text string) []transport.Prompt {
	t.Helper()
	m := transport.NewManual(func(session.Run) (string, error) { return text, nil })
	run.Transport.Capabilities.Manual = true
	host := session.PreparedHost{Listening: true, PairingValid: true, Opaque: engine.PairingDetails{RunID: run.ID, Command: startCommand(run)}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.Send(ctx, run, action, host, func(session.Progress) {}) }()
	var prompts []transport.Prompt
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			return prompts
		case <-m.Changes():
			if p := m.Prompt(); p.Sequence != 0 && m.Acknowledge(p.RunID, p.Sequence) {
				prompts = append(prompts, p)
			}
		case <-ctx.Done():
			t.Fatal("guided transport did not finish")
		}
	}
}

// TestGuidedTextMatchesMac2jb compares what the guided transport tells the user
// to type with mac2jb-commands --dry-run for the same GUI arguments, and the
// bootstrap prompt with the exact bytes mac2jb-send types.
func TestGuidedTextMatchesMac2jb(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	reference := os.Getenv("ANY2JB_REFERENCE_ROOT")
	committed := readParity(t)
	var helpers parity.Helpers
	if reference != "" && runtime.GOOS != "windows" {
		if helpers, err = parity.BuildHelpers(context.Background(), reference, t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
	oracle := map[string]string{}
	for _, c := range hidCases(t, m, reference) {
		if strings.HasSuffix(c.name, "/fast") {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			var got []string
			for _, p := range manualPrompts(t, c.run, c.action, c.text) {
				got = append(got, p.Kind+": "+p.Text)
			}
			sum := sha256.Sum256([]byte(strings.Join(got, "\n")))
			digest := hex.EncodeToString(sum[:])
			if helpers.Commands == "" {
				if want := committed.Digests["guided/"+c.name]; digest != want {
					t.Fatalf("any2jb digest %s, committed mac2jb digest %q", digest, want)
				}
				return
			}
			var want []string
			if c.action == session.Bootstrap {
				want = []string{"slot: ", "bootstrap: " + c.text, "direct: "}
			} else {
				_, args, _ := mac2jbArgs(c, "")
				out, err := exec.Command(helpers.Commands, append(args, "--dry-run")...).CombinedOutput()
				if err != nil {
					t.Fatalf("%v: %s", err, out)
				}
				kinds := []string{"load", "run", "remove", "start"}
				if c.action == session.Save {
					kinds = []string{"save"}
				}
				for _, line := range strings.Split(string(out), "\n") {
					number, command, ok := strings.Cut(line, ": ")
					position, err := strconv.Atoi(number)
					if ok && err == nil && strings.HasSuffix(command, " [Enter]") {
						want = append(want, kinds[position-1]+": "+strings.TrimSuffix(command, " [Enter]"))
					}
					// The additional LOAD Enter is its own guided step.
					if strings.HasPrefix(line, "LOAD receives one additional Enter") || strings.HasPrefix(line, "LOADV receives one additional Enter") {
						want = append(want[:1], append([]string{"load-confirm: "}, want[1:]...)...)
					}
				}
			}
			if strings.Join(want, "\n") != strings.Join(got, "\n") {
				t.Fatalf("mac2jb %q\nany2jb %q", want, got)
			}
			oracle["guided/"+c.name] = digest
		})
	}
	if helpers.Commands != "" {
		checkDigests(t, "guided/", committed, oracle)
	}
}
