package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/thedavidweng/any2jb/civswitch"
	"github.com/thedavidweng/any2jb/diagnostics"
	"github.com/thedavidweng/any2jb/driver"
	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/internal/usbgadget"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/readiness"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

// productionEnvironmentWithCivSwitch is the single production adapter
// composition point. Call off the UI thread: Stage performs file integrity
// checks and disk I/O. Packaged startup leaves Helpers empty and never supplies
// DeveloperSourceRoot or DeveloperProfileID; developer callers supply them
// explicitly, which never changes distribution policy.
// All developer-aware adapters compose here using the same explicit options,
// never reading launch environment themselves.
func productionEnvironmentWithCivSwitch(options engine.Options, inputOptions transport.DarwinOptions, civOptions civswitch.Options) session.Environment {
	root, stageErr := engine.Stage(options)
	bootstrap := func(run session.Run) (string, error) {
		if stageErr != nil {
			return "", stageErr
		}
		profile, err := options.Manifest.Profile(run.Profile.ID)
		if err != nil {
			return "", err
		}
		if profile.Revision != run.Profile.Revision || profile.BootstrapSHA256 != run.Profile.BootstrapSHA256 ||
			!slices.Contains(profile.SessionProfile().Editions, run.Edition) {
			return "", errors.New("bootstrap profile or edition mismatch")
		}
		if err := options.Manifest.VerifyProfile(root, profile.ID); err != nil {
			return "", err
		}
		source, err := os.ReadFile(filepath.Join(root, profile.Bootstrap))
		return string(source), err
	}
	inputOptions.Runtime, inputOptions.Bootstrap = options, bootstrap
	var keyboard transport.USBKeyboard
	if runtime.GOOS == "linux" {
		keyboard = transport.NewLinux(transport.LinuxOptions{
			Runtime:    options,
			Bootstrap:  bootstrap,
			Controller: inputOptions.Controller,
			Timing:     inputOptions.Timing,
			Confirm:    inputOptions.Confirm,
		})
	} else {
		keyboard = transport.NewDarwin(inputOptions)
	}
	civOptions.Manifest, civOptions.Support = options.Manifest, options.Support
	civOptions.DeveloperSourceRoot = options.DeveloperSourceRoot
	composite := civswitch.New(civOptions)
	platform := productionPlatform{root: root, options: options, stageError: stageErr, keyboard: keyboard, composite: composite}
	inputs := transport.NewDispatcher(transport.NewManual(bootstrap), keyboard, platform.Inspect)
	host := engine.NewHostRunner(options)
	tools := productionTools(root, stageErr, options, host)
	return session.Environment{
		CivSwitch: composite,
		Host:      host,
		Retained:  tools,
		Input:     inputs,
		Platform:  platform,
		Tools:     tools,
	}
}

// productionTools composes the retained lease and read-only tool adapter. Tool
// sources are only staged from an explicit developer source root; a packaged app
// stages nothing and every entry keeps the manifest's unavailable reason.
func productionTools(root string, stageError error, options engine.Options, host *engine.HostRunner) *retained.Manager {
	stageOptions := retained.StageOptions{Support: options.Support,
		Version: options.Version, SourceRoot: options.DeveloperSourceRoot, Manifest: options.Manifest,
		Components: []string{"ouncecake", "sd-browser", "savekit", "mintbrew", "mintbrew-native",
			"izlauncher", "izlauncher-blitbun", "izlauncher-shell", "izlauncher-vacootube", "homebrew-runtime", "modloader",
			"launcher-seedsow-demo", "launcher-seedsow-full", "launcher-nora2-demo", "launcher-nora1-demo",
			"launcher-hamidashi-demo", "launcher-lua-editor", "launcher-lua-editor-pro",
			"launcher-balatro", "launcher-balatro-showcase", "launcher-gravity-circuit"}}
	toolRoot, staged, stageToolsError := retained.StageTools(stageOptions)
	node, toolHost := "", ""
	if stageError == nil {
		node, toolHost = filepath.Join(root, "runtime/node"), filepath.Join(root, "runtime/toolhost.cjs")
	}
	homebrewRuntime := ""
	if options.DeveloperSourceRoot != "" && slices.Contains(staged, "homebrew-runtime") {
		// The portable runtime must also be prepared and verified; a failed
		// preparation keeps the homebrew launchers unavailable with the reason.
		var err error
		if homebrewRuntime, err = retained.StageHomebrewRuntime(stageOptions, toolRoot, staged); err != nil {
			staged = slices.DeleteFunc(staged, func(id string) bool { return id == "homebrew-runtime" })
			stageToolsError = errors.Join(stageToolsError, err)
		}
	}
	if node != "" && slices.Contains(staged, "izlauncher") {
		if err := retained.StageIZLauncher(node, filepath.Join(toolRoot, "tools", "izlauncher")); err != nil {
			staged = slices.DeleteFunc(staged, func(id string) bool { return id == "izlauncher" })
			stageToolsError = errors.Join(stageToolsError, err)
		}
	}
	return retained.New(retained.Options{Support: options.Support, Node: node, Host: toolHost,
		ToolRoot: toolRoot, Staged: staged, StageError: stageToolsError,
		EngineStaged: options.DeveloperSourceRoot != "" && options.DeveloperProfileID != "",
		EngineRoot:   root, HomebrewRuntime: homebrewRuntime,
		Manifest: options.Manifest, Runs: host})
}

// Packaged startup leaves the explicit developer source empty.
func productionDriverGate(options engine.Options) *driver.Gate {
	return driver.New(driver.Options{Assets: options.Assets, Manifest: options.Manifest, DeveloperSourceRoot: options.DeveloperSourceRoot})
}

type productionPlatform struct {
	composite  *civswitch.Adapter
	keyboard   transport.USBKeyboard
	root       string
	options    engine.Options
	stageError error
}

func (p productionPlatform) Inspect(ctx context.Context, run session.Run) ([]session.Check, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	checks := []session.Check{}
	state := session.CheckVerified
	message := "Bundled Node runtime integrity verified."
	action := ""
	if p.stageError != nil {
		state = session.RequiresAttention
		message = "Runtime staging failed."
		action = "Install an intact app bundle with its matching runtime resources."
	}
	checks = append(checks, session.Check{ID: "runtime", State: state, Message: message, NextAction: action})
	distribution, err := p.options.Manifest.DistributionFor(run.Profile.ID)
	if err != nil {
		return nil, err
	}
	state, message, action = session.RequiresAttention, distribution.Reason, "This engine cannot launch from the packaged app."
	if p.stageError == nil && p.options.DeveloperSourceRoot != "" && p.options.DeveloperProfileID == run.Profile.ID {
		record, err := engine.ReadRecord(p.root)
		if err == nil && record.Source == "developer-staged" && record.ProfileID == run.Profile.ID &&
			p.options.Manifest.VerifyProfile(p.root, run.Profile.ID) == nil {
			state, message, action = session.CheckVerified, "Developer-staged engine, not redistributable.", ""
		}
	}
	profile, err := p.options.Manifest.Profile(run.Profile.ID)
	if err != nil {
		return nil, err
	}
	if profile.Engine == "izmysh" {
		state, message, action = session.RequiresAttention, locales.IzmyshUnavailable(), "Choose an ns2jb host with an implemented verifier."
	}
	checks = append(checks, session.Check{ID: "engine", State: state, Message: message, NextAction: action})
	observed, err := readiness.New().Inspect(ctx, run)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "linux" && p.keyboard != nil && p.keyboard.SelectedKeyboardController() == "" {
		if single := singleFreeController(observed); single != "" {
			_ = p.keyboard.Configure(single, transport.DefaultTiming())
		}
	}
	usb := session.Check{ID: "usb-runtime", GatesTransport: "usb-hid", State: session.RequiresAttention,
		Message: "Verified USB helpers are not available.", NextAction: "USB input requires permitted, integrity-verified helper binaries. Guided manual input does not require these helpers."}
	if p.stageError == nil && engine.VerifyUSB(p.root, p.options.Manifest) == nil {
		usb.State, usb.Message, usb.NextAction = session.CheckVerified, "USB helper integrity verified; enumeration is not verified.", ""
	}
	if runtime.GOOS == "linux" && p.stageError == nil && p.options.DeveloperSourceRoot != "" &&
		p.options.DeveloperProfileID == run.Profile.ID && p.options.Manifest.VerifyProfile(p.root, run.Profile.ID) == nil {
		if _, err := usbgadget.VerifySelfExecutable(); err == nil {
			usb.State, usb.Message, usb.NextAction = session.CheckVerified, locales.UsbDeveloperHelpers(), ""
		}
	}
	keyboardReady := p.stageError == nil && p.keyboard != nil && p.keyboard.Check(ctx, run) == nil
	if keyboardReady {
		usb.State, usb.Message, usb.NextAction = session.CheckVerified, locales.UsbDeveloperHelpers(), ""
	}
	input := session.Check{ID: "input-adapter", State: session.RequiresAttention,
		Message: locales.AutomatedInputUnavailable(), NextAction: locales.SelectGuidedManual()}
	if run.Transport.Capabilities.Manual {
		input.State, input.Message, input.NextAction = session.CheckVerified, locales.ManualInputAvailable(), ""
	} else if keyboardReady && run.Transport.Capabilities.Keyboard && !run.Transport.Capabilities.Gamepad && !run.Transport.Capabilities.ExternalPico {
		input.State, input.Message, input.NextAction = session.CheckVerified, locales.UsbInputAvailable(), ""
	}
	if run.Transport.Capabilities.Gamepad && run.Transport.Capabilities.Keyboard {
		if p.composite != nil && p.composite.Check() == nil {
			usb.State, usb.Message, usb.NextAction = session.CheckVerified, "Composite helper and payload integrity verified; enumeration is not verified.", ""
			input.State, input.Message, input.NextAction = session.CheckVerified, "Composite input is available for CivSwitch, not SmileBASIC.", ""
		} else {
			input.Message, input.NextAction = "Composite input components are unavailable.", "Use permitted, integrity-verified CivSwitch components before using composite input."
			usb.Message, usb.NextAction = input.Message, input.NextAction
		}
	}
	checks = append(checks, usb, input)
	if keyboardReady {
		observed = session.ObserveKeyboardEnumeration(observed, run.Transport.Controller, p.keyboard.EnumeratedController())
	}
	return append(checks, observed...), nil
}

func singleFreeController(checks []session.Check) string {
	found := ""
	for _, check := range checks {
		if check.Controller == "" || check.State == session.RequiresAttention {
			continue
		}
		if found != "" {
			return ""
		}
		found = check.Controller
	}
	return found
}

func (p productionPlatform) ExportDiagnostics(ctx context.Context, snapshot session.Snapshot) (string, error) {
	return (diagnostics.Store{Support: p.options.Support}).Export(ctx, snapshot)
}
func (p productionPlatform) ReleaseInterrupted(ctx context.Context) error {
	return (diagnostics.Store{Support: p.options.Support}).ReleaseInterrupted(ctx)
}

var _ session.PlatformServices = productionPlatform{}
