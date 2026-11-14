package cli

import (
	"context"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/artemis"
	"github.com/thedavidweng/any2jb/internal/devtools"
	"github.com/thedavidweng/any2jb/locales"
)

func (c *commandLine) addUtilities(root *cobra.Command) {
	c.addArtemis(root)
	c.addNXFormat(root)
	c.addNSPBox(root)
	c.addNUP(root)
	c.addDNSwitch(root)
	c.addBrowserProbes(root)
}

func (c *commandLine) resolveConsoleHost(flagVal string) string {
	if h := strings.TrimSpace(flagVal); h != "" {
		return h
	}
	return c.app.Controller.Snapshot().ConsoleIPv4
}

func (c *commandLine) resolveLocalHost(flagVal string) string {
	if h := strings.TrimSpace(flagVal); h != "" {
		return h
	}
	return c.app.Controller.Snapshot().LANIPv4
}

func defaultSubnet24(ipv4 string) string {
	parts := strings.Split(strings.TrimSpace(ipv4), ".")
	if len(parts) == 4 {
		return strings.Join(parts[:3], ".")
	}
	return ipv4
}

func (c *commandLine) addArtemis(root *cobra.Command) {
	group := &cobra.Command{Use: "artemis", Short: locales.UtilTabArtemis()}
	root.AddCommand(group)

	var consoleHost string
	var port int
	group.PersistentFlags().StringVar(&consoleHost, "console", "", locales.ArtemisHost())
	group.PersistentFlags().IntVar(&port, "port", artemis.DefaultPort, locales.ArtemisPort())

	c.command(group, "catalog", locales.ArtemisCatalog(), false, func(*cobra.Command, []string) error {
		pm := c.app.Plugins()
		if pm == nil {
			return cliFail(exitBlocked, locales.CommandRejected())
		}
		cat, err := artemis.LoadCatalog(pm.ToolRoot(), c.app.Manifest)
		if err != nil {
			return cliFail(exitBlocked, app.PresentError(err))
		}
		return c.print(cat)
	})

	c.command(group, "status", locales.ArtemisStatus(), false, func(*cobra.Command, []string) error {
		res, err := c.app.ArtemisClient(port).Status(context.Background(), c.resolveConsoleHost(consoleHost))
		if err != nil {
			return err
		}
		return c.print(res)
	})

	c.command(group, "health", locales.ArtemisHealth(), false, func(*cobra.Command, []string) error {
		res, err := c.app.ArtemisClient(port).Health(context.Background(), c.resolveConsoleHost(consoleHost))
		if err != nil {
			return err
		}
		return c.print(res)
	})

	c.command(group, "probe", locales.ArtemisProbe(), false, func(*cobra.Command, []string) error {
		res, err := c.app.ArtemisClient(port).Probe(context.Background(), c.resolveConsoleHost(consoleHost))
		if err != nil {
			return err
		}
		return c.print(res)
	})

	var subnet string
	discoverCmd := c.command(group, "discover", locales.ArtemisDiscover(), false, func(*cobra.Command, []string) error {
		target := strings.TrimSpace(subnet)
		if target == "" {
			target = defaultSubnet24(c.resolveLocalHost(consoleHost))
		}
		res, err := c.app.ArtemisClient(port).Discover(context.Background(), target)
		if err != nil {
			return err
		}
		return c.print(res)
	})
	discoverCmd.Flags().StringVar(&subnet, "subnet", "", locales.ArtemisSubnet())

	var statPath string
	statCmd := c.command(group, "stat", locales.ArtemisStat(), false, func(*cobra.Command, []string) error {
		res, err := c.app.ArtemisClient(port).Stat(context.Background(), c.resolveConsoleHost(consoleHost), statPath)
		if err != nil {
			return err
		}
		return c.print(res)
	})
	statCmd.Flags().StringVar(&statPath, "path", "", locales.ArtemisRemotePath())

	var getPath, getOut string
	var getOverwrite bool
	getCmd := c.command(group, "get", locales.ArtemisGet(), false, func(*cobra.Command, []string) error {
		res, err := c.app.ArtemisClient(port).Get(context.Background(), artemis.GetRequest{
			Host:      c.resolveConsoleHost(consoleHost),
			Path:      getPath,
			Out:       getOut,
			Overwrite: getOverwrite,
		})
		if err != nil {
			return err
		}
		return c.print(res)
	})
	getCmd.Flags().StringVar(&getPath, "path", "", locales.ArtemisRemotePath())
	getCmd.Flags().StringVar(&getOut, "out", "", locales.ArtemisLocalPath())
	getCmd.Flags().BoolVar(&getOverwrite, "overwrite", false, locales.LauncherConfirm())

	var evalCode, evalFile, evalConsent string
	evalCmd := c.command(group, "eval", locales.ArtemisEval(), false, func(*cobra.Command, []string) error {
		if err := c.consent(evalConsent, "artemis-eval", locales.ArtemisWriteConsent()); err != nil {
			return err
		}
		res, err := c.app.ArtemisClient(port).Eval(context.Background(), artemis.EvalRequest{
			Host:                   c.resolveConsoleHost(consoleHost),
			Code:                   evalCode,
			File:                   evalFile,
			ConfirmedArbitraryCode: true,
		})
		if err != nil {
			return err
		}
		return c.print(res)
	})
	evalCmd.Flags().StringVar(&evalCode, "code", "", locales.ArtemisLuaCode())
	evalCmd.Flags().StringVar(&evalFile, "file", "", cliMessage("cliFile"))
	evalCmd.Flags().StringVar(&evalConsent, "consent", "", cliMessage("cliConsent"))

	var putIn, putPath, putConsent string
	var putForce, putAtomic bool
	putCmd := c.command(group, "put", locales.ArtemisPut(), false, func(*cobra.Command, []string) error {
		if err := c.consent(putConsent, "artemis-write", locales.ArtemisWriteConsent()); err != nil {
			return err
		}
		res, err := c.app.ArtemisClient(port).Put(context.Background(), artemis.PutRequest{
			Host:              c.resolveConsoleHost(consoleHost),
			Path:              putPath,
			In:                putIn,
			Force:             putForce,
			Atomic:            putAtomic,
			ConfirmedMutation: true,
		})
		if err != nil {
			return err
		}
		return c.print(res)
	})
	putCmd.Flags().StringVar(&putIn, "in", "", locales.ArtemisLocalPath())
	putCmd.Flags().StringVar(&putPath, "path", "", locales.ArtemisRemotePath())
	putCmd.Flags().BoolVar(&putForce, "force", false, locales.LauncherConfirm())
	putCmd.Flags().BoolVar(&putAtomic, "atomic", true, locales.LauncherConfirm())
	putCmd.Flags().StringVar(&putConsent, "consent", "", cliMessage("cliConsent"))

	var rmPath, rmConsent string
	rmCmd := c.command(group, "rm", locales.ArtemisRemove(), false, func(*cobra.Command, []string) error {
		if err := c.consent(rmConsent, "artemis-delete", locales.ArtemisWriteConsent()); err != nil {
			return err
		}
		res, err := c.app.ArtemisClient(port).Remove(context.Background(), artemis.RemoveRequest{
			Host:              c.resolveConsoleHost(consoleHost),
			Path:              rmPath,
			ConfirmedMutation: true,
		})
		if err != nil {
			return err
		}
		return c.print(res)
	})
	rmCmd.Flags().StringVar(&rmPath, "path", "", locales.ArtemisRemotePath())
	rmCmd.Flags().StringVar(&rmConsent, "consent", "", cliMessage("cliConsent"))

	var backupFiles []string
	var backupOut string
	backupCmd := c.command(group, "backup", locales.ArtemisBackup(), false, func(*cobra.Command, []string) error {
		res, err := c.app.ArtemisClient(port).Backup(context.Background(), artemis.BackupRequest{
			Host:  c.resolveConsoleHost(consoleHost),
			Files: backupFiles,
			Dir:   backupOut,
		})
		if err != nil {
			return err
		}
		return c.print(res)
	})
	backupCmd.Flags().StringSliceVar(&backupFiles, "path", nil, locales.ArtemisRemotePath())
	backupCmd.Flags().StringVar(&backupOut, "out", "", locales.ArtemisLocalPath())

	var hostIP string
	fsCmd := c.command(group, "fs", locales.ArtemisFSStart(), false, func(*cobra.Command, []string) error {
		pm := c.app.Plugins()
		if pm == nil {
			return cliFail(exitBlocked, locales.CommandRejected())
		}
		sess, err := artemis.StartSeedsowFS(context.Background(), artemis.SeedsowFSRequest{
			ToolRoot:  pm.ToolRoot(),
			Support:   pm.SupportRoot(),
			Node:      artemis.NodeBinary(pm.NodePath()),
			Manifest:  c.app.Manifest,
			ConsoleIP: c.resolveConsoleHost(consoleHost),
			HostIP:    c.resolveLocalHost(hostIP),
			Port:      port,
		}, func(ev artemis.SeedsowFSEvent) {
			if ev.Message != "" {
				c.say(ev.Type + ": " + ev.Message)
			}
		})
		if err != nil {
			return err
		}
		done := make(chan error, 1)
		go func() { done <- sess.Wait() }()
		select {
		case waitErr := <-done:
			return waitErr
		case <-c.signals:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = sess.Stop(ctx)
			err = <-done
			_ = err
			return cliFail(exitCancelled, cliMessage("cliDeclined"))
		}
	})
	fsCmd.Flags().StringVar(&hostIP, "host", "", locales.ArtemisFSBindHost())
}

func (c *commandLine) addNXFormat(root *cobra.Command) {
	group := &cobra.Command{Use: "nxformat", Short: locales.UtilTabNXFormat()}
	root.AddCommand(group)

	c.command(group, "status", locales.NxActionStatus(), false, func(*cobra.Command, []string) error {
		res, err := c.app.DevToolsConfig().RunNXFormat(context.Background(), devtools.NXFormatRequest{Action: devtools.NXFormatStatus}, nil)
		if err != nil {
			return err
		}
		return c.print(res)
	})

	for _, item := range []struct {
		name   string
		action devtools.NXFormatAction
	}{
		{"info", devtools.NXFormatInfo},
		{"contents", devtools.NXFormatContents},
		{"verify", devtools.NXFormatVerify},
	} {
		var inPath string
		act := item.action
		cmd := c.command(group, item.name, locales.UtilTabNXFormat(), false, func(*cobra.Command, []string) error {
			res, err := c.app.DevToolsConfig().RunNXFormat(context.Background(), devtools.NXFormatRequest{
				Action: act,
				Input:  inPath,
			}, nil)
			if err != nil {
				return err
			}
			return c.print(res)
		})
		cmd.Flags().StringVar(&inPath, "in", "", locales.NxInput())
	}

	var packIn, packOut string
	packCmd := c.command(group, "pack", locales.NxActionPack(), false, func(*cobra.Command, []string) error {
		res, err := c.app.DevToolsConfig().RunNXFormat(context.Background(), devtools.NXFormatRequest{
			Action: devtools.NXFormatPack,
			Input:  packIn,
			Output: packOut,
		}, nil)
		if err != nil {
			return err
		}
		return c.print(res)
	})
	packCmd.Flags().StringVar(&packIn, "in", "", locales.NxInput())
	packCmd.Flags().StringVar(&packOut, "out", "", locales.NxOutput())
}

func (c *commandLine) addNSPBox(root *cobra.Command) {
	group := &cobra.Command{Use: "nsp-box", Short: locales.UtilTabNSPBox()}
	root.AddCommand(group)

	var req devtools.NSPBoxRequest
	buildCmd := c.command(group, "build", locales.NspBuild(), false, func(*cobra.Command, []string) error {
		res, err := c.app.DevToolsConfig().BuildNSP(context.Background(), req, func(ev devtools.NSPBoxEvent) {
			if ev.Message != "" {
				c.say(ev.Message)
			}
		})
		if err != nil {
			return err
		}
		return c.print(res)
	})
	buildCmd.Flags().StringVar(&req.Source, "source", "", locales.NspSource())
	buildCmd.Flags().StringVar(&req.Output, "out", "", locales.NspOutput())
	buildCmd.Flags().BoolVar(&req.DLC, "dlc", false, locales.NspDLC())
	buildCmd.Flags().StringVar(&req.Title, "title", "", locales.NspName())
	buildCmd.Flags().StringVar(&req.DisplayVersion, "version", "", locales.NspVersion())
	buildCmd.Flags().StringVar(&req.Artwork, "artwork", "", locales.NspIcon())
	buildCmd.Flags().StringVar(&req.TitleID, "title-id", "", locales.NspTitleID())
	buildCmd.Flags().StringVar(&req.BaseGame, "base-game", "", locales.NspBaseGame())
}

func (c *commandLine) addNUP(root *cobra.Command) {
	group := &cobra.Command{Use: "nup", Short: locales.UtilTabNUP()}
	root.AddCommand(group)

	for _, item := range []struct {
		name   string
		action devtools.NUPAction
	}{
		{"info", devtools.NUPInfo},
		{"list", devtools.NUPList},
		{"verify", devtools.NUPVerify},
	} {
		var inPath string
		act := item.action
		cmd := c.command(group, item.name, locales.UtilTabNUP(), false, func(*cobra.Command, []string) error {
			res, err := c.app.DevToolsConfig().RunNUP(context.Background(), devtools.NUPRequest{
				Action: act,
				Input:  inPath,
			})
			if err != nil {
				return err
			}
			return c.print(res)
		})
		cmd.Flags().StringVar(&inPath, "in", "", locales.NupInput())
	}

	for _, item := range []struct {
		name   string
		action devtools.NUPAction
	}{
		{"pack", devtools.NUPPack},
		{"extract", devtools.NUPExtract},
	} {
		var inPath, outPath string
		act := item.action
		cmd := c.command(group, item.name, locales.UtilTabNUP(), false, func(*cobra.Command, []string) error {
			res, err := c.app.DevToolsConfig().RunNUP(context.Background(), devtools.NUPRequest{
				Action: act,
				Input:  inPath,
				Output: outPath,
			})
			if err != nil {
				return err
			}
			return c.print(res)
		})
		cmd.Flags().StringVar(&inPath, "in", "", locales.NupInput())
		cmd.Flags().StringVar(&outPath, "out", "", locales.NupOutput())
	}
}

func (c *commandLine) addDNSwitch(root *cobra.Command) {
	group := &cobra.Command{Use: "dnswitch", Short: locales.UtilTabDNSwitch()}
	root.AddCommand(group)

	var req devtools.DNSwitchRequest
	startCmd := c.command(group, "start", locales.DnsStart(), false, func(*cobra.Command, []string) error {
		if strings.TrimSpace(req.Address) == "" {
			req.Address = c.resolveLocalHost("")
		}
		sess, err := c.app.DevToolsConfig().StartDNSwitch(context.Background(), req, func(ev devtools.DNSwitchEvent) {
			if ev.Message != "" {
				c.say(ev.Message)
			}
		})
		if err != nil {
			return err
		}
		if err := c.print(sess.State()); err != nil {
			_ = sess.Stop(context.Background())
			return err
		}
		<-c.signals
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return sess.Stop(ctx)
	})
	startCmd.Flags().StringVar(&req.Address, "address", "", locales.DnsRedirectIP())
	startCmd.Flags().StringVar(&req.Hostname, "hostname", "", locales.DnsHostname())
	startCmd.Flags().StringVar(&req.URL, "url", "", locales.DnsLandingURL())
	startCmd.Flags().IntVar(&req.DNSPort, "dns-port", 0, locales.DnsPort())
	startCmd.Flags().IntVar(&req.HTTPPort, "http-port", 0, locales.DnsHTTPPort())

	c.command(group, "status", locales.DnsStatus(), false, func(*cobra.Command, []string) error {
		state, err := c.app.DevToolsConfig().StatusDNSwitch(context.Background())
		if err != nil {
			return err
		}
		return c.print(state)
	})

	c.command(group, "stop", locales.DnsStop(), false, func(*cobra.Command, []string) error {
		if err := c.app.DevToolsConfig().StopDNSwitch(context.Background()); err != nil {
			return err
		}
		return c.print(struct {
			Stopped bool `json:"stopped"`
		}{Stopped: true})
	})
}

func (c *commandLine) addBrowserProbes(root *cobra.Command) {
	group := &cobra.Command{Use: "browser-probes", Short: locales.UtilTabProbes()}
	root.AddCommand(group)

	c.static(group, "list", locales.ProbesSelect(), func(*cobra.Command, []string) error {
		return c.print(devtools.BrowserProbes)
	})

	var req devtools.BrowserProbesRequest
	serveCmd := c.command(group, "serve", locales.ProbesStart(), false, func(*cobra.Command, []string) error {
		if strings.TrimSpace(req.Address) == "" {
			req.Address = c.resolveLocalHost("")
		}
		sess, err := c.app.DevToolsConfig().StartBrowserProbes(context.Background(), req, func(ev devtools.BrowserProbesEvent) {
			if ev.Message != "" {
				c.say(ev.Message)
			}
		})
		if err != nil {
			return err
		}
		if err := c.print(struct {
			Probe        string `json:"probe"`
			URL          string `json:"url"`
			ReportFolder string `json:"reportFolder"`
		}{
			Probe:        sess.Probe(),
			URL:          sess.URL(),
			ReportFolder: sess.ReportFolder(),
		}); err != nil {
			_ = sess.Stop(context.Background())
			return err
		}
		<-c.signals
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return sess.Stop(ctx)
	})
	serveCmd.Flags().StringVar(&req.Address, "address", "", locales.CivFSHostAddress())
	serveCmd.Flags().IntVar(&req.Port, "port", 0, locales.ProbesPort())
	serveCmd.Flags().StringVar(&req.Probe, "probe", devtools.BrowserProbes[0], locales.ProbesSelect())
	serveCmd.Flags().StringVar(&req.Source, "source", "", locales.ProbesSource())
}
