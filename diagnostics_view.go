package main

import (
	"context"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/diagnostics"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
)

type diagnosticUI struct {
	ready, busy, failed, follow, preview bool
	source                               int
	logs                                 diagnostics.Logs
	folder, bundle                       string
	runID                                string
	plan                                 []string
	scroll                               ui.ScrollState
	store                                diagnostics.Store
	update                               func(func())
}

func (a *app) startDiagnostics(win *mygo.Window, store diagnostics.Store) {
	a.diagnostic = diagnosticUI{ready: true, follow: true, store: store, update: win.Update}
	stop := make(chan struct{})
	win.OnClosed(func() { close(stop) })
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				win.Update(func() { a.refreshDiagnostics() })
			}
		}
	}()
}

func (a *app) refreshDiagnostics() {
	d := &a.diagnostic
	if d.busy || !d.ready || a.controller == nil {
		return
	}
	id := a.controller.Snapshot().RunID
	store, update := d.store, d.update
	d.busy = true
	go func() {
		logs, folder, err := store.Refresh(id)
		update(func() {
			d.busy = false
			if a.controller.Snapshot().RunID != id {
				return
			}
			if err != nil {
				d.failed = true
				return
			}
			d.logs, d.folder = logs, folder
			d.runID = id
		})
	}()
}

func (a *app) diagnosticsView(c *ui.Context) {
	d := &a.diagnostic
	if !d.ready {
		return
	}
	ui.Text(c, locales.DiagnosticsPrivacy()).TextColor(c.Theme().TextMuted)
	ui.RadioGroup(c.Key("logs-source"), func() {
		ui.Radio(c.Key("logs-all"), &d.source, -1, locales.LogAll())
		ui.Radio(c.Key("logs-host"), &d.source, 0, locales.LogHost())
		ui.Radio(c.Key("logs-usb"), &d.source, 1, locales.LogUSB())
		ui.Radio(c.Key("logs-pipeline"), &d.source, 2, locales.LogPipeline())
		ui.Radio(c.Key("logs-acquisition"), &d.source, 3, locales.LogAcquisition())
	}).Row().Wrap().Gap(c.Theme().Space(5)).Label(locales.LogSource())
	ui.Checkbox(c.Key("logs-follow"), &d.follow, locales.FollowLogs())
	text := strings.Join([]string{d.logs.Host, d.logs.USB, d.logs.Pipeline, d.logs.Acquisition}, "\n")
	if d.source >= 0 {
		text = []string{d.logs.Host, d.logs.USB, d.logs.Pipeline, d.logs.Acquisition}[d.source]
	}
	currentRun := a.controller.Snapshot().RunID == d.runID
	if !currentRun {
		text = ""
	}
	if text == "" {
		text = locales.NoLogOutput()
	}
	if d.follow {
		d.scroll.Y = math.MaxFloat32
	}
	t := c.Theme()
	ui.Scroll(c.Key("logs-output")).Height(180).FillWidth().Padding(t.Space(2)).Radius(t.Radius).Border(1, t.Border).
		TrackScroll(&d.scroll).Children(func() { ui.Text(c, text).Selectable() })
	ui.Row(c).Gap(8).Children(func() {
		if inline(ui.Button(c.Key("logs-open"), locales.OpenSessionFolder())).Disabled(!currentRun || d.folder == "").Clicked() {
			d.failed = mygo.Shell.OpenPath(d.folder) != nil
		}
		if inline(ui.Button(c.Key("logs-export"), locales.ExportSupportBundle())).Disabled(d.busy || d.update == nil).Clicked() {
			d.busy = true
			controller, update := a.controller, d.update
			go func() {
				path, err := controller.ExportDiagnostics(context.Background())
				update(func() { d.busy = false; d.failed = err != nil; d.bundle = path })
			}()
		}
	})
	ui.Text(c, locales.LocalDataFolders(d.store.Support))
	if d.bundle != "" {
		ui.Text(c, locales.SupportBundleSaved(d.bundle))
		if inline(ui.Button(c.Key("bundle-open"), locales.OpenBundleFolder())).Clicked() {
			d.failed = mygo.Shell.OpenPath(filepath.Dir(d.bundle)) != nil
		}
	}
	s := a.controller.Snapshot()
	active := s.Busy || s.SideEffects || s.Stage == session.Verified || s.Stage == session.RetainedRPC
	if inline(ui.Button(c.Key("cleanup-preview"), locales.PreviewCleanup())).Disabled(active || d.busy || d.update == nil).Clicked() {
		d.busy = true
		store, update := d.store, d.update
		go func() {
			plan, err := store.PreviewCleanup()
			update(func() { d.busy = false; d.failed = err != nil; d.plan = plan; d.preview = err == nil })
		}()
	}
	if d.preview {
		ui.Text(c, locales.CleanupScope())
		if len(d.plan) == 0 {
			ui.Text(c, locales.NothingToClean())
		} else {
			ui.Text(c, strings.Join(d.plan, "\n"))
		}
		ui.Row(c).Gap(8).Children(func() {
			if inline(ui.Button(c.Key("cleanup-confirm"), locales.DeleteTemporaryFiles())).Disabled(active || d.busy || len(d.plan) == 0 || d.update == nil).Clicked() {
				d.busy = true
				store, update, plan := d.store, d.update, append([]string(nil), d.plan...)
				go func() {
					err := store.Cleanup(plan)
					update(func() { d.busy = false; d.failed = err != nil; d.preview = false })
				}()
			}
			if inline(ui.Button(c.Key("cleanup-cancel"), locales.KeepFiles())).Clicked() {
				d.preview = false
			}
		})
	}
	if d.failed {
		ui.Text(c, locales.DiagnosticsFailed())
	}
}
