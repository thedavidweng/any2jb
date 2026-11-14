package ui

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/thedavidweng/any2jb/locales"
)

const officialGitHubURL = "https://github.com/thedavidweng/any2jb"

func (a *app) aboutView(c *ui.Context) {
	ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Text(c, locales.AboutTitle()).FontSize(c.Theme().Rem(1.55)).Bold()
		grow(c)
		inline(ui.Link(c.Key("about-github"), locales.AboutOpenGitHub(), officialGitHubURL))
	})
	card(c, func() {
		markdown.View(c, locales.AboutMarkdown(mygo.App.Version()), &a.aboutMD)
	})
}
