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
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

type app struct {
	bootstrapMu        sync.Mutex
	developerStaged    bool
	bootstrapReuse     bool
	bootstrapError     bool
	allowOverwrite     bool
	keyboard           keyboardPanel
	usbConfirmation    usbPrompt
	civSwitchPanel     civSwitchPanel
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
	if a.keyboard.forwarding && (!a.keyboard.open || a.driverPanel.open || a.civSwitchPanel.open) {
		a.stopKeyboard()
	}
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Gap(16).Padding(24).Children(func() {
			ui.Text(c, locales.AppName()).FontSize(28).Bold()
			ui.Text(c, locales.AppDescription()).TextColor(c.Theme().TextMuted)
			if a.developerStaged {
				ui.Text(c, locales.DeveloperStaged()).Bold()
			}
			if a.driverPanel.open {
				a.driverView(c)
				return
			}
			if a.controller != nil && a.civSwitchPanel.open {
				a.civSwitchView(c)
				return
			}
			if a.controller != nil {
				s := a.controller.Snapshot()
				if ui.Button(c.Key("civ-open"), locales.CivTitle()).Disabled(s.Busy || s.SideEffects || s.Stage == session.Indeterminate).Clicked() {
					a.civSwitchPanel.open = true
				}
			}
			if a.manifest != nil {
				a.profileView(c)
			}
			if a.controller == nil {
				ui.Text(c, locales.NoSession())
			} else {
				a.sessionView(c)
				a.diagnosticsView(c)
			}
			if a.about {
				ui.Text(c, locales.BuildLimitations())
			}
			if ui.Button(c.Key("about"), locales.AboutBuild()).Clicked() {
				a.about = !a.about
			}
		})
	})
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
	options := engine.Options{Assets: assets, Support: support, Version: mygo.App.Version(), Manifest: a.manifest}
	// This is the only developer launch input. Production builds and packaged
	// development bundles do not even read the environment variable.
	var developer developerLaunch
	if mygo.IsDev() && !mygo.App.IsPackaged() {
		developer, err = loadDeveloperLaunch(os.Getenv("ANY2JB_DEVELOPER_CONFIG"), true)
		if err != nil {
			log.Fatal("developer launch configuration: ", err)
		}
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
				var err error
				built, buildErr := transport.BuildDeveloperHelpers(context.Background(), developer.SourceRoot, support, a.manifest, io.Discard)
				err = buildErr
				if err != nil {
					log.Fatal("developer USB helper staging: ", err)
				}
				inputOptions.Helpers = built
				civOptions.DeveloperHelperPath, civOptions.DeveloperHelperSHA256, err = built.CompositeHelper()
				if err != nil {
					log.Fatal("developer composite helper staging: ", err)
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
