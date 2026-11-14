package ui

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	appstate "github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/civswitch"
	"github.com/thedavidweng/any2jb/internal/devlaunch"
	"github.com/thedavidweng/any2jb/internal/diagnostics"
	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/sessionlock"
	"github.com/thedavidweng/any2jb/internal/transport"
	"github.com/thedavidweng/any2jb/locales"
)

type app struct {
	appstate.State
	flow               flowUI
	startupError       string
	bootstrapReuse     bool
	bootstrapError     bool
	allowOverwrite     bool
	keyboard           keyboardPanel
	civSwitchPanel     civSwitchPanel
	toolsPanel         toolsPanel
	homebrewPanel      homebrewPanel
	pluginsPanel       pluginsPanel
	utilitiesPanel     utilitiesPanel
	saveKit            saveKitPanel
	launcherPanel      launcherPanel
	driverPanel        driverPanel
	about              bool
	aboutMD            markdown.Options
	applyLocale        func(string)
	restart            restartUI
	commandRejected    bool
	commandInstruction string
	developer          bool
	preferenceError    string
	diagnostic         diagnosticUI
}

func loadApp(path string) (*app, error) {
	a := &app{}
	if err := a.Init(path); err != nil {
		return nil, err
	}
	return a, nil
}

// initializeSession is called once real adapters are available, never with a
// production fake. Preferences restore choices, not observed readiness.
func (a *app) initializeSession(env session.Environment) error {
	reject, err := a.InitializeSession(env)
	if err != nil {
		return err
	}
	if a.KeyboardInput != nil {
		a.keyboard.input = a.KeyboardInput
	}
	if reject != nil {
		a.command(a.Controller.Snapshot(), reject)
	}
	return nil
}

// openSession starts the session the window shows. An interrupted run needs
// a console restart first; otherwise this host is checked once, read-only, so
// the first stage opens on observations instead of an empty list.
func (a *app) openSession(env session.Environment, interrupted bool) error {
	if err := a.initializeSession(env); err != nil {
		return err
	}
	if interrupted {
		a.Controller.MarkInterrupted()
		return nil
	}
	a.command(a.Controller.Inspect())
	return nil
}

func (a *app) languageButtonLabel() string {
	pref := a.Preferences.Language
	if pref == "" {
		return locales.LanguageLabel() + ": " + locales.LanguageSystem()
	}
	for _, opt := range locales.Switch2Languages {
		if opt.Tag == pref {
			return locales.LanguageLabel() + ": " + opt.Autonym
		}
	}
	return locales.LanguageLabel() + ": " + pref
}

func (a *app) setLanguage(tag string) {
	norm, ok := locales.NormalizeLanguagePreference(tag)
	if !ok {
		return
	}
	a.Preferences.Language = norm
	if a.PreferencesPath != "" {
		_ = a.SavePreferences()
	}
	if a.applyLocale != nil {
		a.applyLocale(norm)
	}
}

func (a *app) toggleTopPanel(panel string) {
	a.stopKeyboard()
	nextPlugins := panel == "plugins" && !a.flow.plugins
	nextUtilities := panel == "utilities" && !a.flow.utilities
	nextDiagnostics := panel == "diagnostics" && !a.flow.diagnostics
	nextSettings := panel == "settings" && !a.flow.settings
	nextAbout := panel == "about" && !a.flow.aboutPage
	a.flow.plugins = nextPlugins
	a.flow.utilities = nextUtilities
	a.flow.diagnostics = nextDiagnostics
	a.flow.settings = nextSettings
	a.flow.aboutPage = nextAbout
	a.driverPanel.open = false
}

func (a *app) view(c *ui.Context) {
	if a.Controller != nil {
		a.keyboard.observe(a.Controller.Snapshot())
	}
	applyTheme(c)
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Padding(12, 20).Background(cardFill(c)).Children(func() {
			ui.Column(c).Gap(1).Grow(1).Children(func() {
				ui.Text(c, locales.AppName()).FontSize(c.Theme().Rem(1.35)).Bold()
				muted(c, locales.AppDescriptionShort()).FontSize(c.Theme().Rem(0.85)).SingleLine()
			})
			ui.PopoverMenuButton(c.Key("language-select"), a.languageButtonLabel(), func(m *ui.Menu) {
				if m.Item(locales.LanguageSystem()).Radio(a.Preferences.Language == "").Chosen() {
					a.setLanguage("")
				}
				m.Separator()
				for _, opt := range locales.Switch2Languages {
					tag := opt.Tag
					if m.Item(opt.Autonym).Radio(a.Preferences.Language == tag).Chosen() {
						a.setLanguage(tag)
					}
				}
			})
			if ui.Button(c.Key("plugins-open"), locales.PluginsTitle()).Clicked() {
				a.toggleTopPanel("plugins")
			}
			if ui.Button(c.Key("utilities-open"), locales.UtilTitle()).Clicked() {
				a.toggleTopPanel("utilities")
			}
			if ui.Button(c.Key("diagnostics-open"), locales.Diagnostics()).Clicked() {
				a.toggleTopPanel("diagnostics")
			}
			if ui.Button(c.Key("settings-open"), locales.Settings()).Clicked() {
				a.toggleTopPanel("settings")
			}
			if ui.Button(c.Key("about-open"), locales.AboutTitle()).Clicked() {
				a.toggleTopPanel("about")
			}
		})
		rule(c)
		flow := a.inFlow()
		if flow {
			a.syncStage()
		}
		// A zero minimum lets the stage scroll inside the window instead of
		// growing past it and pushing the footer out of view.
		ui.Row(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			if flow {
				sidebar(c, a.flow.stage, flowTitlesFor(a.Controller.Snapshot().Checks))
				ui.Column(c).Width(1).FillHeight().Shrink(0).Background(c.Theme().Border)
			}
			ui.Scroll(c).Fill().Children(func() {
				ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(20, 20, 24).Children(func() {
					ui.Column(c).FillWidth().MaxWidth(contentWidth).Gap(16).Children(func() { a.body(c) })
				})
			})
		})
		if flow {
			a.footerView(c)
		}
	})
}

func (a *app) inFlow() bool {
	return !a.flow.plugins && !a.flow.utilities && !a.flow.diagnostics && !a.driverPanel.open && !a.flow.settings && !a.flow.aboutPage && a.Controller != nil
}

func (a *app) body(c *ui.Context) {
	if a.DeveloperStaged {
		ui.Text(c, locales.DeveloperStaged()).Bold().TextColor(c.Theme().Warning)
	}
	if a.startupError != "" {
		ui.Text(c.Key("startup-error"), a.startupError).Role(ui.RoleStatus).Bold().TextColor(c.Theme().Danger)
	}
	switch {
	case a.flow.plugins:
		a.pluginsView(c)
	case a.flow.utilities:
		a.utilitiesView(c)
	case a.flow.diagnostics:
		ui.Text(c, locales.Diagnostics()).FontSize(c.Theme().Rem(1.55)).Bold()
		card(c, func() { a.diagnosticsView(c) })
	case a.driverPanel.open:
		card(c, func() { a.driverView(c) })
	case a.flow.settings:
		a.settingsView(c)
	case a.flow.aboutPage:
		a.aboutView(c)
	case a.Controller == nil:
		ui.Text(c, locales.NoSession())
	default:
		a.flowView(c)
	}
}

// Run launches the native GUI application.
func Run() error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	a, err := loadApp(filepath.Join(dir, "any2jb", "preferences.json"))
	if err != nil {
		return err
	}
	assets, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return err
	}
	support, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return err
	}
	release, err := sessionlock.Acquire(support)
	if err != nil {
		return err
	}
	defer release()
	options := engine.Options{Assets: assets, Support: support, Version: mygo.App.Version(), Manifest: a.Manifest}
	// This is the only developer launch input; it is read only when the build
	// may use it.
	developer, err := devlaunch.Load(os.Getenv("ANY2JB_DEVELOPER_CONFIG"))
	if err != nil {
		return err
	}
	if developer.SourceRoot != "" {
		options.Assets = developer.Assets
		options.DeveloperSourceRoot, options.DeveloperProfileID = developer.SourceRoot, a.Preferences.Profile
		a.DeveloperStaged = true
	}
	var helpers *transport.DeveloperHelpers
	defer func() {
		if helpers != nil {
			helpers.Close()
		}
	}()
	a.driverPanel.gate = appstate.ProductionDriverGate(options)
	mygo.App.WhenReady(func() {
		a.applyLocale = mygo.App.SetLocale
		mygo.App.SetLocale(a.Preferences.Language)
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     locales.AppName(),
			Width:     760,
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
		win.OnClosed(func() { a.stopKeyboard(); a.stopUtilitySessions(); close(closed) })
		inputOptions := transport.DarwinOptions{Timing: transport.DefaultTiming(), Confirm: a.USBConfirmation.Wait}
		if a.Preferences.USB != nil {
			inputOptions.Controller = a.Preferences.USB.Controller
			inputOptions.Timing = a.Preferences.USB.Timing
		}
		go func() {
			civOptions := civswitch.Options{DeveloperInstallerPython: developer.InstallerPython}
			if developer.SourceRoot != "" && runtime.GOOS == "darwin" {
				built, err := transport.BuildDeveloperHelpers(context.Background(), developer.SourceRoot, support, a.Manifest, io.Discard)
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
			env := appstate.ProductionEnvironmentWithCivSwitch(options, inputOptions, civOptions)
			store := diagnostics.Store{Support: support}
			interrupted, recoveryErr := store.Interrupted()
			win.Update(func() {
				helpers = inputOptions.Helpers
				if err := a.openSession(env, interrupted || recoveryErr != nil); err != nil {
					a.commandRejected = true
					return
				}
				a.startDiagnostics(win, store)
				a.pluginsPanel.update = win.Update
				a.utilitiesPanel.update = win.Update
				a.toolsPanel.update = win.Update
				a.launcherPanel.update = win.Update
				a.saveKit.update = win.Update
				a.driverPanel.update = win.Update
				a.restart.update = win.Update
				if recoveryErr != nil {
					a.diagnostic.failed = true
				}
				controller, manual := a.Controller, a.Manual
				// This watcher owns disk writes off the UI thread. No readiness,
				// pairing details or console data enter the identity file.
				path := a.BootstrapPath()
				initialRecord := controller.Snapshot().SavedBootstrap
				go func() {
					last := initialRecord
					// Flush a completed confirmation even if closing the window
					// wins over its final redraw notification.
					defer func() {
						s := controller.Snapshot()
						if s.BootstrapSaved && s.SavedBootstrap != last {
							a.PersistBootstrap(path)
						}
					}()
					appstate.RefreshSession(closed, controller.Changes(), manual.Changes(), func() {
						s := controller.Snapshot()
						failed := false
						if s.BootstrapSaved && s.SavedBootstrap != last {
							failed = a.PersistBootstrap(path) != nil
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
	return mygo.App.Run()
}
