package ui

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	appstate "github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/artemis"
	"github.com/thedavidweng/any2jb/internal/devtools"
	"github.com/thedavidweng/any2jb/locales"
)

type utilitiesPanel struct {
	tab    string
	busy   bool
	output string
	failed bool
	update func(func())

	// Artemis & Seedsow FS
	artemisHost       string
	artemisPort       string
	artemisSubnet     string
	artemisRemotePath string
	artemisLocalPath  string
	artemisLua        string
	artemisConsent    bool
	artemisFSHost     string
	seedsowSession    *artemis.SeedsowFSSession

	// NXFormat
	nxAction string
	nxInput  string
	nxOutput string

	// NSP-Box
	nspSource   string
	nspOutput   string
	nspDLC      bool
	nspTitle    string
	nspVersion  string
	nspArtwork  string
	nspTitleID  string
	nspBaseGame string

	// NUP
	nupAction string
	nupInput  string
	nupOutput string

	// DNSwitch
	dnsAddress  string
	dnsHostname string
	dnsURL      string
	dnsPort     string
	dnsHTTPPort string
	dnsSession  *devtools.DNSwitchSession

	// Browser Probes
	probeName    string
	probeAddress string
	probePort    string
	probeSource  string
	probeSession *devtools.BrowserProbesSession
}

func (a *app) utilitiesView(c *ui.Context) {
	p := &a.utilitiesPanel
	if p.tab == "" {
		p.tab = "artemis"
	}
	ui.Text(c, locales.UtilTitle()).FontSize(c.Theme().Rem(1.55)).Bold()
	card(c, func() {
		muted(c, locales.UtilIntro())
		tabs := []string{"artemis", "nxformat", "nsp-box", "nup", "dnswitch", "browser-probes"}
		ui.Select(c.Key("util-tab"), &p.tab, tabs).Label(locales.UtilTab()).Disabled(p.busy)
		rule(c)
		switch p.tab {
		case "artemis":
			a.artemisUtilityView(c)
		case "nxformat":
			a.nxformatUtilityView(c)
		case "nsp-box":
			a.nspBoxUtilityView(c)
		case "nup":
			a.nupUtilityView(c)
		case "dnswitch":
			a.dnswitchUtilityView(c)
		case "browser-probes":
			a.probesUtilityView(c)
		}
		if p.output != "" {
			rule(c)
			ui.Text(c, locales.UtilOutput()).Bold()
			out := ui.Text(c.Key("util-output"), p.output).Selectable().Role(ui.RoleStatus)
			if p.failed {
				out.TextColor(c.Theme().Danger)
			}
		}
	})
}

func (a *app) runUtilityJob(fn func(context.Context) (any, error)) {
	p := &a.utilitiesPanel
	update := p.update
	if p.busy || update == nil {
		return
	}
	p.busy = true
	p.failed = false
	go func() {
		res, err := fn(context.Background())
		update(func() {
			p.busy = false
			if err != nil {
				p.failed = true
				p.output = appstate.PresentError(err)
				return
			}
			switch v := res.(type) {
			case string:
				p.output = v
			default:
				raw, marshalErr := json.MarshalIndent(v, "", "  ")
				if marshalErr != nil {
					p.failed = true
					p.output = marshalErr.Error()
					return
				}
				p.output = string(raw)
			}
		})
	}()
}

func parseOptionalPort(raw string, fallback int) int {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback
	}
	v, err := strconv.Atoi(trimmed)
	if err != nil {
		return -1
	}
	return v
}

func (a *app) defaultLocalIPv4() string {
	if a.Controller != nil {
		if ip := a.Controller.Snapshot().LANIPv4; ip != "" {
			return ip
		}
	}
	return a.Preferences.LANIPv4
}

func (a *app) artemisUtilityView(c *ui.Context) {
	p := &a.utilitiesPanel
	sectionTitle(c, locales.UtilTabArtemis())
	ready := !p.busy && p.update != nil

	ui.TextInput(c.Key("artemis-host"), &p.artemisHost).Label(locales.ArtemisHost()).Disabled(p.busy)
	ui.TextInput(c.Key("artemis-port"), &p.artemisPort).Label(locales.ArtemisPort()).Disabled(p.busy)
	ui.TextInput(c.Key("artemis-subnet"), &p.artemisSubnet).Label(locales.ArtemisSubnet()).Disabled(p.busy)
	ui.TextInput(c.Key("artemis-remote-path"), &p.artemisRemotePath).Label(locales.ArtemisRemotePath()).Disabled(p.busy)
	ui.TextInput(c.Key("artemis-local-path"), &p.artemisLocalPath).Label(locales.ArtemisLocalPath()).Disabled(p.busy)
	ui.TextArea(c.Key("artemis-lua"), &p.artemisLua).Label(locales.ArtemisLuaCode()).Disabled(p.busy)
	ui.Checkbox(c.Key("artemis-consent"), &p.artemisConsent, locales.ArtemisWriteConsent()).Disabled(p.busy)

	port := parseOptionalPort(p.artemisPort, artemis.DefaultPort)
	host := strings.TrimSpace(p.artemisHost)

	ui.Row(c).Wrap().Gap(8).Children(func() {
		if ui.Button(c.Key("artemis-catalog"), locales.ArtemisCatalog()).Disabled(!ready).Clicked() {
			pm := a.Plugins()
			manifest := a.Manifest
			a.runUtilityJob(func(context.Context) (any, error) {
				if pm == nil {
					return nil, nil
				}
				return artemis.LoadCatalog(pm.ToolRoot(), manifest)
			})
		}
		if ui.Button(c.Key("artemis-status"), locales.ArtemisStatus()).Disabled(!ready || host == "").Clicked() {
			client := a.ArtemisClient(port)
			a.runUtilityJob(func(ctx context.Context) (any, error) { return client.Status(ctx, host) })
		}
		if ui.Button(c.Key("artemis-health"), locales.ArtemisHealth()).Disabled(!ready || host == "").Clicked() {
			client := a.ArtemisClient(port)
			a.runUtilityJob(func(ctx context.Context) (any, error) { return client.Health(ctx, host) })
		}
		if ui.Button(c.Key("artemis-probe"), locales.ArtemisProbe()).Disabled(!ready || host == "").Clicked() {
			client := a.ArtemisClient(port)
			a.runUtilityJob(func(ctx context.Context) (any, error) { return client.Probe(ctx, host) })
		}
		if ui.Button(c.Key("artemis-discover"), locales.ArtemisDiscover()).Disabled(!ready).Clicked() {
			client := a.ArtemisClient(port)
			subnet := strings.TrimSpace(p.artemisSubnet)
			if subnet == "" {
				parts := strings.Split(strings.TrimSpace(a.defaultLocalIPv4()), ".")
				if len(parts) == 4 {
					subnet = strings.Join(parts[:3], ".")
				}
			}
			a.runUtilityJob(func(ctx context.Context) (any, error) { return client.Discover(ctx, subnet) })
		}
		if ui.Button(c.Key("artemis-stat"), locales.ArtemisStat()).Disabled(!ready || host == "" || strings.TrimSpace(p.artemisRemotePath) == "").Clicked() {
			client := a.ArtemisClient(port)
			rpath := strings.TrimSpace(p.artemisRemotePath)
			a.runUtilityJob(func(ctx context.Context) (any, error) { return client.Stat(ctx, host, rpath) })
		}
		if ui.Button(c.Key("artemis-get"), locales.ArtemisGet()).Disabled(!ready || host == "" || strings.TrimSpace(p.artemisRemotePath) == "" || strings.TrimSpace(p.artemisLocalPath) == "").Clicked() {
			client := a.ArtemisClient(port)
			req := artemis.GetRequest{Host: host, Path: strings.TrimSpace(p.artemisRemotePath), Out: strings.TrimSpace(p.artemisLocalPath), Overwrite: true}
			a.runUtilityJob(func(ctx context.Context) (any, error) { return client.Get(ctx, req) })
		}
		if ui.Button(c.Key("artemis-backup"), locales.ArtemisBackup()).Disabled(!ready || host == "" || strings.TrimSpace(p.artemisRemotePath) == "").Clicked() {
			client := a.ArtemisClient(port)
			req := artemis.BackupRequest{Host: host, Files: []string{strings.TrimSpace(p.artemisRemotePath)}, Dir: strings.TrimSpace(p.artemisLocalPath)}
			a.runUtilityJob(func(ctx context.Context) (any, error) { return client.Backup(ctx, req) })
		}
		if ui.Button(c.Key("artemis-eval"), locales.ArtemisEval()).Disabled(!ready || !p.artemisConsent || host == "" || strings.TrimSpace(p.artemisLua) == "").Clicked() {
			client := a.ArtemisClient(port)
			req := artemis.EvalRequest{Host: host, Code: p.artemisLua, ConfirmedArbitraryCode: true}
			p.artemisConsent = false
			a.runUtilityJob(func(ctx context.Context) (any, error) { return client.Eval(ctx, req) })
		}
		if ui.Button(c.Key("artemis-put"), locales.ArtemisPut()).Disabled(!ready || !p.artemisConsent || host == "" || strings.TrimSpace(p.artemisRemotePath) == "" || strings.TrimSpace(p.artemisLocalPath) == "").Clicked() {
			client := a.ArtemisClient(port)
			req := artemis.PutRequest{Host: host, Path: strings.TrimSpace(p.artemisRemotePath), In: strings.TrimSpace(p.artemisLocalPath), Atomic: true, ConfirmedMutation: true}
			p.artemisConsent = false
			a.runUtilityJob(func(ctx context.Context) (any, error) { return client.Put(ctx, req) })
		}
		if ui.Button(c.Key("artemis-rm"), locales.ArtemisRemove()).Disabled(!ready || !p.artemisConsent || host == "" || strings.TrimSpace(p.artemisRemotePath) == "").Clicked() {
			client := a.ArtemisClient(port)
			req := artemis.RemoveRequest{Host: host, Path: strings.TrimSpace(p.artemisRemotePath), ConfirmedMutation: true}
			p.artemisConsent = false
			a.runUtilityJob(func(ctx context.Context) (any, error) { return client.Remove(ctx, req) })
		}
	})

	rule(c)
	ui.TextInput(c.Key("artemis-fs-host"), &p.artemisFSHost).Label(locales.ArtemisFSBindHost()).Disabled(p.busy || p.seedsowSession != nil)
	ui.Row(c).Wrap().Gap(8).Children(func() {
		if ui.Button(c.Key("artemis-fs-start"), locales.ArtemisFSStart()).Disabled(!ready || p.seedsowSession != nil || host == "").Clicked() {
			pm := a.Plugins()
			manifest := a.Manifest
			localIP := strings.TrimSpace(p.artemisFSHost)
			if localIP == "" {
				localIP = a.defaultLocalIPv4()
			}
			update := p.update
			a.runUtilityJob(func(ctx context.Context) (any, error) {
				sess, err := artemis.StartSeedsowFS(ctx, artemis.SeedsowFSRequest{
					ToolRoot:  pm.ToolRoot(),
					Support:   pm.SupportRoot(),
					Node:      artemis.NodeBinary(pm.NodePath()),
					Manifest:  manifest,
					ConsoleIP: host,
					HostIP:    localIP,
					Port:      port,
				}, func(ev artemis.SeedsowFSEvent) {
					if ev.Message != "" {
						update(func() {
							p.failed = ev.Type == "failed"
							p.output = ev.Type + ": " + ev.Message
						})
					}
				})
				if err != nil {
					return nil, err
				}
				update(func() { p.seedsowSession = sess })
				go func() {
					_ = sess.Wait()
					update(func() {
						if p.seedsowSession == sess {
							p.seedsowSession = nil
						}
					})
				}()
				return struct {
					Started bool `json:"started"`
				}{Started: true}, nil
			})
		}
		if ui.Button(c.Key("artemis-fs-stop"), locales.ArtemisFSStop()).Disabled(!ready || p.seedsowSession == nil).Clicked() {
			sess := p.seedsowSession
			p.seedsowSession = nil
			a.runUtilityJob(func(context.Context) (any, error) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				err := sess.Stop(ctx)
				return struct {
					Stopped bool `json:"stopped"`
				}{Stopped: err == nil}, err
			})
		}
	})
}

func (a *app) nxformatUtilityView(c *ui.Context) {
	p := &a.utilitiesPanel
	if p.nxAction == "" {
		p.nxAction = string(devtools.NXFormatStatus)
	}
	sectionTitle(c, locales.UtilTabNXFormat())
	actions := []string{
		string(devtools.NXFormatStatus),
		string(devtools.NXFormatInfo),
		string(devtools.NXFormatContents),
		string(devtools.NXFormatVerify),
		string(devtools.NXFormatPack),
	}
	ui.Select(c.Key("nxformat-action"), &p.nxAction, actions).Label(locales.NxAction()).Disabled(p.busy)
	ui.TextInput(c.Key("nxformat-input"), &p.nxInput).Label(locales.NxInput()).Disabled(p.busy || p.nxAction == string(devtools.NXFormatStatus))
	ui.TextInput(c.Key("nxformat-output"), &p.nxOutput).Label(locales.NxOutput()).Disabled(p.busy || p.nxAction != string(devtools.NXFormatPack))
	if inline(ui.PrimaryButton(c.Key("nxformat-run"), locales.NxRun())).Disabled(p.busy || p.update == nil).Clicked() {
		cfg := a.DevToolsConfig()
		req := devtools.NXFormatRequest{
			Action: devtools.NXFormatAction(p.nxAction),
			Input:  strings.TrimSpace(p.nxInput),
			Output: strings.TrimSpace(p.nxOutput),
		}
		a.runUtilityJob(func(ctx context.Context) (any, error) {
			return cfg.RunNXFormat(ctx, req, nil)
		})
	}
}

func (a *app) nspBoxUtilityView(c *ui.Context) {
	p := &a.utilitiesPanel
	sectionTitle(c, locales.UtilTabNSPBox())
	ui.TextInput(c.Key("nsp-source"), &p.nspSource).Label(locales.NspSource()).Disabled(p.busy)
	ui.TextInput(c.Key("nsp-output"), &p.nspOutput).Label(locales.NspOutput()).Disabled(p.busy)
	ui.Checkbox(c.Key("nsp-dlc"), &p.nspDLC, locales.NspDLC()).Disabled(p.busy)
	ui.TextInput(c.Key("nsp-title"), &p.nspTitle).Label(locales.NspName()).Disabled(p.busy)
	ui.TextInput(c.Key("nsp-version"), &p.nspVersion).Label(locales.NspVersion()).Disabled(p.busy)
	ui.TextInput(c.Key("nsp-artwork"), &p.nspArtwork).Label(locales.NspIcon()).Disabled(p.busy)
	ui.TextInput(c.Key("nsp-title-id"), &p.nspTitleID).Label(locales.NspTitleID()).Disabled(p.busy)
	ui.TextInput(c.Key("nsp-base-game"), &p.nspBaseGame).Label(locales.NspBaseGame()).Disabled(p.busy || !p.nspDLC)
	if inline(ui.PrimaryButton(c.Key("nsp-build"), locales.NspBuild())).Disabled(p.busy || p.update == nil || strings.TrimSpace(p.nspSource) == "" || strings.TrimSpace(p.nspOutput) == "").Clicked() {
		cfg := a.DevToolsConfig()
		req := devtools.NSPBoxRequest{
			Source:         strings.TrimSpace(p.nspSource),
			Output:         strings.TrimSpace(p.nspOutput),
			DLC:            p.nspDLC,
			Title:          strings.TrimSpace(p.nspTitle),
			DisplayVersion: strings.TrimSpace(p.nspVersion),
			Artwork:        strings.TrimSpace(p.nspArtwork),
			TitleID:        strings.TrimSpace(p.nspTitleID),
			BaseGame:       strings.TrimSpace(p.nspBaseGame),
		}
		a.runUtilityJob(func(ctx context.Context) (any, error) {
			return cfg.BuildNSP(ctx, req, nil)
		})
	}
}

func (a *app) nupUtilityView(c *ui.Context) {
	p := &a.utilitiesPanel
	if p.nupAction == "" {
		p.nupAction = string(devtools.NUPInfo)
	}
	sectionTitle(c, locales.UtilTabNUP())
	actions := []string{
		string(devtools.NUPInfo),
		string(devtools.NUPList),
		string(devtools.NUPVerify),
		string(devtools.NUPPack),
		string(devtools.NUPExtract),
	}
	ui.Select(c.Key("nup-action"), &p.nupAction, actions).Label(locales.NupAction()).Disabled(p.busy)
	ui.TextInput(c.Key("nup-input"), &p.nupInput).Label(locales.NupInput()).Disabled(p.busy)
	needsOut := p.nupAction == string(devtools.NUPPack) || p.nupAction == string(devtools.NUPExtract)
	ui.TextInput(c.Key("nup-output"), &p.nupOutput).Label(locales.NupOutput()).Disabled(p.busy || !needsOut)
	if inline(ui.PrimaryButton(c.Key("nup-run"), locales.NupRun())).Disabled(p.busy || p.update == nil || strings.TrimSpace(p.nupInput) == "").Clicked() {
		cfg := a.DevToolsConfig()
		req := devtools.NUPRequest{
			Action: devtools.NUPAction(p.nupAction),
			Input:  strings.TrimSpace(p.nupInput),
			Output: strings.TrimSpace(p.nupOutput),
		}
		a.runUtilityJob(func(ctx context.Context) (any, error) {
			return cfg.RunNUP(ctx, req)
		})
	}
}

func (a *app) dnswitchUtilityView(c *ui.Context) {
	p := &a.utilitiesPanel
	sectionTitle(c, locales.UtilTabDNSwitch())
	ready := !p.busy && p.update != nil
	ui.TextInput(c.Key("dnswitch-address"), &p.dnsAddress).Label(locales.DnsRedirectIP()).Disabled(p.busy || p.dnsSession != nil)
	ui.TextInput(c.Key("dnswitch-hostname"), &p.dnsHostname).Label(locales.DnsHostname()).Disabled(p.busy || p.dnsSession != nil)
	ui.TextInput(c.Key("dnswitch-url"), &p.dnsURL).Label(locales.DnsLandingURL()).Disabled(p.busy || p.dnsSession != nil)
	ui.TextInput(c.Key("dnswitch-dns-port"), &p.dnsPort).Label(locales.DnsPort()).Disabled(p.busy || p.dnsSession != nil)
	ui.TextInput(c.Key("dnswitch-http-port"), &p.dnsHTTPPort).Label(locales.DnsHTTPPort()).Disabled(p.busy || p.dnsSession != nil)

	ui.Row(c).Wrap().Gap(8).Children(func() {
		if ui.PrimaryButton(c.Key("dnswitch-start"), locales.DnsStart()).Disabled(!ready || p.dnsSession != nil || strings.TrimSpace(p.dnsURL) == "").Clicked() {
			cfg := a.DevToolsConfig()
			addr := strings.TrimSpace(p.dnsAddress)
			if addr == "" {
				addr = a.defaultLocalIPv4()
			}
			req := devtools.DNSwitchRequest{
				Address:  addr,
				Hostname: strings.TrimSpace(p.dnsHostname),
				URL:      strings.TrimSpace(p.dnsURL),
				DNSPort:  parseOptionalPort(p.dnsPort, 0),
				HTTPPort: parseOptionalPort(p.dnsHTTPPort, 0),
			}
			update := p.update
			a.runUtilityJob(func(ctx context.Context) (any, error) {
				sess, err := cfg.StartDNSwitch(ctx, req, nil)
				if err != nil {
					return nil, err
				}
				update(func() { p.dnsSession = sess })
				return sess.State(), nil
			})
		}
		if ui.Button(c.Key("dnswitch-status"), locales.DnsStatus()).Disabled(!ready).Clicked() {
			cfg := a.DevToolsConfig()
			a.runUtilityJob(func(ctx context.Context) (any, error) {
				return cfg.StatusDNSwitch(ctx)
			})
		}
		if ui.Button(c.Key("dnswitch-stop"), locales.DnsStop()).Disabled(!ready).Clicked() {
			cfg := a.DevToolsConfig()
			sess := p.dnsSession
			p.dnsSession = nil
			a.runUtilityJob(func(ctx context.Context) (any, error) {
				if sess != nil {
					stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					err := sess.Stop(stopCtx)
					return struct {
						Stopped bool `json:"stopped"`
					}{Stopped: err == nil}, err
				}
				err := cfg.StopDNSwitch(ctx)
				return struct {
					Stopped bool `json:"stopped"`
				}{Stopped: err == nil}, err
			})
		}
	})
}

func (a *app) probesUtilityView(c *ui.Context) {
	p := &a.utilitiesPanel
	if p.probeName == "" {
		p.probeName = devtools.BrowserProbes[0]
	}
	sectionTitle(c, locales.UtilTabProbes())
	ready := !p.busy && p.update != nil
	ui.Select(c.Key("probes-select"), &p.probeName, devtools.BrowserProbes).Label(locales.ProbesSelect()).Disabled(p.busy || p.probeSession != nil)
	ui.TextInput(c.Key("probes-address"), &p.probeAddress).Label(locales.CivFSHostAddress()).Disabled(p.busy || p.probeSession != nil)
	ui.TextInput(c.Key("probes-port"), &p.probePort).Label(locales.ProbesPort()).Disabled(p.busy || p.probeSession != nil)
	ui.TextInput(c.Key("probes-source"), &p.probeSource).Label(locales.ProbesSource()).Disabled(p.busy || p.probeSession != nil)

	ui.Row(c).Wrap().Gap(8).Children(func() {
		if ui.PrimaryButton(c.Key("probes-start"), locales.ProbesStart()).Disabled(!ready || p.probeSession != nil).Clicked() {
			cfg := a.DevToolsConfig()
			addr := strings.TrimSpace(p.probeAddress)
			if addr == "" {
				addr = a.defaultLocalIPv4()
			}
			req := devtools.BrowserProbesRequest{
				Address: addr,
				Port:    parseOptionalPort(p.probePort, 0),
				Probe:   p.probeName,
				Source:  strings.TrimSpace(p.probeSource),
			}
			update := p.update
			a.runUtilityJob(func(ctx context.Context) (any, error) {
				sess, err := cfg.StartBrowserProbes(ctx, req, nil)
				if err != nil {
					return nil, err
				}
				update(func() { p.probeSession = sess })
				return struct {
					Probe        string `json:"probe"`
					URL          string `json:"url"`
					ReportFolder string `json:"reportFolder"`
				}{
					Probe:        sess.Probe(),
					URL:          sess.URL(),
					ReportFolder: sess.ReportFolder(),
				}, nil
			})
		}
		if ui.Button(c.Key("probes-stop"), locales.ProbesStop()).Disabled(!ready || p.probeSession == nil).Clicked() {
			sess := p.probeSession
			p.probeSession = nil
			a.runUtilityJob(func(ctx context.Context) (any, error) {
				stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				err := sess.Stop(stopCtx)
				return struct {
					Stopped bool `json:"stopped"`
				}{Stopped: err == nil}, err
			})
		}
	})
}

func (a *app) stopUtilitySessions() {
	p := &a.utilitiesPanel
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if p.seedsowSession != nil {
		_ = p.seedsowSession.Stop(ctx)
		p.seedsowSession = nil
	}
	if p.dnsSession != nil {
		_ = p.dnsSession.Stop(ctx)
		p.dnsSession = nil
	}
	if p.probeSession != nil {
		_ = p.probeSession.Stop(ctx)
		p.probeSession = nil
	}
}
