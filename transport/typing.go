package transport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/session"
)

func (d *Darwin) send(ctx context.Context, run session.Run, action session.InputAction, host session.PreparedHost, report session.Report) error {
	if action == session.Bootstrap {
		return d.bootstrap(ctx, run, report)
	}
	if d.options.Confirm == nil {
		return errors.New("USB commands require explicit console acknowledgement")
	}
	var commands []string
	switch action {
	case session.Save:
		if !programName(run.Profile.SaveName) {
			return errors.New("invalid bootstrap save name")
		}
		command := "SAVE \"" + run.Profile.SaveName + "\""
		if run.Edition == "demo" {
			command = "PRGEDIT 0:C$=\"\":FOR I=0 TO PRGSIZE(0)-1:C$=C$+PRGGET$() :NEXT:SAVEV \"TXT:" + run.Profile.SaveName + "\",C$"
		}
		commands = []string{command}
	case session.Launch:
		p, err := engine.Pairing(host)
		if err != nil || p.RunID != run.ID {
			return errors.New("USB command lacks matching host pairing")
		}
		if !programName(run.Profile.SaveName) {
			return errors.New("invalid bootstrap save name")
		}
		load := "LOAD \"TXT:" + run.Profile.SaveName + "\""
		if run.Edition == "demo" {
			load = "PRGEDIT 0:LOADV \"TXT:" + run.Profile.SaveName + "\" OUT C$:PRGSET C$"
		}
		commands = []string{load, "RUN", "REMOVE NSR,&H60000000,&H3FFFFFFF", p.Command}
	default:
		return errors.New("unsupported keyboard action")
	}
	// Validate all text before opening USB. Pairing text is never reported.
	for _, text := range commands {
		if _, err := encodeText(text + "\n"); err != nil {
			return err
		}
	}
	k, err := d.OpenKeyboard(ctx, run, report)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		k.Close(cleanup)
	}()
	for i, text := range commands {
		label := "SAVE"
		if action == session.Launch {
			label = []string{"LOAD", "RUN", "REMOVE", "START"}[i]
		}
		if err := d.options.Confirm(ctx, run, "Observe the console in Direct mode before "+label); err != nil {
			return err
		}
		report(session.Progress{Current: i, Total: len(commands), SideEffect: true})
		if err := d.typeText(ctx, k, text+"\n"); err != nil {
			return err
		}
		if action == session.Launch && i == 0 {
			// Upstream commands.c:241-246 auto-confirms after a fixed wait.
			// Keep the same key sequence, but require an observed prompt first.
			if err := d.options.Confirm(ctx, run, "Observe the LOAD YES / LOADV OK prompt before confirming"); err != nil {
				return err
			}
			if err := d.typeText(ctx, k, "\n"); err != nil {
				return err
			}
		}
		report(session.Progress{Current: i + 1, Total: len(commands), SideEffect: true})
		if action == session.Launch && i < 3 {
			wait := []int{d.options.Timing.LoadMS, d.options.Timing.RunMS, d.options.Timing.RemoveMS}[i]
			if err := pause(ctx, wait); err != nil {
				return err
			}
		}
	}
	return k.Close(ctx)
}

func programName(s string) bool {
	if len(s) < 1 || len(s) > 16 {
		return false
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func (d *Darwin) bootstrap(ctx context.Context, run session.Run, report session.Report) (err error) {
	if d.options.Bootstrap == nil {
		return errors.New("matching bootstrap loader unavailable")
	}
	text, err := d.options.Bootstrap(run)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(text))
	if hex.EncodeToString(sum[:]) != run.Profile.BootstrapSHA256 {
		return errors.New("profile/edition bootstrap digest mismatch")
	}
	if _, err := encodeText(text); err != nil {
		return err
	}
	f, err := os.CreateTemp("", "any2jb-bootstrap-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	p, err := d.start(ctx, run, "sender", f.Name(), "--delay-ms", strconv.Itoa(d.options.Timing.GapMS))
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err = errors.Join(err, p.close(cleanup))
	}()
	report(session.Progress{Processes: []session.Process{{PID: p.cmd.Process.Pid, Name: "sender"}}})
	completed, total := 0, 0
	for {
		line, err := p.next(ctx)
		if err == io.EOF {
			if total == 0 || completed != total {
				return errors.New("bootstrap ended without complete line evidence")
			}
			return nil
		}
		if err != nil {
			return err
		}
		if !strings.HasPrefix(line, "MAC2JB_GUI ") {
			continue
		}
		var event struct {
			Type        string `json:"type"`
			Line        int    `json:"line"`
			Completed   int    `json:"completed"`
			Total       int    `json:"total"`
			RemainingMS *int   `json:"remainingMs"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "MAC2JB_GUI ")), &event); err != nil {
			return errors.New("invalid bootstrap progress")
		}
		if event.Total < 1 || total != 0 && total != event.Total || event.Line != completed+1 ||
			event.RemainingMS != nil && *event.RemainingMS < 0 {
			return errors.New("invalid bootstrap progress order")
		}
		total = event.Total
		switch event.Type {
		case "line-start":
			if event.Completed != completed {
				return errors.New("invalid line start")
			}
		case "line-complete":
			if event.Completed != completed+1 || event.Completed > total {
				return errors.New("invalid line completion")
			}
			completed = event.Completed
		default:
			return errors.New("unknown bootstrap progress")
		}
		report(session.Progress{Current: completed, Total: total, SideEffect: true, Remaining: remaining(event.RemainingMS), HasEstimate: event.RemainingMS != nil})
	}
}
func remaining(ms *int) time.Duration {
	if ms == nil {
		return -1
	}
	return time.Duration(*ms) * time.Millisecond
}

func pause(ctx context.Context, ms int) error {
	if ms == 0 {
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

func (d *Darwin) typeText(ctx context.Context, k *Keyboard, text string) error {
	reports, err := encodeText(text)
	if err != nil {
		return err
	}
	for _, report := range reports {
		if err := k.Send(ctx, report); err != nil {
			return err
		}
		if err := pause(ctx, d.options.Timing.HoldMS); err != nil {
			return err
		}
		if err := k.Send(ctx, [8]byte{}); err != nil {
			return err
		}
		if err := pause(ctx, d.options.Timing.GapMS); err != nil {
			return err
		}
		if report[2] == 40 {
			if err := pause(ctx, d.options.Timing.EnterMS); err != nil {
				return err
			}
		}
	}
	return nil
}

// US keyboard mapping from sender.c encode, including SmileBASIC's inverted
// letter case. CRLF is one Enter; a bare CR is unsupported upstream.
func encodeText(text string) ([][8]byte, error) {
	var reports [][8]byte
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == '\r' && i+1 < len(text) && text[i+1] == '\n' {
			continue
		}
		var key, mods byte
		switch {
		case c >= 'a' && c <= 'z':
			key = c - 'a' + 4
			mods = 2
		case c >= 'A' && c <= 'Z':
			key = c - 'A' + 4
		case c >= '1' && c <= '9':
			key = c - '1' + 30
		case c == '0':
			key = 39
		default:
			if pos := strings.IndexByte("!@#$%^&*()", c); pos >= 0 {
				key = byte(30 + pos)
				if pos == 9 {
					key = 39
				}
				mods = 2
			} else if pos := strings.IndexByte("-=[]\\;'`,./", c); pos >= 0 {
				key = []byte{45, 46, 47, 48, 49, 51, 52, 53, 54, 55, 56}[pos]
			} else if pos := strings.IndexByte("_+{}|:\"~<>?", c); pos >= 0 {
				key = []byte{45, 46, 47, 48, 49, 51, 52, 53, 54, 55, 56}[pos]
				mods = 2
			} else {
				switch c {
				case ' ':
					key = 44
				case '\t':
					key = 43
				case '\n':
					key = 40
				default:
					return nil, errors.New("text requires printable US ASCII, tabs or line breaks")
				}
			}
		}
		reports = append(reports, [8]byte{mods, 0, key})
	}
	return reports, nil
}
