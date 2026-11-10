package transport_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/diagnostics"
	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

func helperFactory(t *testing.T, capture string) transport.HelperCommand {
	t.Helper()
	return func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestKeyboardHelperProcess$")
		cmd.Env = append(os.Environ(), "ANY2JB_KEYBOARD_FIXTURE=1", "ANY2JB_CAPTURE="+capture)
		return cmd
	}
}

func TestLiveKeyboardAcknowledgesReportsAndReleasesOnFocusLoss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports")
	d := transport.NewHelperDarwin(transport.DarwinOptions{Controller: "usb-drd0", Timing: transport.DefaultTiming()}, helperFactory(t, path))
	run := session.Run{ID: "live"}
	k, err := d.OpenKeyboard(context.Background(), run, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Send(context.Background(), [8]byte{2, 0, 4}); err != nil {
		t.Fatal(err)
	}
	if err := k.FocusLost(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{2, 0, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("reports %v, want %v", got, want)
	}
	if err := k.Send(context.Background(), [8]byte{0, 0, 5}); err == nil {
		t.Fatal("closed keyboard accepted input")
	}
}

func TestControllerLeaseExcludesAnotherTransportUntilRelease(t *testing.T) {
	support := t.TempDir()
	options := transport.DarwinOptions{Runtime: engine.Options{Support: support}, Controller: "usb-drd0", Timing: transport.DefaultTiming()}
	first := transport.NewHelperDarwin(options, helperFactory(t, filepath.Join(t.TempDir(), "one")))
	second := transport.NewHelperDarwin(options, helperFactory(t, filepath.Join(t.TempDir(), "two")))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	k, err := first.OpenKeyboard(ctx, session.Run{ID: "first"}, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { k.Close(ctx) })
	if other, err := second.OpenKeyboard(ctx, session.Run{ID: "second"}, func(session.Progress) {}); err == nil {
		other.Close(ctx)
		t.Fatal("two transports acquired the same controller")
	}
	if err := k.Close(ctx); err != nil {
		t.Fatal(err)
	}
	other, err := second.OpenKeyboard(ctx, session.Run{ID: "second"}, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLivePasteMatchesSmileBASICCaseAndDoesNotSubmit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports")
	d := transport.NewHelperDarwin(transport.DarwinOptions{Controller: "usb-drd0", Timing: transport.DefaultTiming()}, helperFactory(t, path))
	k, err := d.OpenKeyboard(context.Background(), session.Run{ID: "paste"}, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Paste(context.Background(), "Aa0)"); err != nil {
		t.Fatal(err)
	}
	if err := k.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		2, 0, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 39, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		2, 0, 39, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reports %v, want %v", got, want)
	}
}

func TestKeyboardHelperProcess(t *testing.T) {
	if os.Getenv("ANY2JB_KEYBOARD_FIXTURE") != "1" {
		return
	}
	f, err := os.Create(os.Getenv("ANY2JB_CAPTURE"))
	if err != nil {
		os.Exit(2)
	}
	defer f.Close()
	limit, _ := strconv.Atoi(os.Getenv("ANY2JB_ACK_LIMIT"))
	delay, _ := strconv.Atoi(os.Getenv("ANY2JB_ACK_DELAY_MS"))
	if path := os.Getenv("ANY2JB_SIGNAL_LOG"); path != "" {
		// Deliberately neither releases nor reports release, so any success
		// must come from the acknowledged neutral report.
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		go func() {
			<-signals
			os.WriteFile(path, []byte("sigterm"), 0600)
			os.Exit(0)
		}()
	}
	if os.Getenv("ANY2JB_NO_ACK") == "1" || limit > 0 {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		go func() {
			<-signals
			f.Write(make([]byte, 8))
			fmt.Println("MAC2JB_KEYBOARD_RELEASED")
			f.Close()
			os.Exit(0)
		}()
	}
	out := bufio.NewWriter(os.Stdout)
	fmt.Fprintln(out, "MAC2JB_KEYBOARD_READY")
	out.Flush()
	for count := 1; ; count++ {
		var report [8]byte
		_, err := io.ReadFull(os.Stdin, report[:])
		if err == io.EOF {
			if path := os.Getenv("ANY2JB_EOF_RELEASED"); path != "" {
				os.WriteFile(path, make([]byte, 8), 0600)
			}
			fmt.Fprintln(out, "MAC2JB_KEYBOARD_RELEASED")
			out.Flush()
			return
		}
		if err != nil {
			os.Exit(2)
		}
		f.Write(report[:])
		if os.Getenv("ANY2JB_NO_ACK") == "1" || limit > 0 && count > limit {
			continue
		}
		if count == 1 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
		fmt.Fprintf(out, "MAC2JB_KEYBOARD_SENT %d\n", count)
		out.Flush()
	}
}

func TestFocusLossInterruptsAnUnacknowledgedSend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports")
	factory := helperFactory(t, path)
	d := transport.NewHelperDarwin(transport.DarwinOptions{Controller: "usb-drd0", Timing: transport.DefaultTiming()}, func(name string, args ...string) *exec.Cmd {
		cmd := factory(name, args...)
		cmd.Env = append(cmd.Env, "ANY2JB_NO_ACK=1")
		return cmd
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	k, err := d.OpenKeyboard(ctx, session.Run{ID: "noack"}, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	sent := make(chan error, 1)
	go func() { sent <- k.Send(ctx, [8]byte{0, 0, 4}) }()
	for {
		data, _ := os.ReadFile(path)
		if len(data) >= 8 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("fixture did not receive report")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	k.FocusLost(ctx)
	select {
	case err := <-sent:
		if err == nil {
			t.Fatal("unacknowledged send succeeded")
		}
	case <-ctx.Done():
		t.Fatal("focus loss blocked behind a Send")
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) < 16 || !reflect.DeepEqual(data[len(data)-8:], make([]byte, 8)) {
		t.Fatalf("missing neutral release: %v, %v", data, err)
	}
}

func fixtureEnv(factory transport.HelperCommand, env ...string) transport.HelperCommand {
	return func(name string, args ...string) *exec.Cmd {
		cmd := factory(name, args...)
		cmd.Env = append(cmd.Env, env...)
		return cmd
	}
}

func TestCloseAcknowledgesNeutralBeforeAnySignalEvenAfterOpenContextEnds(t *testing.T) {
	dir := t.TempDir()
	path, signals := filepath.Join(dir, "reports"), filepath.Join(dir, "signals")
	d := transport.NewHelperDarwin(transport.DarwinOptions{Controller: "usb-drd0", Timing: transport.DefaultTiming()},
		fixtureEnv(helperFactory(t, path), "ANY2JB_SIGNAL_LOG="+signals))
	ctx, cancel := context.WithCancel(context.Background())
	k, err := d.OpenKeyboard(ctx, session.Run{ID: "orderly"}, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Send(ctx, [8]byte{0, 0, 4}); err != nil {
		t.Fatal(err)
	}
	cancel()
	time.Sleep(100 * time.Millisecond)
	cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()
	if err := k.Close(cleanup); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(signals); !os.IsNotExist(err) {
		t.Fatal("helper was signalled before or instead of an acknowledged neutral release")
	}
	got, err := os.ReadFile(path)
	if want := []byte{0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}; err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("reports %v, want %v", got, want)
	}
}

func TestLateAcknowledgementOfCancelledSendIsNotNeutralReleaseEvidence(t *testing.T) {
	support := t.TempDir()
	options := transport.DarwinOptions{Runtime: engine.Options{Support: support}, Controller: "usb-drd0", Timing: transport.DefaultTiming()}
	d := transport.NewHelperDarwin(options, fixtureEnv(helperFactory(t, filepath.Join(t.TempDir(), "reports")), "ANY2JB_ACK_DELAY_MS=300"))
	run := session.Run{ID: "late-ack"}
	k, err := d.OpenKeyboard(context.Background(), run, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := k.Send(ctx, [8]byte{0, 0, 4}); err == nil {
		t.Fatal("send outlived its context")
	}
	logs, _, err := (diagnostics.Store{Support: support}).Refresh(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.USB, "neutral_release_confirmed") || !strings.Contains(logs.USB, "delivered_reports=2") {
		t.Fatalf("neutral release was not separately acknowledged: %q", logs.USB)
	}
	if err := d.Release(context.Background(), run); err != nil {
		t.Fatal(err)
	}
}
