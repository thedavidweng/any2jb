package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/launcher"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/savekit"
	"github.com/thedavidweng/any2jb/session"
)

// launcherPanel is the launcher-installer section's own ephemeral state. The
// prepared package lives in a private folder the manager prepares again at run
// time; the preview digest binds this confirmation to exactly those files.
type launcherPanel struct {
	pkg          string
	account      string
	version      string
	physical     bool
	exportFolder string
	source       string
	preview      *savekit.Preview
	previewed    string
	busy         bool
	failed       bool
	confirm      bool
	update       func(func())
}

// launcherLabel names one pinned package. The ids come from the canonical
// manifest, so an unrecorded id has no label and no control.
func launcherLabel(id string) string {
	switch id {
	case "launcher-seedsow-demo":
		return locales.LauncherSeedsowDemo()
	case "launcher-seedsow-full":
		return locales.LauncherSeedsowFull()
	case "launcher-nora2-demo":
		return locales.LauncherNora2Demo()
	case "launcher-nora1-demo":
		return locales.LauncherNora1Demo()
	case "launcher-hamidashi-demo":
		return locales.LauncherHamidashiDemo()
	case "launcher-lua-editor":
		return locales.LauncherLuaEditor()
	case "launcher-lua-editor-pro":
		return locales.LauncherLuaEditorPro()
	case "launcher-balatro":
		return locales.LauncherBalatro()
	case "launcher-balatro-showcase":
		return locales.LauncherBalatroShowcase()
	default:
		return locales.LauncherGravityCircuit()
	}
}

// launcherSection renders the gated launcher installers. Every install is one
// SaveKit write: the pinned package is digest-verified and previewed, the
// supported game, edition, version and account are validated, and the write
// gate takes and verifies a backup before anything changes.
func (a *app) launcherSection(c *ui.Context, s session.Snapshot, state retained.State, ready bool) {
	p := &a.launcherPanel
	ui.Text(c, locales.LauncherSection()).FontSize(16).Bold()
	ui.Text(c, locales.LauncherIntro()).TextColor(c.Theme().TextMuted)
	ui.Text(c, locales.LauncherRestrictions()).TextColor(c.Theme().TextMuted)
	ui.Text(c, locales.LauncherSideEffects())
	ui.Text(c, locales.LauncherExistingUntouched()).TextColor(c.Theme().TextMuted)

	labels := make([]string, 0, len(launcher.IDs(a.manifest)))
	ids := map[string]string{}
	for _, id := range launcher.IDs(a.manifest) {
		label := launcherLabel(id)
		labels = append(labels, label)
		ids[label] = id
	}
	if p.pkg == "" {
		p.pkg = labels[0]
	}
	ui.Select(c.Key("launcher-package"), &p.pkg, labels).Label(locales.LauncherPackage())
	id := ids[p.pkg]
	component, err := a.manifest.ToolComponent(id)
	if err != nil || component.Launcher == nil {
		return
	}

	reason := ""
	if a.tools != nil {
		reason = a.tools.Unavailable(id, s.Profile.ID)
	}
	if reason != "" {
		ui.Text(c.Key("launcher-reason"), localize(reason)).TextColor(c.Theme().TextMuted)
	}

	if len(component.Launcher.Versions) > 0 {
		ui.Select(c.Key("launcher-version"), &p.version, component.Launcher.Versions).
			Label(locales.LauncherVersion())
	}
	if component.Launcher.PhysicalCompatibility {
		ui.Checkbox(c.Key("launcher-physical"), &p.physical, locales.LauncherPhysicalCartridge())
	}
	if component.Launcher.FullGame {
		ui.Text(c, locales.LauncherFullGameHint()).TextColor(c.Theme().TextMuted)
		ui.TextInput(c.Key("launcher-fullgame-export"), &p.exportFolder).
			Label(locales.LauncherFullGameExport())
	}

	ui.Text(c, locales.LauncherAccountHint()).TextColor(c.Theme().TextMuted)
	accounts := launcherAccounts(state)
	if len(accounts) == 1 {
		// One console account owns every save, so there is nothing to choose.
		p.account = accounts[0]
	}
	if len(accounts) > 0 {
		ui.Select(c.Key("launcher-account"), &p.account, accounts).Label(locales.LauncherAccount())
	}

	request := launcher.Request{ID: id, Version: p.version,
		PhysicalCompatibility: p.physical, BackupFolder: strings.TrimSpace(p.exportFolder)}
	target := savekit.Request{TitleID: component.Launcher.TitleID, UID: p.account}
	// A changed package, account, declaration or export folder invalidates both
	// the prepared preview and the confirmation it was made for.
	selection := fmt.Sprintf("%s:%s:%s:%t:%s", id, p.account, p.version, p.physical, request.BackupFolder)
	if p.previewed != selection {
		p.preview, p.source, p.confirm, p.previewed = nil, "", false, selection
	}
	usable := ready && reason == "" && !p.busy && a.tools != nil && p.update != nil &&
		launcher.Validate(request, component, target) == nil
	if component.Launcher.FullGame && request.BackupFolder == "" {
		usable = false
	}
	if ui.Button(c.Key("launcher-preview"), locales.LauncherPreview()).Disabled(!usable).Clicked() {
		a.previewLauncher(request, target)
	}
	if p.failed {
		ui.Text(c.Key("launcher-failed"), locales.LauncherPreviewFailed()).Role(ui.RoleStatus)
	}
	if p.preview != nil {
		ui.Text(c, locales.SaveKitPreviewFiles(fmt.Sprint(len(p.preview.Files)),
			fmt.Sprint(p.preview.Bytes)))
		for _, file := range p.preview.Files {
			ui.Text(c, file.Path+"  "+fmt.Sprint(file.Size)).Selectable()
		}
		ui.Text(c, locales.SaveKitWriteTarget(target.TitleID, target.UID, "1", "1", "0", "0")).Selectable()
		ui.Checkbox(c.Key("launcher-confirm"), &p.confirm, locales.LauncherConfirm())
	}
	write := savekit.Request{Action: "write", TitleID: target.TitleID, UID: target.UID,
		SourceFolder: p.source, AllowReplace: true, Confirm: p.confirm}
	if p.preview != nil {
		write.PreviewDigest = p.preview.Digest
	}
	if ui.Button(c.Key("launcher-install"), locales.LauncherInstall()).
		Disabled(!usable || write.Validate() != nil).Clicked() {
		a.installLauncher(write, request)
	}
}

// launcherAccounts offers the console accounts the read-only listing reported.
func launcherAccounts(state retained.State) []string {
	if state.SaveKit == nil {
		return nil
	}
	return state.SaveKit.Users
}

// previewLauncher prepares the pinned package off the UI thread: it verifies
// recorded digests, unpacks into a private folder and hashes every file.
func (a *app) previewLauncher(request launcher.Request, target savekit.Request) {
	p := &a.launcherPanel
	if p.busy || p.update == nil || a.tools == nil {
		return
	}
	p.busy, p.failed, p.preview, p.source, p.confirm = true, false, nil, "", false
	tools, update := a.tools, p.update
	go func() {
		source, preview, err := tools.PrepareLauncher(context.Background(), request, target)
		update(func() {
			p.busy, p.failed = false, err != nil
			if err == nil {
				p.preview, p.source = &preview, source
			}
		})
	}()
}

// installLauncher runs the install as one gated SaveKit write. The manager
// prepares the package again and rebinds the confirmed preview to it.
func (a *app) installLauncher(write savekit.Request, request launcher.Request) {
	a.command(a.controller.RunToolWith(session.ToolRequest{Tool: "savekit",
		SaveKit: &write, Launcher: &request}))
	p := &a.launcherPanel
	p.failed = a.commandRejected
	p.confirm, p.preview, p.source = false, nil, ""
}

var _ = manifest.Component{}
