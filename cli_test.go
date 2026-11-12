package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/sessionlock"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
	"github.com/thedavidweng/any2jb/transport"
)

func fixtureCLI(t *testing.T, input string) (*commandLine, *bytes.Buffer) {
	return fixtureCLIWith(t, sessiontest.NewFakeEnvironment(), input)
}

func fixtureCLIWith(t *testing.T, f *sessiontest.FakeEnvironment, input string) (*commandLine, *bytes.Buffer) {
	t.Helper()
	t.Setenv("ANY2JB_DEVELOPER_CONFIG", "")
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := a.manifest.Profile(a.preferences.Profile)
	a.controller = session.New(f.Environment(), p.SessionProfile())
	out := &bytes.Buffer{}
	c := newCommandLine(strings.NewReader(input), out, out, false)
	c.app, c.support = a, t.TempDir()
	c.signals = make(chan os.Signal, 1)
	t.Cleanup(c.close)
	return c, out
}

func TestCLIStatusOneShotJSON(t *testing.T) {
	c, out := fixtureCLI(t, "")
	if err := c.execute([]string{"status", "--json"}); err != nil {
		t.Fatal(err)
	}
	var s session.Snapshot
	if err := json.Unmarshal(out.Bytes(), &s); err != nil || s.Stage != session.Idle {
		t.Fatalf("status: %v %s", err, out)
	}
}

func oneCLIJSONObject(t *testing.T, out *bytes.Buffer) map[string]any {
	t.Helper()
	d := json.NewDecoder(out)
	var object map[string]any
	if err := d.Decode(&object); err != nil || object == nil {
		t.Fatalf("expected one JSON object: %v %s", err, out)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		t.Fatalf("extra stdout after JSON object: %v %v", extra, err)
	}
	return object
}

func TestCLIJSONRecoveryFailure(t *testing.T) {
	c, out := fixtureCLI(t, "")
	c.stderr = io.Discard
	c.app.controller.MarkInterrupted()
	err := c.execute([]string{"detect", "--json"})
	if cliExitCode(err) != exitIndeterminate {
		t.Fatalf("expected recovery exit: %v", err)
	}
	object := oneCLIJSONObject(t, out)
	if object["code"] != "recovery_required" || object["exit"] != float64(4) {
		t.Fatalf("recovery failure: %v", object)
	}
	recovery, ok := object["recovery"].(map[string]any)
	if !ok || recovery["required"] != true || recovery["stage"] != "indeterminate_console_restart_required" || object["next"] == "" {
		t.Fatalf("missing recovery instruction: %v", object)
	}
}

func TestCLIJSONBlockedAndErrorFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		exit int
		code string
	}{
		{"live command", []string{"launch", "--json"}, 3, "blocked"},
		{"invalid language", []string{"status", "--lang=invalid", "--json"}, 1, "error"},
		{"bad argument", []string{"status", "extra", "--json"}, 1, "error"},
		{"unknown command", []string{"not-a-command", "--json"}, 1, "error"},
		{"unknown flag", []string{"status", "--unknown", "--json"}, 1, "error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, out := fixtureCLI(t, "")
			c.stderr = io.Discard
			err := c.execute(test.args)
			if cliExitCode(err) != test.exit {
				t.Fatalf("unexpected exit: %v", err)
			}
			object := oneCLIJSONObject(t, out)
			if object["code"] != test.code || object["exit"] != float64(test.exit) || object["message"] == "" {
				t.Fatalf("failure envelope: %v", object)
			}
		})
	}
}

func TestCLIJSONDetectNeedsSetupIsOneObject(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	f.Checks = []session.Check{{ID: "runtime", State: session.RequiresAttention, Message: "Fixture requires setup"}}
	c, out := fixtureCLIWith(t, f, "")
	c.stderr = io.Discard
	if err := c.execute([]string{"detect", "--json"}); cliExitCode(err) != 3 {
		t.Fatalf("expected setup block: %v", err)
	}
	object := oneCLIJSONObject(t, out)
	if object["code"] != "blocked" || object["Stage"] != "needs_setup" {
		t.Fatalf("expected status and block together: %v", object)
	}
}

func TestCLIJSONOneShotResultsAreObjects(t *testing.T) {
	for _, line := range []string{
		"status", "detect", "settings get", "settings profiles", "settings developer", "settings about",
		"language", "usb-defaults", "tool list", "tool state", "discover", "launcher list",
		"savekit preview delete --file fixture", "diagnostics logs", "diagnostics export", "diagnostics folders",
		"diagnostics cleanup", "civswitch availability", "driver status", "--help", "settings --help",
	} {
		t.Run(line, func(t *testing.T) {
			c, out := fixtureCLI(t, "")
			c.stderr = io.Discard
			c.execute(append(strings.Fields(line), "--json"))
			oneCLIJSONObject(t, out)
		})
	}
}

func TestCLIJSONConsentFailureKeepsPreviewInOneObject(t *testing.T) {
	c, out := fixtureCLI(t, "savekit delete --file fixture\n")
	c.stderr = io.Discard
	if err := c.execute([]string{"session", "--json"}); cliExitCode(err) != 2 {
		t.Fatalf("expected missing consent: %v", err)
	}
	object := oneCLIJSONObject(t, out)
	if object["code"] != "cancelled" || object["exit"] != float64(2) {
		t.Fatalf("expected consent failure: %v", object)
	}
}

func TestCLIRecoveryRequiredHasItsOwnExitCode(t *testing.T) {
	c, out := fixtureCLI(t, "launch\n")
	c.app.controller.MarkInterrupted()
	err := c.execute([]string{"session"})
	if code := cliExitCode(err); code != exitIndeterminate {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(err.Error(), "restart-confirm --confirm=console-restarted") {
		t.Fatalf("no recovery instruction: %v", err)
	}
}

func TestCLIDetectOneShotJSON(t *testing.T) {
	c, out := fixtureCLI(t, "")
	if err := c.execute([]string{"detect", "--json"}); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !strings.Contains(out.String(), `"Stage":"ready"`) {
		t.Fatalf("detect did not report readiness as JSON: %s", out)
	}
}

func TestCLILiveCommandNeedsSession(t *testing.T) {
	c, _ := fixtureCLI(t, "")
	if code := cliExitCode(c.execute([]string{"launch"})); code != exitBlocked {
		t.Fatalf("one-shot launch exit %d", code)
	}
}

func TestCLIScriptStopsAtFirstBlockedStep(t *testing.T) {
	c, out := fixtureCLI(t, "launch\nsettings get\n")
	err := c.execute([]string{"session"})
	if cliExitCode(err) != exitBlocked {
		t.Fatalf("want readiness exit, got %v: %s", err, out)
	}
	if strings.Contains(out.String(), `"transport"`) {
		t.Fatal("script ran after blocked launch")
	}
}

func TestCLIScriptLaunchesAndFinishesWithPerActionConsent(t *testing.T) {
	script := "detect\nuse-saved --confirm=matching-bootstrap\nlaunch\nfinish\n"
	c, out := fixtureCLI(t, script)
	if err := c.execute([]string{"session"}); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if s := c.app.controller.Snapshot(); s.Stage != session.Finished {
		t.Fatalf("stage %s: %s", s.Stage, out)
	}
}

func TestCLINonTerminalConsentNeedsItsOwnFlag(t *testing.T) {
	for _, script := range []string{"detect\nuse-saved\n", "detect\nuse-saved --confirm=run-user-code\n"} {
		c, out := fixtureCLI(t, script)
		if code := cliExitCode(c.execute([]string{"session"})); code != exitCancelled {
			t.Fatalf("%q exit %d: %s", script, code, out)
		}
		if !strings.Contains(out.String(), "The app cannot read saved program contents") {
			t.Fatalf("risk text missing: %s", out)
		}
		if c.app.controller.Snapshot().BootstrapSaved {
			t.Fatal("bootstrap accepted without its confirmation")
		}
	}
}

func TestCLITerminalConsentPrompts(t *testing.T) {
	c, out := fixtureCLI(t, "detect\nuse-saved\ny\n")
	c.terminal = true
	if err := c.execute([]string{"session"}); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !strings.Contains(out.String(), "[y/N]") || !c.app.controller.Snapshot().BootstrapSaved {
		t.Fatalf("terminal prompt did not authorize: %s", out)
	}
}

func TestCLIScriptFileRequiresFlagsEvenFromATerminal(t *testing.T) {
	c, out := fixtureCLI(t, "y\n")
	c.terminal = true
	code := cliExitCode(c.execute([]string{"session", "--script", writeScript(t, "detect\nuse-saved\n")}))
	if code != exitCancelled || c.app.controller.Snapshot().BootstrapSaved {
		t.Fatalf("script consent came from the terminal: exit %d: %s", code, out)
	}
}

func writeScript(t *testing.T, body string) string {
	path := filepath.Join(t.TempDir(), "steps.txt")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCLIInterruptBeforeConsoleCancels(t *testing.T) {
	gate := make(chan struct{})
	f := sessiontest.NewFakeEnvironment()
	f.PrepareGate = gate
	c, out := fixtureCLIWith(t, f, "detect\nuse-saved --confirm=matching-bootstrap\nlaunch\n")
	code := interruptAt(t, c, session.PreparingHost)
	if code != exitCancelled || c.app.controller.Snapshot().Stage != session.Ready {
		t.Fatalf("exit %d stage %s: %s", code, c.app.controller.Snapshot().Stage, out)
	}
}

func TestCLIInterruptAfterConsoleStops(t *testing.T) {
	gate := make(chan struct{})
	f := sessiontest.NewFakeEnvironment()
	f.InputGate = gate
	c, out := fixtureCLIWith(t, f, "detect\nuse-saved --confirm=matching-bootstrap\nlaunch\n")
	code := interruptAt(t, c, session.WaitingForConsole)
	if code != exitIndeterminate || c.app.controller.Snapshot().Stage != session.Indeterminate {
		t.Fatalf("exit %d stage %s: %s", code, c.app.controller.Snapshot().Stage, out)
	}
	if !strings.Contains(strings.Join(f.Trace(), " "), "input.release") {
		t.Fatal("stop did not release input")
	}
}

func interruptAt(t *testing.T, c *commandLine, stage session.Stage) int {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- c.execute([]string{"session"}) }()
	deadline := time.After(5 * time.Second)
	for c.app.controller.Snapshot().Stage != stage {
		select {
		case <-deadline:
			t.Fatalf("never reached %s", stage)
		case <-time.After(5 * time.Millisecond):
		}
	}
	c.signals <- os.Interrupt
	select {
	case err := <-done:
		return cliExitCode(err)
	case <-time.After(10 * time.Second):
		t.Fatal("interrupt did not end the session")
		return 0
	}
}

func TestCLISessionExcludesAnotherOwner(t *testing.T) {
	c, _ := fixtureCLI(t, "")
	release, err := sessionlock.Acquire(c.support)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if code := cliExitCode(c.execute([]string{"session"})); code != exitBlocked {
		t.Fatalf("second owner exit %d", code)
	}
}

func TestCLIArguments(t *testing.T) {
	for line, want := range map[string][]string{
		`tool run custom-homebrew --custom-folder "/tmp/my app"`: {"tool", "run", "custom-homebrew", "--custom-folder", "/tmp/my app"},
		`["savekit","write","--source","/a b"]`:                  {"savekit", "write", "--source", "/a b"},
		`ack --confirm=console-step`:                             {"ack", "--confirm=console-step"},
	} {
		got, err := cliArguments(line)
		if err != nil || strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s: %q %v", line, got, err)
		}
	}
	if _, err := cliArguments(`ack "open`); err == nil {
		t.Error("unterminated quote accepted")
	}
}

func TestCLIGuidedManualBootstrapSetup(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	env := f.Environment()
	manual := transport.NewManual(func(session.Run) (string, error) { return "PRINT \"FIXTURE\"", nil })
	env.Input = manual
	c, out := fixtureCLI(t, "")
	p, _ := c.app.manifest.Profile(c.app.preferences.Profile)
	c.app.controller, c.app.manual = session.New(env, p.SessionProfile()), manual
	ack := "ack --confirm=console-step\n"
	c.in = strings.NewReader("detect\nprepare\n" + ack + ack + ack +
		"save --consent=replace-bootstrap\n" + ack + "confirm-save --confirm=console-saved\n")
	if err := c.execute([]string{"session"}); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !c.app.controller.Snapshot().BootstrapSaved || !strings.Contains(out.String(), `PRINT "FIXTURE"`) {
		t.Fatalf("guided setup incomplete: %s", out)
	}
	if _, err := os.Stat(c.app.bootstrapPath()); err != nil {
		t.Fatal("confirmed bootstrap identity was not persisted")
	}
}
