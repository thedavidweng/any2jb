package main

import (
	"context"
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

type usbPreferences struct {
	Controller string           `json:"controller"`
	Timing     transport.Timing `json:"timing"`
}

type keyboardPanel struct {
	input                                                        transport.USBKeyboard
	open, initialized, available, invalid, checkOK               bool
	controller, gap, hold, enter, enumeration, load, run, remove string
	completion                                                   chan bool
	forwarding, closing, focused, focusRequested                 bool
	focus                                                        ui.Handle
	report                                                       [8]byte
	runID                                                        string
	applied                                                      [8]string
	checkedProfile, checkedEdition                               string
}

func (p *keyboardPanel) defaults(saved *usbPreferences) {
	if p.initialized {
		return
	}
	p.initialized = true
	t := transport.DefaultTiming()
	if saved != nil {
		p.controller = saved.Controller
		t = saved.Timing
	}
	p.gap, p.hold, p.enter = strconv.Itoa(t.GapMS), strconv.Itoa(t.HoldMS), strconv.Itoa(t.EnterMS)
	p.enumeration = strconv.Itoa(t.EnumerationSeconds)
	p.load, p.run, p.remove = strconv.Itoa(t.LoadMS), strconv.Itoa(t.RunMS), strconv.Itoa(t.RemoveMS)
}

func (p *keyboardPanel) values() [8]string {
	return [8]string{p.controller, p.gap, p.hold, p.enter, p.enumeration, p.load, p.run, p.remove}
}

func (p *keyboardPanel) observe(s session.Snapshot) {
	if p.forwarding && (!s.Busy || p.runID != s.RunID) {
		p.forwarding = false
		p.closing = false
		p.focused = false
		p.focusRequested = false
		p.report = [8]byte{}
		p.runID = ""
	}
}

func (a *app) keyboardView(c *ui.Context) {
	p := &a.keyboard
	p.defaults(a.preferences.USB)
	if p.completion != nil {
		select {
		case p.checkOK = <-p.completion:
			p.completion = nil
		default:
		}
	}
	s := session.Snapshot{}
	if a.controller != nil {
		s = a.controller.Snapshot()
	}
	p.observe(s)
	sectionTitle(c, locales.UsbKeyboardTitle())
	ui.Text(c, locales.UsbKeyboardSafety())
	if p.forwarding {
		if p.closing {
			ui.Text(c, locales.UsbKeyboardClosing())
		} else if s.KeyboardOpen {
			ui.Text(c.Key("usb-capture"), locales.UsbKeyboardCapture()).Focusable().
				InputMethod(false).Bind(&p.focus).HandleInput(a.keyboardEvent)
			if !p.focusRequested {
				p.focus.Focus(c)
				p.focusRequested = true
			}
			focused := p.focus.Focused(c)
			if focused {
				p.focused = true
			} else if p.focused {
				a.stopKeyboard()
			}
		} else {
			ui.Text(c, locales.UsbKeyboardOpening())
		}
		if inline(ui.Button(c.Key("usb-stop-forwarding"), locales.UsbStopForwarding())).Disabled(p.closing).Clicked() {
			a.stopKeyboard()
		}
		return
	}
	controllers := []string{}
	prerequisites := true
	for _, check := range s.Checks {
		if check.GatesTransport == "usb-hid" && check.State == session.RequiresAttention && check.ID != "usb-controllers" {
			prerequisites = false
		}
		if (check.Controller != "" || transport.ValidateAnyController(check.ID) == nil) && check.State != session.RequiresAttention {
			controllers = append(controllers, check.ID)
		}
	}
	// Derived every frame: a controller list briefly emptied by re-inspection
	// hides availability but never consumes the one-shot helper check.
	p.available = p.checkOK && p.applied == p.values() && p.checkedProfile == s.Profile.ID &&
		p.checkedEdition == s.Edition && slices.Contains(controllers, p.controller)
	if !p.available {
		ui.Text(c, locales.UsbHelpersUnavailable())
	} else {
		ui.Text(c, locales.UsbDeveloperHelpers())
	}
	busy := s.Busy || s.SideEffects || s.Stage == session.Indeterminate || p.completion != nil
	ui.Text(c, locales.UsbController())
	ui.Select(c.Key("usb-controller"), &p.controller, controllers).Label(locales.UsbController()).Disabled(busy || len(controllers) == 0)
	if len(controllers) == 0 {
		ui.Text(c, locales.UsbDiscoverControllers())
	}
	// Timing values sit two to a row so the panel's actions stay near them.
	ui.Row(c).FillWidth().Wrap().Gap(12).Children(func() {
		ui.Column(c).WidthPercent(46).MinWidth(200).Shrink(0).Grow(1).Gap(4).Children(func() {
			ui.Text(c, locales.UsbGap())
			ui.TextInput(c.Key("usb-gap"), &p.gap).Label(locales.UsbGap()).Disabled(busy)
		})
		ui.Column(c).WidthPercent(46).MinWidth(200).Shrink(0).Grow(1).Gap(4).Children(func() {
			ui.Text(c, locales.UsbHold())
			ui.TextInput(c.Key("usb-hold"), &p.hold).Label(locales.UsbHold()).Disabled(busy)
		})
		ui.Column(c).WidthPercent(46).MinWidth(200).Shrink(0).Grow(1).Gap(4).Children(func() {
			ui.Text(c, locales.UsbEnter())
			ui.TextInput(c.Key("usb-enter"), &p.enter).Label(locales.UsbEnter()).Disabled(busy)
		})
		ui.Column(c).WidthPercent(46).MinWidth(200).Shrink(0).Grow(1).Gap(4).Children(func() {
			ui.Text(c, locales.UsbEnumeration())
			ui.TextInput(c.Key("usb-enumeration"), &p.enumeration).Label(locales.UsbEnumeration()).Disabled(busy)
		})
		ui.Column(c).WidthPercent(46).MinWidth(200).Shrink(0).Grow(1).Gap(4).Children(func() {
			ui.Text(c, locales.UsbLoadWait())
			ui.TextInput(c.Key("usb-load-wait"), &p.load).Label(locales.UsbLoadWait()).Disabled(busy)
		})
		ui.Column(c).WidthPercent(46).MinWidth(200).Shrink(0).Grow(1).Gap(4).Children(func() {
			ui.Text(c, locales.UsbRunWait())
			ui.TextInput(c.Key("usb-run-wait"), &p.run).Label(locales.UsbRunWait()).Disabled(busy)
		})
		ui.Column(c).WidthPercent(46).MinWidth(200).Shrink(0).Grow(1).Gap(4).Children(func() {
			ui.Text(c, locales.UsbRemoveWait())
			ui.TextInput(c.Key("usb-remove-wait"), &p.remove).Label(locales.UsbRemoveWait()).Disabled(busy)
		})
	})
	if p.invalid {
		ui.Text(c, locales.UsbInvalidSettings())
	}
	ui.Row(c).Gap(8).Children(func() {
		if inline(ui.Button(c.Key("usb-stock-defaults"), locales.UsbStockDefaults())).Disabled(busy).Clicked() {
			p.initialized = false
			p.defaults(nil)
			p.checkOK = false
			p.invalid = false
		}
		if inline(ui.Button(c.Key("usb-apply"), locales.UsbApplySettings())).Disabled(busy).Clicked() {
			a.applyUSBSettings(c, controllers, s)
		}
	})
	caps := s.Transport.Capabilities
	if inline(ui.PrimaryButton(c.Key("usb-start-forwarding"), locales.UsbStartForwarding())).Disabled(busy || !p.available || !prerequisites || !caps.Keyboard || caps.Gamepad || caps.ExternalPico || a.controller == nil).Clicked() {
		s, err := a.controller.StartKeyboard()
		a.command(s, err)
		if err == nil {
			p.forwarding = true
			p.runID = s.RunID
			p.closing = false
			p.focused = false
			p.focusRequested = false
			p.report = [8]byte{}
		}
	}
	if inline(ui.Button(c.Key("usb-close-panel"), locales.UsbClosePanel())).Disabled(p.completion != nil).Clicked() {
		a.closeKeyboardPanel()
	}
}

func (a *app) applyUSBSettings(c *ui.Context, controllers []string, s session.Snapshot) {
	p := &a.keyboard
	p.invalid = false
	p.available = false
	var n [7]int
	for i, value := range []string{p.gap, p.hold, p.enter, p.enumeration, p.load, p.run, p.remove} {
		var err error
		n[i], err = strconv.Atoi(value)
		if err != nil {
			p.invalid = true
			return
		}
	}
	t := transport.Timing{GapMS: n[0], HoldMS: n[1], EnterMS: n[2], EnumerationSeconds: n[3], LoadMS: n[4], RunMS: n[5], RemoveMS: n[6]}
	if t.Validate() != nil || !slices.Contains(controllers, p.controller) {
		p.invalid = true
		return
	}
	a.preferences.USB = &usbPreferences{Controller: p.controller, Timing: t}
	if err := a.savePreferences(); err != nil {
		p.invalid = true
		return
	}
	p.applied = p.values()
	p.checkedProfile = s.Profile.ID
	p.checkedEdition = s.Edition
	if a.controller != nil && s.Transport.Controller != p.controller {
		t := s.Transport
		t.Controller = p.controller
		if _, err := a.controller.ConfigureTransport(t, s.LANOverride); err != nil {
			p.invalid = true
			return
		}
		a.command(a.controller.Inspect())
	}
	if p.input == nil {
		return
	}
	p.completion = make(chan bool, 1)
	ch, input, controller, services := p.completion, p.input, p.controller, c.Services()
	run := session.Run{ID: s.RunID, Profile: s.Profile, Edition: s.Edition, Transport: s.Transport, LANIPv4: s.LANIPv4}
	run.Transport.Controller = controller
	go func() {
		err := input.Configure(controller, t)
		if err == nil {
			err = input.Check(context.Background(), run)
		}
		ch <- err == nil
		services.Invalidate()
	}()
}

func (a *app) stopKeyboard() {
	p := &a.keyboard
	if p.forwarding && !p.closing && a.controller != nil {
		p.closing = true
		a.command(a.controller.CloseKeyboard())
	}
}

// closeKeyboardPanel releases live forwarding before hiding the panel, so a
// hidden capture never leaves a key held down on the console.
func (a *app) closeKeyboardPanel() {
	a.stopKeyboard()
	a.keyboard.open = false
}

func (a *app) keyboardEvent(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputKeyDown && ev.Kind != ui.InputKeyUp {
		return false
	}
	if ev.Code == ui.CodeEscape {
		a.stopKeyboard()
		return true
	}
	// Tab keeps the native panel keyboard-accessible; leaving the capture
	// control closes its keyboard lease rather than forwarding a stuck key.
	if ev.Code == ui.CodeTab {
		return false
	}
	if !a.keyboard.forwarding || a.keyboard.closing || a.controller == nil {
		return false
	}
	p := &a.keyboard
	report := p.report
	code := uint16(ev.Code)
	if code >= 0xe0 && code <= 0xe7 {
		mask := byte(1 << (code - 0xe0))
		if ev.Kind == ui.InputKeyDown {
			report[0] |= mask
		} else {
			report[0] &= ^mask
		}
	} else if code >= 4 && code <= 0x73 {
		keys := []byte{}
		found := false
		for _, key := range report[2:] {
			if key == 0 {
				continue
			}
			if key == byte(code) {
				found = true
				if ev.Kind == ui.InputKeyUp {
					continue
				}
			}
			keys = append(keys, key)
		}
		if ev.Kind == ui.InputKeyDown && !found {
			keys = append(keys, byte(code))
		}
		if len(keys) > 6 {
			a.stopKeyboard()
			return true
		}
		for i := 2; i < 8; i++ {
			report[i] = 0
		}
		copy(report[2:], keys)
	} else {
		a.stopKeyboard()
		return true
	}
	if report != p.report {
		p.report = report
		_, err := a.controller.ForwardKeyboardReport(report)
		if err != nil {
			a.stopKeyboard()
		}
	}
	return true
}
