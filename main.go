package main

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/civswitch"
	"github.com/thedavidweng/any2jb/diagnostics"
	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/internal/devlaunch"
	"github.com/thedavidweng/any2jb/internal/sessionlock"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

type app struct {
	flow               flowUI
	bootstrapMu        sync.Mutex
	developerStaged    bool
	startupError       string
	bootstrapReuse     bool
	bootstrapError     bool
	allowOverwrite     bool
	keyboard           keyboardPanel
	usbConfirmation    usbPrompt
	civSwitchPanel     civSwitchPanel
	toolsPanel         toolsPanel
	homebrewPanel      homebrewPanel
	saveKit            saveKitPanel
	tools              toolAdapter
	launcherPanel      launcherPanel
	driverPanel        driverPanel
	about              bool
	controller         *session.SessionController
	manual             *transport.Manual
	restart            restartUI
	commandRejected    bool
	commandInstruction string
	manifest           *manifest.Manifest
	preferences        preferences
	preferencesPath    string
	developer          bool
	preferenceError    string
	diagnostic         diagnosticUI
}

func (a *app) view(c *ui.Context) {
	if a.controller != nil {
		a.keyboard.observe(a.controller.Snapshot())
	}
	applyTheme(c)
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Gap(8).Padding(12, 20).Background(cardFill(c)).Children(func() {
			ui.Column(c).Gap(1).Grow(1).Children(func() {
				ui.Text(c, locales.AppName()).FontSize(c.Theme().Rem(1.35)).Bold()
				muted(c, locales.AppDescriptionShort()).FontSize(c.Theme().Rem(0.85)).SingleLine()
			})
			if ui.Button(c.Key("diagnostics-open"), locales.Diagnostics()).Clicked() {
				a.stopKeyboard()
				a.flow.diagnostics = !a.flow.diagnostics
				a.flow.settings = false
			}
			if ui.Button(c.Key("settings-open"), locales.Settings()).Clicked() {
				a.stopKeyboard()
				a.flow.method = ""
				a.flow.settings = !a.flow.settings
				a.flow.diagnostics = false
			}
		})
		rule(c)
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(20, 20, 24).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(contentWidth).Gap(16).Children(func() { a.body(c) })
			})
		})
		if a.inFlow() {
			a.footerView(c)
		}
	})
}

func (a *app) inFlow() bool {
	return !a.flow.diagnostics && !a.driverPanel.open && !a.flow.settings && a.controller != nil
}

func (a *app) body(c *ui.Context) {
	if a.developerStaged {
		ui.Text(c, locales.DeveloperStaged()).Bold().TextColor(c.Theme().Warning)
	}
	if a.startupError != "" {
		ui.Text(c.Key("startup-error"), a.startupError).Role(ui.RoleStatus).Bold().TextColor(c.Theme().Danger)
	}
	switch {
	case a.flow.diagnostics:
		ui.Text(c, locales.Diagnostics()).FontSize(c.Theme().Rem(1.55)).Bold()
		card(c, func() { a.diagnosticsView(c) })
	case a.driverPanel.open:
		card(c, func() { a.driverView(c) })
	case a.flow.settings:
		a.settingsView(c)
	case a.controller == nil:
		ui.Text(c, locales.NoSession())
	default:
		a.flowView(c)
	}
}
func main() {
	if len(os.Args) == 2 && os.Args[1] == "--self-test" {
		if err := packagedSelfTest(); err != nil {
			log.Fatal(err)
		}
		return
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	a, err := loadApp(filepath.Join(dir, "any2jb", "preferences.json"))
	if err != nil {
		log.Fatal(err)
	}
	assets, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		log.Fatal(err)
	}
	support, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	release, err := sessionlock.Acquire(filepath.Join(support, "session.lock"))
	if err != nil {
		log.Fatal(err)
	}
	defer release()
	options := engine.Options{Assets: assets, Support: support, Version: mygo.App.Version(), Manifest: a.manifest}
	// This is the only developer launch input; it is read only when the build
	// may use it.
	developer, err := devlaunch.Load(os.Getenv("ANY2JB_DEVELOPER_CONFIG"))
	if err != nil {
		log.Fatal("developer launch configuration: ", err)
	}
	if developer.SourceRoot != "" {
		options.Assets = developer.Assets
		options.DeveloperSourceRoot, options.DeveloperProfileID = developer.SourceRoot, a.preferences.Profile
		a.developerStaged = true
	}
	var helpers *transport.DeveloperHelpers
	defer func() {
		if helpers != nil {
			helpers.Close()
		}
	}()
	a.driverPanel.gate = productionDriverGate(options)
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     locales.AppName(),
			Width:     640,
			Height:    720,
			MinWidth:  560,
			MinHeight: 360,
			StateKey:  "main",
			Content:   ui.View(a.view),
		})
		mygo.App.OnLocaleChanged(func(string) {
			win.SetTitle(locales.AppName())
		})
		win.OnBlur(a.stopKeyboard)
		closed := make(chan struct{})
		win.OnClosed(func() { a.stopKeyboard(); close(closed) })
		inputOptions := transport.DarwinOptions{Timing: transport.DefaultTiming(), Confirm: a.usbConfirmation.Wait}
		if a.preferences.USB != nil {
			inputOptions.Controller = a.preferences.USB.Controller
			inputOptions.Timing = a.preferences.USB.Timing
		}
		go func() {
			civOptions := civswitch.Options{DeveloperInstallerPython: developer.InstallerPython}
			if developer.SourceRoot != "" {
				built, err := transport.BuildDeveloperHelpers(context.Background(), developer.SourceRoot, support, a.manifest, io.Discard)
				if err != nil {
					win.Update(func() { a.startupError = locales.DeveloperStagingFailed(err.Error()) })
					return
				}
				inputOptions.Helpers = built
				if civOptions.DeveloperHelperPath, civOptions.DeveloperHelperSHA256, err = built.CompositeHelper(); err != nil {
					win.Update(func() { a.startupError = locales.DeveloperStagingFailed(err.Error()) })
					return
				}
			}
			env := productionEnvironmentWithCivSwitch(options, inputOptions, civOptions)
			store := diagnostics.Store{Support: support}
			interrupted, recoveryErr := store.Interrupted()
			win.Update(func() {
				helpers = inputOptions.Helpers
				if err := a.initializeSession(env); err != nil {
					a.commandRejected = true
					return
				}
				if interrupted || recoveryErr != nil {
					a.controller.MarkInterrupted()
				}
				a.startDiagnostics(win, store)
				a.toolsPanel.update = win.Update
				a.launcherPanel.update = win.Update
				a.saveKit.update = win.Update
				a.driverPanel.update = win.Update
				a.restart.update = win.Update
				if recoveryErr != nil {
					a.diagnostic.failed = true
				}
				controller, manual := a.controller, a.manual
				// This watcher owns disk writes off the UI thread. No readiness,
				// pairing details or console data enter the identity file.
				path := a.bootstrapPath()
				initialRecord := controller.Snapshot().SavedBootstrap
				go func() {
					last := initialRecord
					// Flush a completed confirmation even if closing the window
					// wins over its final redraw notification.
					defer func() {
						s := controller.Snapshot()
						if s.BootstrapSaved && s.SavedBootstrap != last {
							a.persistBootstrap(path)
						}
					}()
					refreshSession(closed, controller.Changes(), manual.Changes(), func() {
						s := controller.Snapshot()
						failed := false
						if s.BootstrapSaved && s.SavedBootstrap != last {
							failed = a.persistBootstrap(path) != nil
							last = s.SavedBootstrap
						}
						win.Update(func() {
							a.bootstrapError = a.bootstrapError || failed
						})
					})
				}()
			})
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
