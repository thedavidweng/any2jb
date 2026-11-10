package transport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/session"
)

func (d *Darwin) send(ctx context.Context, run session.Run, action session.InputAction, host session.PreparedHost, report session.Report) (err error) {
	if d.options.Confirm == nil {
		return errors.New("USB input requires explicit console acknowledgement")
	}
	if action == session.Bootstrap {
		return d.bootstrap(ctx, run, report)
	}
	var commands []string
	workspace := ""
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
		p, err := engine.PairingFor(host, run)
		if err != nil {
			return errors.New("USB command lacks matching host pairing")
		}
		workspace = p.Workspace
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
	k, err := d.openKeyboard(ctx, run, report, workspace)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err = errors.Join(err, k.Close(cleanup))
	}()
	for i, text := range commands {
		step := SaveCommand
		if action == session.Launch {
			step = []ConsoleStep{LoadCommand, RunCommand, RemoveCommand, StartCommand}[i]
		}
		if err := d.options.Confirm(ctx, run, step); err != nil {
			return err
		}
		report(session.Progress{Current: i, Total: len(commands), SideEffect: true})
		if err := d.typeText(ctx, k, text+"\n"); err != nil {
			return err
		}
		if action == session.Launch && i == 0 {
			// Upstream commands.c:241-246 auto-confirms after a fixed wait.
			// Keep the same key sequence, but require an observed prompt first.
			if err := d.options.Confirm(ctx, run, LoadPrompt); err != nil {
				return err
			}
			if err := d.typeText(ctx, k, "\n"); err != nil {
				return err
			}
		}
		report(session.Progress{Current: i + 1, Total: len(commands), SideEffect: true})
		if err := k.p.audit.event("command_complete", fmt.Sprintf("completed=%d total=%d", i+1, len(commands))); err != nil {
			return err
		}
		if action == session.Launch && i < 3 {
			_, timing := d.settings()
			wait := []int{timing.LoadMS, timing.RunMS, timing.RemoveMS}[i]
			if err := pause(ctx, wait); err != nil {
				return err
			}
		}
	}
	return nil
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
	plan, total, err := bootstrapPlan(text)
	if err != nil {
		return err
	}
	if err := d.options.Confirm(ctx, run, EditorFocus); err != nil {
		return err
	}
	k, err := d.OpenKeyboard(ctx, run, report)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err = errors.Join(err, k.Close(cleanup))
	}()
	report(session.Progress{Total: len(plan), SideEffect: true})
	started, sent := time.Now(), 0
	for i, line := range plan {
		for _, stroke := range line {
			if err := d.stroke(ctx, k, stroke); err != nil {
				return err
			}
			sent++
		}
		left := time.Duration(0)
		if sent > 0 {
			left = time.Since(started) / time.Duration(sent) * time.Duration(total-sent)
		}
		report(session.Progress{Current: i + 1, Total: len(plan), SideEffect: true,
			Remaining: left, HasEstimate: sent > 0})
		if err := k.p.auditLine(i+1, len(plan), left); err != nil {
			return err
		}
	}
	return nil
}

// Match sender.c's inherited-space editor correction. Keyboard SENT events
// acknowledge every down/up report, so line progress never advances on a timer.
func bootstrapPlan(text string) ([][][8]byte, int, error) {
	if text == "" {
		return nil, 0, errors.New("bootstrap is empty")
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	plan, total, inherited := make([][][8]byte, 0, len(lines)), 0, 0
	for _, line := range lines {
		desired := len(line) - len(strings.TrimLeft(line, " "))
		var strokes [][8]byte
		for inherited > desired {
			strokes = append(strokes, [8]byte{0, 0, 42})
			inherited--
		}
		for inherited < desired {
			strokes = append(strokes, [8]byte{0, 0, 44})
			inherited++
		}
		body, err := encodeText(line[desired:])
		if err != nil {
			return nil, 0, err
		}
		strokes = append(strokes, body...)
		total += len(strokes)
		plan = append(plan, strokes)
	}
	return plan, total, nil
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
		if err := d.stroke(ctx, k, report); err != nil {
			return err
		}
	}
	return nil
}

func (d *Darwin) stroke(ctx context.Context, k *Keyboard, report [8]byte) error {
	_, timing := d.settings()
	if err := k.Send(ctx, report); err != nil {
		return err
	}
	if err := pause(ctx, timing.HoldMS); err != nil {
		return err
	}
	if err := k.Send(ctx, [8]byte{}); err != nil {
		return err
	}
	if err := pause(ctx, timing.GapMS); err != nil {
		return err
	}
	if report[2] == 40 {
		return pause(ctx, timing.EnterMS)
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
