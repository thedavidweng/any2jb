package main

import (
	"context"
	"errors"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/readiness"
	"github.com/thedavidweng/any2jb/session"
)

// productionEnvironment is the single production adapter composition point.
// Call off the UI thread: Stage performs file integrity checks and disk I/O.
// Packaged startup never supplies DeveloperSourceRoot or DeveloperProfileID.
func productionEnvironment(options engine.Options) session.Environment {
	root, err := engine.Stage(options)
	return session.Environment{
		Host:     engine.NewHostRunner(options),
		Input:    unavailableInput{root: root, options: options},
		Platform: productionPlatform{root: root, options: options, stageError: err},
		Tools:    unavailableTools{},
	}
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
	checks = append(checks, usb, session.Check{ID: "input-adapter", State: session.RequiresAttention,
		Message: "The input workflow is not integrated.", NextAction: "Connect the manual or USB input adapter before launching."})
	observed, err := readiness.New().Inspect(ctx, run)
	if err != nil {
		return nil, err
	}
	return append(checks, observed...), nil
}

func (productionPlatform) ExportDiagnostics(context.Context, session.Snapshot) (string, error) {
	return "", errors.New("diagnostics export is not available")
}
func (productionPlatform) Credentials() session.CredentialStore { return unavailableCredentials{} }

type unavailableInput struct {
	root    string
	options engine.Options
}

func (i unavailableInput) Send(ctx context.Context, run session.Run, action session.InputAction, host session.PreparedHost, report session.Report) error {
	if !run.Transport.Capabilities.Manual {
		if err := engine.VerifyUSB(i.root, i.options.Manifest); err != nil {
			return err
		}
	}
	return errors.New("input adapter is not available; no commands were sent")
}
func (unavailableInput) Release(context.Context, session.Run) error { return nil }

type unavailableTools struct{}

func (unavailableTools) Run(context.Context, session.Run, string, session.Report) error {
	return errors.New("tool adapter is not available")
}
func (unavailableTools) Stop(context.Context, session.Run) error { return nil }

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
