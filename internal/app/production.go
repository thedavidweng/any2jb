package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/thedavidweng/any2jb/internal/civswitch"
	"github.com/thedavidweng/any2jb/internal/diagnostics"
	"github.com/thedavidweng/any2jb/internal/driver"
	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/readiness"
	"github.com/thedavidweng/any2jb/internal/retained"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/transport"
	"github.com/thedavidweng/any2jb/internal/usbgadget"
	"github.com/thedavidweng/any2jb/locales"
)

// ProductionEnvironmentWithCivSwitch is the single production adapter
// composition point. Call off the UI thread: Stage performs file integrity
// checks and disk I/O. Packaged startup leaves Helpers empty and never supplies
// DeveloperSourceRoot or DeveloperProfileID; developer callers supply them
// explicitly, which never changes distribution policy.
func ProductionEnvironmentWithCivSwitch(options engine.Options, inputOptions transport.DarwinOptions, civOptions civswitch.Options) session.Environment {
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
	civOptions.Assets, civOptions.StagedRuntimeRoot = options.Assets, root
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

func productionTools(root string, stageError error, options engine.Options, host *engine.HostRunner) *retained.Manager {
	allDeveloperComponents := []string{"ouncecake", "sd-browser", "savekit", "mintbrew", "mintbrew-native",
		"izlauncher", "izlauncher-blitbun", "izlauncher-shell", "izlauncher-vacootube", "homebrew-runtime", "modloader",
		"launcher-seedsow-demo", "launcher-seedsow-full", "launcher-nora2-demo", "launcher-nora1-demo",
		"launcher-hamidashi-demo", "launcher-lua-editor", "launcher-lua-editor-pro",
		"launcher-balatro", "launcher-balatro-showcase", "launcher-gravity-circuit"}
	var toolRoot string
	var staged []string
	var stageToolsError error
	bundled := false
	homebrewRuntime := ""
	node, toolHost := "", ""
	if stageError == nil {
		node, toolHost = filepath.Join(root, "runtime/node"), filepath.Join(root, "runtime/toolhost.cjs")
	}
	if options.DeveloperSourceRoot != "" {
		stageOptions := retained.StageOptions{
			Support: options.Support, Version: options.Version,
			SourceRoot: options.DeveloperSourceRoot, Manifest: options.Manifest,
			Components: allDeveloperComponents,
		}
		toolRoot, staged, stageToolsError = retained.StageTools(stageOptions)
		if slices.Contains(staged, "homebrew-runtime") {
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
	} else if stageError == nil && options.Assets != "" && options.Manifest != nil {
		bundled = true
		var bundledIDs []string
		for _, c := range options.Manifest.Components {
			if c.Source != nil && c.Distribution.Available() {
				bundledIDs = append(bundledIDs, c.ID)
			}
		}
		if len(bundledIDs) > 0 {
			toolRoot, staged, stageToolsError = retained.StageTools(retained.StageOptions{
				Support: options.Support, Version: options.Version,
				SourceRoot: options.Assets, Bundled: true,
				Manifest: options.Manifest, Components: bundledIDs,
			})
		}
		if toolRoot != "" {
			for _, optID := range retained.VerifyStagedComponents(toolRoot, options.Manifest, retained.OptionalHomebrewComponents) {
				if !slices.Contains(staged, optID) {
					staged = append(staged, optID)
				}
			}
			if slices.Contains(staged, "homebrew-runtime") {
				if homebrewRuntime = retained.CheckHomebrewRuntime(toolRoot); homebrewRuntime == "" {
					staged = slices.DeleteFunc(staged, func(id string) bool { return id == "homebrew-runtime" })
				}
			}
			if node != "" && slices.Contains(staged, "izlauncher") {
				if retained.CheckIZLauncher(node, filepath.Join(toolRoot, "tools", "izlauncher")) != nil {
					staged = slices.DeleteFunc(staged, func(id string) bool { return id == "izlauncher" })
				}
			}
		}
	}
	engineStaged := stageError == nil && ((options.DeveloperSourceRoot != "" && options.DeveloperProfileID != "") || options.DeveloperSourceRoot == "")
	return retained.New(retained.Options{Support: options.Support, Version: options.Version, Bundled: bundled,
		Node: node, Host: toolHost,
		ToolRoot: toolRoot, Staged: staged, StageError: stageToolsError,
		EngineStaged: engineStaged,
		EngineRoot:   root, HomebrewRuntime: homebrewRuntime,
		Manifest: options.Manifest, Runs: host})
}

// ProductionDriverGate creates the driver gate with packaged or explicit developer sources.
func ProductionDriverGate(options engine.Options) *driver.Gate {
	return driver.New(driver.Options{Assets: options.Assets, Manifest: options.Manifest, DeveloperSourceRoot: options.DeveloperSourceRoot})
}

type productionPlatform struct {
	composite  *civswitch.Adapter
	keyboard   transport.USBKeyboard
	root       string
	options    engine.Options
	stageError error
}

func newCheck(id string, state session.CheckState, msg, action locales.Text, gates string) session.Check {
	return session.Check{
		ID:             id,
		State:          state,
		Message:        msg.English(),
		MessageText:    msg,
		NextAction:     action.English(),
		NextActionText: action,
		GatesTransport: gates,
	}
}

func (p productionPlatform) Inspect(ctx context.Context, run session.Run) ([]session.Check, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	checks := []session.Check{}
	rtState := session.CheckVerified
	rtMsg := locales.Key("dynRtNodeVerified")
	var rtAction locales.Text
	if p.stageError != nil {
		rtState = session.RequiresAttention
		rtMsg = locales.Key("dynRtStagingFailed")
		rtAction = locales.Key("dynRtInstallIntact")
	}
	checks = append(checks, newCheck(session.CheckRuntime, rtState, rtMsg, rtAction, ""))
	distribution, err := p.options.Manifest.DistributionFor(run.Profile.ID)
	if err != nil {
		return nil, err
	}
	engState := session.RequiresAttention
	engMsg := locales.DistributionReason(run.Profile.ID, distribution.Reason)
	engAction := locales.Key("dynRtEnginePackaged")
	if distribution.Available() && p.stageError != nil {
		engAction = locales.Key("dynRtInstallIntact")
	}
	if p.stageError == nil && p.options.DeveloperSourceRoot != "" && p.options.DeveloperProfileID == run.Profile.ID {
		record, err := engine.ReadRecord(p.root)
		if err == nil && record.Source == "developer-staged" && record.ProfileID == run.Profile.ID &&
			p.options.Manifest.VerifyProfile(p.root, run.Profile.ID) == nil {
			engState, engMsg, engAction = session.CheckVerified, locales.Key("dynRtDeveloperStaged"), locales.Text{}
		}
	} else if p.stageError == nil && p.options.DeveloperSourceRoot == "" && distribution.Available() {
		record, err := engine.ReadRecord(p.root)
		if err == nil && record.Source == "bundled" && record.Redistributable &&
			p.options.Manifest.VerifyProfile(p.root, run.Profile.ID) == nil {
			engState, engMsg, engAction = session.CheckVerified, locales.Key("dynRtBundledEngine"), locales.Text{}
		} else {
			engAction = locales.Key("dynRtInstallIntact")
		}
	}
	profile, err := p.options.Manifest.Profile(run.Profile.ID)
	if err != nil {
		return nil, err
	}
	if profile.Engine == "izmysh" {
		engState = session.RequiresAttention
		engMsg = locales.Key("izmyshUnavailable")
		engAction = locales.Key("dynRtChooseVerifier")
	}
	checks = append(checks, newCheck(session.CheckEngine, engState, engMsg, engAction, ""))
	observed, err := readiness.New().Inspect(ctx, run)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "linux" && p.keyboard != nil && p.keyboard.SelectedKeyboardController() == "" {
		if single := readiness.SingleFreeController(observed); single != "" {
			_ = p.keyboard.Configure(single, transport.DefaultTiming())
		}
	}
	usb := newCheck(session.CheckUSBRuntime, session.RequiresAttention,
		locales.Key("dynRtHelpersUnavailable"), locales.Key("dynRtHelpersRequired"), session.GateUSBHID)
	if p.stageError == nil && engine.VerifyUSB(p.root, p.options.Manifest) == nil {
		usb = newCheck(session.CheckUSBRuntime, session.CheckVerified, locales.Key("dynRtHelpersVerified"), locales.Text{}, session.GateUSBHID)
	}
	if runtime.GOOS == "linux" && p.stageError == nil &&
		((p.options.DeveloperSourceRoot != "" && p.options.DeveloperProfileID == run.Profile.ID) ||
			(p.options.DeveloperSourceRoot == "" && distribution.Available())) &&
		p.options.Manifest.VerifyProfile(p.root, run.Profile.ID) == nil {
		if _, err := usbgadget.VerifySelfExecutable(); err == nil {
			if p.options.DeveloperSourceRoot != "" {
				usb = newCheck(session.CheckUSBRuntime, session.CheckVerified, locales.Key("usbDeveloperHelpers"), locales.Text{}, session.GateUSBHID)
			} else {
				usb = newCheck(session.CheckUSBRuntime, session.CheckVerified, locales.Key("dynRtHelpersVerified"), locales.Text{}, session.GateUSBHID)
			}
		}
	}
	keyboardReady := p.stageError == nil && p.keyboard != nil && p.keyboard.Check(ctx, run) == nil
	if keyboardReady {
		if p.options.DeveloperSourceRoot != "" {
			usb = newCheck(session.CheckUSBRuntime, session.CheckVerified, locales.Key("usbDeveloperHelpers"), locales.Text{}, session.GateUSBHID)
		} else {
			usb = newCheck(session.CheckUSBRuntime, session.CheckVerified, locales.Key("dynRtHelpersVerified"), locales.Text{}, session.GateUSBHID)
		}
	}
	input := newCheck(session.CheckInputAdapter, session.RequiresAttention,
		locales.Key("automatedInputUnavailable"), locales.Key("selectGuidedManual"), "")
	if run.Transport.Capabilities.Manual {
		input = newCheck(session.CheckInputAdapter, session.CheckVerified, locales.Key("manualInputAvailable"), locales.Text{}, "")
	} else if keyboardReady && run.Transport.Capabilities.Keyboard && !run.Transport.Capabilities.Gamepad && !run.Transport.Capabilities.ExternalPico {
		input = newCheck(session.CheckInputAdapter, session.CheckVerified, locales.Key("usbInputAvailable"), locales.Text{}, "")
	}
	if run.Transport.Capabilities.Gamepad && run.Transport.Capabilities.Keyboard {
		if p.composite != nil && p.composite.Check() == nil {
			usb = newCheck(session.CheckUSBRuntime, session.CheckVerified, locales.Key("dynCompositeVerified"), locales.Text{}, session.GateUSBHID)
			input = newCheck(session.CheckInputAdapter, session.CheckVerified, locales.Key("dynCompositeInput"), locales.Text{}, "")
		} else {
			input = newCheck(session.CheckInputAdapter, session.RequiresAttention, locales.Key("dynCompositeUnavailable"), locales.Key("dynCompositeAction"), "")
			usb = newCheck(session.CheckUSBRuntime, session.RequiresAttention, locales.Key("dynCompositeUnavailable"), locales.Key("dynCompositeAction"), session.GateUSBHID)
		}
	}
	checks = append(checks, usb, input)
	if keyboardReady {
		observed = session.ObserveKeyboardEnumeration(observed, run.Transport.Controller, p.keyboard.EnumeratedController())
	}
	return append(checks, observed...), nil
}

func (p productionPlatform) ExportDiagnostics(ctx context.Context, snapshot session.Snapshot) (string, error) {
	return (diagnostics.Store{Support: p.options.Support}).Export(ctx, snapshot)
}

func (p productionPlatform) ReleaseInterrupted(ctx context.Context) error {
	return (diagnostics.Store{Support: p.options.Support}).ReleaseInterrupted(ctx)
}

var _ session.PlatformServices = productionPlatform{}

// PackagedSelfTest verifies integrity, Node, and bridge without console or USB access.
func PackagedSelfTest() error {
	assets, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return err
	}
	m, err := manifest.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := engine.SelfTest(ctx, assets, mygo.App.Version(), m); err != nil {
		return err
	}
	fmt.Println("any2jb self-test passed: integrity, Node and bridge; no console or USB access")
	return nil
}
