package main

import (
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"

	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/session"
)

// Only non-sensitive choices belong here. Readiness and pairing never survive a restart.
type preferences struct {
	Profile   string `json:"profile"`
	Edition   string `json:"edition"`
	LANIPv4   string `json:"lanIPv4,omitempty"`
	Transport string `json:"transport"`
}

func loadApp(path string) (*app, error) {
	m, err := manifest.Load()
	if err != nil {
		return nil, err
	}
	a := &app{manifest: m, preferencesPath: path,
		preferences: preferences{Profile: m.Default, Edition: "full", Transport: "manual"}}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		a.preferences = preferences{}
		if err := json.Unmarshal(data, &a.preferences); err != nil {
			return nil, err
		}
	}
	if err := a.validatePreferences(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *app) validatePreferences() error {
	p, err := a.manifest.Profile(a.preferences.Profile)
	if err != nil {
		return errors.New(locales.ChooseKnownHost())
	}
	if !slices.Contains(p.SessionProfile().Editions, a.preferences.Edition) {
		return errors.New(locales.UnsupportedEdition())
	}
	if !slices.Contains([]string{"manual", "keyboard", "composite", "external-pico"}, a.preferences.Transport) {
		return errors.New(locales.ChooseTransport())
	}
	if a.preferences.LANIPv4 != "" {
		ip, err := netip.ParseAddr(a.preferences.LANIPv4)
		if err != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.String() == "255.255.255.255" {
			return errors.New(locales.ChooseLANIPv4())
		}
	}
	return nil
}

func (a *app) applyPreferences() error {
	if err := a.validatePreferences(); err != nil {
		return err
	}
	if a.controller != nil {
		s := a.controller.Snapshot()
		if s.Busy || s.SideEffects || !slices.Contains([]session.Stage{session.Idle, session.Ready, session.NeedsSetup, session.Finished, session.Failed}, s.Stage) {
			return errors.New(locales.CommandRejected())
		}
		p, _ := a.manifest.Profile(a.preferences.Profile)
		if _, err := a.controller.SelectProfile(p.SessionProfile(), a.preferences.Edition); err != nil {
			return err
		}
		if _, err := a.controller.ConfigureTransport(preferredTransport(a.preferences.Transport), a.preferences.LANIPv4); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(a.preferences, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.preferencesPath), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(a.preferencesPath), ".preferences-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), a.preferencesPath)
}

// initializeSession is called once real adapters are available, never with a
// production fake. Preferences restore choices, not observed readiness.
func (a *app) initializeSession(env session.Environment) error {
	if err := a.validatePreferences(); err != nil {
		return err
	}
	p, _ := a.manifest.Profile(a.preferences.Profile)
	c := session.New(env, p.SessionProfile())
	if _, err := c.SelectProfile(p.SessionProfile(), a.preferences.Edition); err != nil {
		return err
	}
	if _, err := c.ConfigureTransport(preferredTransport(a.preferences.Transport), a.preferences.LANIPv4); err != nil {
		return err
	}
	a.controller = c
	return nil
}

func preferredTransport(mode string) session.Transport {
	c := session.Capabilities{}
	switch mode {
	case "manual":
		c.Manual = true
	case "keyboard":
		c.Keyboard = true
	case "composite":
		c.Keyboard, c.Gamepad = true, true
	case "external-pico":
		c.Keyboard, c.ExternalPico = true, true
	}
	return session.Transport{Mode: mode, Capabilities: c}
}
