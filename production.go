package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/thedavidweng/any2jb/civswitch"
	"github.com/thedavidweng/any2jb/diagnostics"
	"github.com/thedavidweng/any2jb/driver"
	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/readiness"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

// productionEnvironment is the single production adapter composition point.
// Call off the UI thread: Stage performs file integrity checks and disk I/O.
// Packaged startup never supplies DeveloperSourceRoot or DeveloperProfileID.
func productionEnvironment(options engine.Options) session.Environment {
	root, err := engine.Stage(options)
	host := engine.NewHostRunner(options)
	tools := productionTools(root, err, options, host)
	// The manual adapter never sends USB input. A verified Darwin adapter can be
	// composed here separately when its readiness gates and sender are available.
	return session.Environment{
		CivSwitch: civswitch.New(civswitch.Options{Manifest: options.Manifest}),
		Host:      host,
		Retained:  tools,
		Input: transport.NewManual(func(run session.Run) (string, error) {
			if err != nil {
				return "", err
			}
			if verifyErr := options.Manifest.VerifyProfile(root, run.Profile.ID); verifyErr != nil {
				return "", verifyErr
			}
			profile, profileErr := options.Manifest.Profile(run.Profile.ID)
			if profileErr != nil {
				return "", profileErr
			}
			source, readErr := os.ReadFile(filepath.Join(root, profile.Bootstrap))
			return string(source), readErr
		}),
		Platform: productionPlatform{root: root, options: options, stageError: err},
		Tools:    tools,
	}
}

// productionTools composes the retained lease and read-only tool adapter. Tool
// sources are only staged from an explicit developer source root; a packaged app
// stages nothing and every entry keeps the manifest's unavailable reason.
func productionTools(root string, stageError error, options engine.Options, host *engine.HostRunner) *retained.Manager {
	toolRoot, staged, stageToolsError := retained.StageTools(retained.StageOptions{Support: options.Support,
		Version: options.Version, SourceRoot: options.DeveloperSourceRoot, Manifest: options.Manifest,
		Components: []string{"ouncecake", "sd-browser"}})
	node, toolHost := "", ""
	if stageError == nil {
		node, toolHost = filepath.Join(root, "runtime/node"), filepath.Join(root, "runtime/toolhost.cjs")
	}
	return retained.New(retained.Options{Support: options.Support, Node: node, Host: toolHost,
		ToolRoot: toolRoot, Staged: staged, StageError: stageToolsError,
		EngineStaged: options.DeveloperSourceRoot != "" && options.DeveloperProfileID != "",
		Manifest:     options.Manifest, Runs: host})
}

// productionDriverGate uses the packaged resource root; packaged startup never
// opts into a developer driver source.
func productionDriverGate(options engine.Options) *driver.Gate {
	return driver.New(driver.Options{Assets: options.Assets, Manifest: options.Manifest})
}

type productionPlatform struct {
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
	usb := session.Check{ID: "usb-runtime", GatesTransport: "usb-hid", State: session.RequiresAttention,
		Message: "Verified USB helpers are not available.", NextAction: "USB input requires permitted, integrity-verified helper binaries. Guided manual input does not require these helpers."}
	if p.stageError == nil && engine.VerifyUSB(p.root, p.options.Manifest) == nil {
		usb.State, usb.Message, usb.NextAction = session.CheckVerified, "USB helper integrity verified; enumeration is not verified.", ""
	}
	input := session.Check{ID: "input-adapter", State: session.RequiresAttention,
		Message: locales.AutomatedInputUnavailable(), NextAction: locales.SelectGuidedManual()}
	if run.Transport.Capabilities.Manual {
		input.State, input.Message, input.NextAction = session.CheckVerified, locales.ManualInputAvailable(), ""
	}
	checks = append(checks, usb, input)
	observed, err := readiness.New().Inspect(ctx, run)
	if err != nil {
		return nil, err
	}
	return append(checks, observed...), nil
}

func (p productionPlatform) ExportDiagnostics(ctx context.Context, snapshot session.Snapshot) (string, error) {
	return (diagnostics.Store{Support: p.options.Support}).Export(ctx, snapshot)
}
func (p productionPlatform) ReleaseInterrupted(ctx context.Context) error {
	return (diagnostics.Store{Support: p.options.Support}).ReleaseInterrupted(ctx)
}
func (productionPlatform) Credentials() session.CredentialStore { return unavailableCredentials{} }

type unavailableCredentials struct{}

func (unavailableCredentials) Get(context.Context, string) ([]byte, error) {
	return nil, errors.New("credential storage is not available")
}
func (unavailableCredentials) Set(context.Context, string, []byte) error {
	return errors.New("credential storage is not available")
}
func (unavailableCredentials) Delete(context.Context, string) error {
	return errors.New("credential storage is not available")
}

var _ session.PlatformServices = productionPlatform{}
