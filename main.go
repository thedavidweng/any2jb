package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

type app struct {
	driverPanel        driverPanel
	about              bool
	controller         *session.SessionController
	manual             *transport.Manual
	commandRejected    bool
	commandInstruction string
	manifest           *manifest.Manifest
	preferences        preferences
	preferencesPath    string
	developer          bool
	preferenceError    string
}

func (a *app) view(c *ui.Context) {
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Gap(16).Padding(24).Children(func() {
			ui.Text(c, locales.AppName()).FontSize(28).Bold()
			ui.Text(c, locales.AppDescription()).TextColor(c.Theme().TextMuted)
			if a.driverPanel.open {
				a.driverView(c)
				return
			}
			if a.manifest != nil {
				a.profileView(c)
			}
			if a.controller == nil {
				ui.Text(c, locales.NoSession())
			} else {
				a.sessionView(c)
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
		closed := make(chan struct{})
		win.OnClosed(func() { close(closed) })
		go func() {
			env := productionEnvironment(options)
			win.Update(func() {
				if err := a.initializeSession(env); err != nil {
					a.commandRejected = true
					return
				}
				controller, manual := a.controller, a.manual
				go refreshSession(closed, controller.Changes(), manual.Changes(), func() { win.Update(func() {}) })
			})
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
