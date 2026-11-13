package usbgadget

import (
	"context"
	"errors"
	"fmt"
	"os"
)

const (
	MaxCivSwitchPayloadBytes = 131072
	MaxCivSwitchPayloadLines = 256
	MaxCivSwitchLineBytes    = 254

	civKeyHoldMS    = 40
	civKeyGapMS     = 40
	civEnterHoldMS  = 40
	civEnterGapMS   = 200
	civPreAWaitMS   = 800
	civPostAWaitMS  = 1200
	civAPressMS     = 50
	civPreambleA    = 5
	civPreambleGap  = 800
	civEscToAWaitMS = 800
)

type CivSwitchPayload struct {
	Lines       []string
	Characters  int
	EstimatedMS int64
}

// CivKeyForASCII matches civswitch-hid.c key_for_ascii identically.
func CivKeyForASCII(c byte) (modifier, key uint8, ok bool) {
	if c >= 'a' && c <= 'z' {
		return 0, 4 + c - 'a', true
	}
	if c >= 'A' && c <= 'Z' {
		return 2, 4 + c - 'A', true
	}
	if c >= '1' && c <= '9' {
		return 0, 30 + c - '1', true
	}
	if c == '0' {
		return 0, 39, true
	}
	if c == ' ' {
		return 0, 44, true
	}
	if c == '\t' {
		return 0, 43, true
	}
	const (
		plain   = "-=[]\\;',./`"
		shifted = "_+{}|:\"<>?~"
	)
	punctKeys := [...]uint8{45, 46, 47, 48, 49, 51, 52, 54, 55, 56, 53}
	for i := 0; i < len(punctKeys); i++ {
		if c == plain[i] {
			return 0, punctKeys[i], true
		}
		if c == shifted[i] {
			return 2, punctKeys[i], true
		}
	}
	const shiftDigits = "!@#$%^&*()"
	for i := 0; i < len(shiftDigits); i++ {
		if c == shiftDigits[i] {
			if i == 9 {
				return 2, 39, true
			}
			return 2, uint8(30 + i), true
		}
	}
	return 0, 0, false
}

// ParseCivSwitchPayload implements the exact bounds, ASCII check, and duration
// estimator from pinned mac2jb civswitch-hid.c load_payload.
func ParseCivSwitchPayload(path string) (*CivSwitchPayload, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxCivSwitchPayloadBytes {
		return nil, errors.New("CivSwitch chat payload is invalid or unreadable")
	}
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) != info.Size() {
		return nil, errors.New("CivSwitch chat payload is invalid or unreadable")
	}
	p := &CivSwitchPayload{}
	start := 0
	for index := 0; index <= len(data); index++ {
		if index < len(data) && data[index] != '\n' {
			continue
		}
		end := index
		if end > start && data[end-1] == '\r' {
			end--
		}
		if end == start && index == len(data) && start == len(data) {
			break
		}
		if end == start || end-start > MaxCivSwitchLineBytes || len(p.Lines) == MaxCivSwitchPayloadLines {
			return nil, errors.New("CivSwitch chat payload is invalid or unreadable")
		}
		for j := start; j < end; j++ {
			if _, _, ok := CivKeyForASCII(data[j]); !ok {
				return nil, errors.New("CivSwitch chat payload is invalid or unreadable")
			}
		}
		line := string(data[start:end])
		p.Lines = append(p.Lines, line)
		p.Characters += len(line)
		start = index + 1
	}
	if len(p.Lines) == 0 {
		return nil, errors.New("CivSwitch chat payload is invalid or unreadable")
	}
	p.EstimatedMS = civPreambleA * (civAPressMS + civPreambleGap)
	for i, row := range p.Lines {
		p.EstimatedMS += int64(civKeyHoldMS + civEscToAWaitMS + civAPressMS + civPostAWaitMS +
			len(row)*civKeyHoldMS + 2*civEnterHoldMS + civEnterGapMS)
		var previous uint8
		for j := 0; j < len(row); j++ {
			_, key, _ := CivKeyForASCII(row[j])
			if j > 0 && key == previous {
				p.EstimatedMS += civKeyGapMS
			}
			previous = key
		}
		if i+1 < len(p.Lines) {
			p.EstimatedMS += civPreAWaitMS + civAPressMS + civPostAWaitMS
		}
	}
	return p, nil
}

// RunCivSwitchMacro drives the exact composite gamepad (iface 0) + keyboard
// (iface 1) report sequence and MAC2JB_CIVSWITCH progress events from civswitch-hid.c.
func RunCivSwitchMacro(
	ctx context.Context,
	payload *CivSwitchPayload,
	write func(ctx context.Context, iface int, report [8]byte) error,
	sleepMS func(ctx context.Context, ms int) error,
	emit func(line string),
) (finished bool, cancelled bool) {
	curPad := NeutralGamepadReport
	curKbd := NeutralKeyboardReport

	setState := func(pad, kbd [8]byte) bool {
		if ctx.Err() != nil {
			return false
		}
		if pad != curPad {
			if err := write(ctx, 0, pad); err != nil {
				return false
			}
			curPad = pad
		}
		if kbd != curKbd {
			if err := write(ctx, 1, kbd); err != nil {
				return false
			}
			curKbd = kbd
		}
		return ctx.Err() == nil
	}
	neutral := func() bool { return setState(NeutralGamepadReport, NeutralKeyboardReport) }
	step := func(pad, kbd [8]byte, ms int) bool {
		if !setState(pad, kbd) {
			return false
		}
		if err := sleepMS(ctx, ms); err != nil {
			return false
		}
		return ctx.Err() == nil
	}
	aPress := func() bool {
		pad := NeutralGamepadReport
		pad[0] = 4
		return step(pad, NeutralKeyboardReport, civAPressMS)
	}
	keyPress := func(mod, key uint8, ms int) bool {
		return step(NeutralGamepadReport, [8]byte{mod, 0, key}, ms)
	}
	waitNeutral := func(ms int) bool {
		if !neutral() {
			return false
		}
		if err := sleepMS(ctx, ms); err != nil {
			return false
		}
		return ctx.Err() == nil
	}

	for i := 0; i < civPreambleA; i++ {
		if !aPress() || !waitNeutral(civPreambleGap) {
			return false, ctx.Err() != nil
		}
	}
	total := len(payload.Lines)
	for i, line := range payload.Lines {
		emit(fmt.Sprintf(`MAC2JB_CIVSWITCH {"type":"line-start","line":%d,"total":%d,"completed":%d}`, i+1, total, i))
		if !keyPress(0, 41, civKeyHoldMS) || !waitNeutral(civEscToAWaitMS) ||
			!aPress() || !waitNeutral(civPostAWaitMS) {
			return false, ctx.Err() != nil
		}
		var previous uint8
		for j := 0; j < len(line); j++ {
			mod, key, ok := CivKeyForASCII(line[j])
			if !ok {
				return false, false
			}
			if j > 0 && key == previous && !waitNeutral(civKeyGapMS) {
				return false, ctx.Err() != nil
			}
			if !keyPress(mod, key, civKeyHoldMS) {
				return false, ctx.Err() != nil
			}
			previous = key
		}
		if !keyPress(0, 40, civEnterHoldMS) || !waitNeutral(civEnterGapMS) ||
			!keyPress(0, 40, civEnterHoldMS) {
			return false, ctx.Err() != nil
		}
		emit(fmt.Sprintf(`MAC2JB_CIVSWITCH {"type":"line-complete","line":%d,"total":%d,"completed":%d}`, i+1, total, i+1))
		if i+1 < total {
			if !waitNeutral(civPreAWaitMS) || !aPress() || !waitNeutral(civPostAWaitMS) {
				return false, ctx.Err() != nil
			}
		}
	}
	if !neutral() {
		return false, ctx.Err() != nil
	}
	return true, false
}
