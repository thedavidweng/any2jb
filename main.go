package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
)

type app struct {
	about bool
}

func (a *app) view(c *ui.Context) {
	ui.Column(c).Fill().Center().Gap(16).Padding(24).Children(func() {
		ui.Text(c, locales.AppName()).FontSize(28).Bold()
		ui.Text(c, locales.AppDescription()).TextColor(c.Theme().TextMuted)
		ui.Text(c, locales.NoSession())
		if a.about {
			ui.Text(c, locales.BuildLimitations())
		}
		if ui.Button(c.Key("about"), locales.AboutBuild()).Clicked() {
			a.about = !a.about
		}
	})
}

func main() {
	a := &app{}
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     locales.AppName(),
			Width:     640,
			Height:    400,
			MinWidth:  560,
			MinHeight: 360,
			StateKey:  "main",
			Content:   ui.View(a.view),
		})
		mygo.App.OnLocaleChanged(func(string) {
			win.SetTitle(locales.AppName())
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
