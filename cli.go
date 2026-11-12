package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/spf13/cobra"
	"github.com/thedavidweng/any2jb/civswitch"
	"github.com/thedavidweng/any2jb/diagnostics"
	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/internal/devlaunch"
	"github.com/thedavidweng/any2jb/internal/sessionlock"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

type cliFailure struct {
	code int
	err  error
}

func (e *cliFailure) Error() string { return e.err.Error() }
func cliExitCode(err error) int {
	if err == nil {
		return 0
	}
	var failure *cliFailure
	if errors.As(err, &failure) {
		return failure.code
	}
	return exitError
}

// Exit codes are part of the documented CLI contract (docs/cli.md).
const (
	exitError         = 1
	exitCancelled     = 2
	exitBlocked       = 3
	exitIndeterminate = 4
)

func cliFail(code int, text string) error { return &cliFailure{code, errors.New(text)} }

type cliLine struct {
	text string
	err  error
}
type cliJSONOutput struct {
	values []any
	text   bytes.Buffer
	stream bool
}
type commandLine struct {
	app                   *app
	support               string
	in                    io.Reader
	out, stderr           io.Writer
	terminal, shell, json bool
	lang                  string
	lines                 chan cliLine
	signals               chan os.Signal
	pulse                 chan struct{}
	release               func() error
	helpers               *transport.DeveloperHelpers
	sequence              uint64
	output                *cliJSONOutput
}

func newCommandLine(in io.Reader, out, stderr io.Writer, terminal bool) *commandLine {
	return &commandLine{in: in, out: out, stderr: stderr, terminal: terminal, pulse: make(chan struct{}, 1)}
}
func cliMessage(key string) string     { return locales.Catalog.Format(i18n.Locale(), key, nil) }
func (c *commandLine) say(text string) { fmt.Fprintln(c.stderr, text) }
func (c *commandLine) print(value any) error {
	if c.json {
		c.output.values = append(c.output.values, value)
		return nil
	}
	switch v := value.(type) {
	case string:
		_, err := fmt.Fprintln(c.out, v)
		return err
	default:
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(c.out, string(data))
		return err
	}
}
func (c *commandLine) load() error {
	if c.app != nil {
		return nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	c.app, err = loadApp(filepath.Join(dir, "any2jb", "preferences.json"))
	return err
}
func (c *commandLine) own() error {
	if c.release != nil {
		return nil
	}
	if c.support == "" {
		var err error
		c.support, err = mygo.App.Path(mygo.PathUserData)
		if err != nil {
			return err
		}
	}
	release, err := sessionlock.Acquire(c.support)
	if errors.Is(err, sessionlock.ErrOwned) {
		return cliFail(exitBlocked, cliMessage("cliOwned"))
	}
	c.release = release
	return err
}
func (c *commandLine) ensure() error {
	if err := c.load(); err != nil {
		return err
	}
	if c.app.controller != nil {
		return nil
	}
	if err := c.own(); err != nil {
		return err
	}
	assets, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return err
	}
	options := engine.Options{Assets: assets, Support: c.support, Version: mygo.App.Version(), Manifest: c.app.manifest}
	developer, err := devlaunch.Load(os.Getenv("ANY2JB_DEVELOPER_CONFIG"))
	if err != nil {
		return err
	}
	if developer.SourceRoot != "" {
		options.Assets, options.DeveloperSourceRoot, options.DeveloperProfileID = developer.Assets, developer.SourceRoot, c.app.preferences.Profile
		c.app.developerStaged = true
		c.say(locales.DeveloperStaged())
	}
	input := transport.DarwinOptions{Timing: transport.DefaultTiming(), Confirm: c.app.usbConfirmation.Wait}
	if c.app.preferences.USB != nil {
		input.Controller, input.Timing = c.app.preferences.USB.Controller, c.app.preferences.USB.Timing
	}
	civ := civswitch.Options{DeveloperInstallerPython: developer.InstallerPython}
	if developer.SourceRoot != "" {
		c.helpers, err = transport.BuildDeveloperHelpers(context.Background(), developer.SourceRoot, c.support, c.app.manifest, io.Discard)
		if err != nil {
			return err
		}
		input.Helpers = c.helpers
		civ.DeveloperHelperPath, civ.DeveloperHelperSHA256, err = c.helpers.CompositeHelper()
		if err != nil {
			return err
		}
	}
	c.app.usbConfirmation.notify = func() {
		select {
		case c.pulse <- struct{}{}:
		default:
		}
	}
	c.app.driverPanel.gate = productionDriverGate(options)
	if err := c.app.initializeSession(productionEnvironmentWithCivSwitch(options, input, civ)); err != nil {
		return err
	}
	store := diagnostics.Store{Support: c.support}
	interrupted, recoveryErr := store.Interrupted()
	if interrupted || recoveryErr != nil {
		c.app.controller.MarkInterrupted()
		if recoveryErr != nil {
			return recoveryErr
		}
	}
	return nil
}
func (c *commandLine) close() {
	if c.helpers != nil {
		c.helpers.Close()
	}
	if c.release != nil {
		c.release()
	}
}

// locale resolves the CLI language without MyGo's main thread, which never
// runs in a command-line process: --lang, then MYGO_LOCALE and the POSIX
// locale variables a terminal sets, then the catalog's source language.
func (c *commandLine) locale() string {
	want := []string{c.lang}
	for _, name := range []string{"MYGO_LOCALE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		want = append(want, os.Getenv(name))
	}
	var tags []string
	for _, tag := range want {
		if n := i18n.Normalize(tag); n != "" {
			tags = append(tags, n)
		}
	}
	if m := i18n.Match(tags, locales.Catalog.Languages()); m != "" {
		return m
	}
	return "en"
}

func (c *commandLine) execute(args []string) (err error) {
	previous := c.output
	output := &cliJSONOutput{}
	c.output = output
	defer func() {
		c.output = previous
		if !c.json || output.stream {
			return
		}
		object := map[string]any{}
		if len(output.values) == 1 {
			data, marshalErr := json.Marshal(output.values[0])
			if marshalErr != nil {
				err = marshalErr
			} else if len(data) > 0 && data[0] == '{' {
				decoder := json.NewDecoder(bytes.NewReader(data))
				decoder.UseNumber()
				errDecode := decoder.Decode(&object)
				if errDecode != nil {
					err = errDecode
				}
			} else {
				object["result"] = output.values[0]
			}
		} else if len(output.values) > 1 {
			object["results"] = output.values
		}
		if output.text.Len() > 0 {
			object["text"] = output.text.String()
		}
		if err != nil {
			exit := cliExitCode(err)
			object["code"] = map[int]string{exitError: "error", exitCancelled: "cancelled", exitBlocked: "blocked", exitIndeterminate: "recovery_required"}[exit]
			object["exit"], object["message"] = exit, localize(err.Error())
			if exit == exitIndeterminate {
				object["recovery"] = map[string]any{"required": true, "stage": session.Indeterminate}
				object["next"] = "any2jb session, then restart-confirm --confirm=console-restarted"
			}
		}
		if writeErr := json.NewEncoder(c.out).Encode(object); writeErr != nil {
			err = writeErr
		}
	}()
	// Translate help too, before Cobra parses flags and constructs descriptions.
	for i, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--json" || arg == "--json=true" {
			c.json = true
		}
		if arg == "--json=false" {
			c.json = false
		}
		if strings.HasPrefix(arg, "--lang=") {
			c.lang = strings.TrimPrefix(arg, "--lang=")
		}
		if arg == "--lang" && i+1 < len(args) {
			c.lang = args[i+1]
		}
	}
	if c.lang != "" && c.lang != "en" && c.lang != "zh-CN" {
		return cliFail(exitError, "use --lang=en or --lang=zh-CN")
	}
	i18n.SetLocaleFunc(func() string { return c.locale() })
	root := c.root()
	root.SetArgs(args)
	root.SetIn(c.in)
	root.SetOut(c.out)
	if c.json {
		root.SetOut(&output.text)
	}
	root.SetErr(c.stderr)
	return root.Execute()
}
func (c *commandLine) command(parent *cobra.Command, use, title string, live bool, fn func(*cobra.Command, []string) error) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: title, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if live && !c.shell {
			return cliFail(exitBlocked, cliMessage("cliNeedsSession"))
		}
		if err := c.ensure(); err != nil {
			return err
		}
		return fn(cmd, args)
	}}
	parent.AddCommand(cmd)
	return cmd
}

// static adds a command that reads only preferences and the manifest, so it
// runs while another process owns the session.
func (c *commandLine) static(parent *cobra.Command, use, title string, fn func(*cobra.Command, []string) error) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: title, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := c.load(); err != nil {
			return err
		}
		return fn(cmd, args)
	}}
	parent.AddCommand(cmd)
	return cmd
}

func (c *commandLine) root() *cobra.Command {
	root := &cobra.Command{Use: "any2jb", Short: cliMessage("cliDescription"), SilenceUsage: true, SilenceErrors: true}
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().BoolVar(&c.json, "json", c.json, cliMessage("cliJSON"))
	// execute reads --lang before the tree exists, so help is translated too.
	var lang string
	root.PersistentFlags().StringVar(&lang, "lang", "", cliMessage("cliLanguage"))
	root.AddCommand(&cobra.Command{Use: "status", Short: locales.VerifiedSession(), Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		if err := c.ensure(); err != nil {
			return err
		}
		return c.status()
	}})
	c.command(root, "detect", locales.CheckingReadiness(), false, func(*cobra.Command, []string) error {
		if err := c.operation(c.app.controller.Inspect()); err != nil {
			return err
		}
		if err := c.status(); err != nil {
			return err
		}
		if c.app.controller.Snapshot().Stage == session.NeedsSetup {
			return cliFail(exitBlocked, locales.SetupNeedsAttention())
		}
		return nil
	})
	var script string
	shell := &cobra.Command{Use: "session", Short: cliMessage("cliCmdSession"), Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		if c.shell {
			return cliFail(exitError, cliMessage("cliNeedsSession"))
		}
		if err := c.ensure(); err != nil {
			return err
		}
		if err := c.own(); err != nil {
			return err
		}
		if script != "" {
			f, err := os.Open(script)
			if err != nil {
				return err
			}
			defer f.Close()
			c.in, c.terminal = f, false
		}
		c.output.stream = true
		return c.runShell()
	}}
	shell.Flags().StringVar(&script, "script", "", cliMessage("cliScript"))
	root.AddCommand(shell)
	c.command(root, "prepare", locales.PrepareBootstrap(), true, func(*cobra.Command, []string) error { return c.operation(c.app.controller.PrepareBootstrap()) })
	var overwrite string
	save := c.command(root, "save", locales.ManualSave(), true, func(*cobra.Command, []string) error {
		if err := c.consent(overwrite, "replace-bootstrap", locales.AllowBootstrapOverwrite()); err != nil {
			return err
		}
		return c.operation(c.app.controller.SaveBootstrap(false))
	})
	save.Flags().StringVar(&overwrite, "consent", "", cliMessage("cliConsent"))
	var saved string
	confirmSave := c.command(root, "confirm-save", locales.ConfirmBootstrapIdentityAction(), true, func(*cobra.Command, []string) error {
		if err := c.consent(saved, "console-saved", locales.ManualSave()); err != nil {
			return err
		}
		if err := c.operation(c.app.controller.SaveBootstrap(true)); err != nil {
			return err
		}
		return c.app.persistBootstrap(c.app.bootstrapPath())
	})
	confirmSave.Flags().StringVar(&saved, "confirm", "", cliMessage("cliConfirm"))
	var reuse string
	bootstrap := c.command(root, "use-saved", locales.UseSavedBootstrap(), true, func(*cobra.Command, []string) error {
		s := c.app.controller.Snapshot()
		text := locales.ConfirmBootstrapIdentity(s.Profile.SaveName, s.Profile.ID, s.Edition, s.Profile.Revision, s.Profile.BootstrapSHA256)
		if err := c.consent(reuse, "matching-bootstrap", text); err != nil {
			return err
		}
		return c.app.confirmExistingBootstrap()
	})
	bootstrap.Flags().StringVar(&reuse, "confirm", "", cliMessage("cliConfirm"))
	c.command(root, "launch", locales.StartJailbreak(), true, func(*cobra.Command, []string) error {
		// A recovery-required session reports its own exit code through Start.
		if s := c.app.controller.Snapshot(); s.Stage != session.Indeterminate && !s.CanStart() {
			return cliFail(exitBlocked, locales.SetupNeedsAttention())
		}
		return c.operation(c.app.controller.Start())
	})
	c.command(root, "wait", locales.SessionInProgress(), true, func(*cobra.Command, []string) error { return c.wait() })
	c.command(root, "cancel", locales.Cancel(), true, func(*cobra.Command, []string) error { return c.operation(c.app.controller.Cancel()) })
	c.command(root, "stop", locales.Stop(), true, func(*cobra.Command, []string) error { return c.operation(c.app.controller.Stop()) })
	c.command(root, "finish", locales.Finish(), true, func(*cobra.Command, []string) error { return c.operation(c.app.controller.Finish()) })
	var restart string
	recovery := c.command(root, "restart-confirm", locales.RecoveryRequired(), true, func(*cobra.Command, []string) error {
		if err := c.consent(restart, "console-restarted", locales.RestartConsole()); err != nil {
			return err
		}
		return c.operation(c.app.controller.ConfirmConsoleRestart(context.Background(), true))
	})
	recovery.Flags().StringVar(&restart, "confirm", "", cliMessage("cliConfirm"))
	var ack string
	acknowledge := c.command(root, "ack", locales.ManualAcknowledge(), true, func(*cobra.Command, []string) error { return c.acknowledge(ack) })
	acknowledge.Flags().StringVar(&ack, "confirm", "", cliMessage("cliConfirm"))
	c.addSettings(root)
	c.addTools(root)
	c.addDiagnostics(root)
	c.addCivSwitch(root)
	c.addKeyboard(root)
	root.InitDefaultHelpCmd()
	localizeCommands(root)
	return root
}

// localizeCommands describes every command from the shared catalog, keyed by
// its path: "savekit write" is cliCmdSavekitWrite.
func localizeCommands(parent *cobra.Command) {
	for _, cmd := range parent.Commands() {
		cmd.Short = cliMessage(cliCommandKey(cmd))
		localizeCommands(cmd)
	}
}

func cliCommandKey(cmd *cobra.Command) string {
	key := "cliCmd"
	for _, word := range strings.FieldsFunc(strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()), func(r rune) bool { return r == ' ' || r == '-' }) {
		key += strings.ToUpper(word[:1]) + word[1:]
	}
	return key
}

func (c *commandLine) status() error {
	s := c.app.controller.Snapshot()
	if c.json {
		return c.print(s)
	}
	fmt.Fprintln(c.out, stageTitle(s.Stage))
	fmt.Fprintf(c.out, "%s: %s\n%s: %s\n", locales.HostProfile(), s.Profile.ID, locales.TransportMode(), s.Transport.Mode)
	for _, check := range s.Checks {
		fmt.Fprintf(c.out, "%s: %s\n%s\n", checkName(check), checkStateTitle(check.State), localize(check.Message))
		if check.NextAction != "" {
			fmt.Fprintln(c.out, localize(check.NextAction))
		}
	}
	if s.Stage == session.Indeterminate {
		fmt.Fprintln(c.out, locales.RestartConsole())
		fmt.Fprintln(c.out, cliMessage("cliRestartHint"))
	}
	// Startup security is a manual note, never an observed readiness claim.
	if startupSecurityApplies(s.Transport) {
		fmt.Fprintln(c.out, locales.StartupSecurityTitle(), locales.StartupSecurityUnverified(), locales.StartupSecurityAction())
	}
	return nil
}
func (c *commandLine) consent(provided, expected, risk string) error {
	c.say(risk)
	if c.terminal {
		c.say(cliMessage("cliPrompt"))
		line, err := c.readLine()
		if err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(line), "y") {
			return nil
		}
		return cliFail(exitCancelled, cliMessage("cliDeclined"))
	}
	if provided != expected {
		return cliFail(exitCancelled, cliMessage("cliExplicitConsent")+" "+expected)
	}
	return nil
}
func (c *commandLine) operation(s session.Snapshot, err error) error {
	var reject *session.Rejection
	if !errors.As(err, &reject) {
		if err != nil {
			return err
		}
		return c.wait()
	}
	// The GUI's own mapping turns a rejection into the localized instruction.
	c.app.command(s, err)
	switch {
	case reject.Stage == session.Indeterminate:
		return cliFail(exitIndeterminate, locales.RecoveryRequired()+"\n"+locales.RestartConsole()+"\n"+cliMessage("cliRestartHint"))
	case c.app.commandInstruction != "":
		return cliFail(exitBlocked, c.app.commandInstruction)
	case reject.Instruction != "":
		return cliFail(exitBlocked, localize(reject.Instruction))
	}
	return cliFail(exitBlocked, locales.CommandRejected())
}

// prompt reports the console step a live operation waits on: a guided manual
// step or a USB console confirmation, with the identity that acknowledges it.
func (c *commandLine) prompt() (text string, ack func() bool, ok bool) {
	if c.app.manual != nil {
		if p := c.app.manual.Prompt(); p.Kind != "" {
			text = manualInstruction(p.Kind)
			if p.Text != "" {
				text += "\n" + p.Text
			}
			return text, func() bool { return c.app.manual.Acknowledge(p.RunID, p.Sequence) }, true
		}
	}
	if step, id, pending := c.app.usbConfirmation.current(); pending {
		return consolePrompt(step), func() bool { return c.app.usbConfirmation.confirm(id) }, true
	}
	return "", nil, false
}
func (c *commandLine) wait() error {
	for {
		s := c.app.controller.Snapshot()
		for _, event := range c.app.controller.Events(c.sequence) {
			c.sequence = event.Sequence
			c.say(fmt.Sprintf("%d %s %d/%d", event.Sequence, stageTitle(event.Stage), s.Current, s.Total))
		}
		if text, _, ok := c.prompt(); ok {
			c.say(text)
			c.say(cliMessage("cliAck"))
			return nil
		}
		if s.KeyboardOpen || s.CivSwitchActions.ControlCenterLive {
			return nil
		}
		if !s.Busy {
			switch s.Stage {
			case session.Indeterminate:
				return cliFail(exitIndeterminate, locales.RecoveryRequired())
			case session.Failed:
				return cliFail(exitError, locales.SessionFailed())
			}
			return nil
		}
		var manual <-chan struct{}
		if c.app.manual != nil {
			manual = c.app.manual.Changes()
		}
		select {
		case <-c.app.controller.Changes():
		case <-manual:
		case <-c.pulse:
		case <-c.signals:
			return c.interrupt()
		}
	}
}
func (c *commandLine) acknowledge(flag string) error {
	text, ack, ok := c.prompt()
	if !ok {
		return cliFail(exitBlocked, locales.CommandRejected())
	}
	c.say(text)
	if err := c.consent(flag, "console-step", locales.ManualAcknowledge()); err != nil {
		return err
	}
	if !ack() {
		return cliFail(exitBlocked, locales.CommandRejected())
	}
	return c.wait()
}

// interrupt maps Ctrl-C onto the controller's own semantics: Cancel before
// anything reached the console, Stop after. Without a running operation it
// only ends this command line; a verified session is left to Finish.
func (c *commandLine) interrupt() error {
	s := c.app.controller.Snapshot()
	if !s.Busy {
		return cliFail(exitCancelled, cliMessage("cliDeclined"))
	}
	var err error
	if s.SideEffects {
		_, err = c.app.controller.Stop()
	} else {
		_, err = c.app.controller.Cancel()
	}
	if err != nil {
		return &cliFailure{exitBlocked, err}
	}
	if err := c.wait(); err != nil {
		return err
	}
	return cliFail(exitCancelled, cliMessage("cliDeclined"))
}
func (c *commandLine) startInput() {
	if c.lines != nil {
		return
	}
	c.lines = make(chan cliLine)
	go func() {
		defer close(c.lines)
		scanner := bufio.NewScanner(c.in)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			c.lines <- cliLine{text: scanner.Text()}
		}
		if err := scanner.Err(); err != nil {
			c.lines <- cliLine{err: err}
		}
	}()
}
func (c *commandLine) readLine() (string, error) {
	c.startInput()
	select {
	case line, ok := <-c.lines:
		if !ok {
			return "", io.EOF
		}
		return line.text, line.err
	case <-c.signals:
		return "", c.interrupt()
	}
}
func (c *commandLine) runShell() (err error) {
	c.shell = true
	defer func() {
		c.shell = false
		// EOF and a failed script never abandon a live writer/host.
		if c.app.controller.Snapshot().Busy {
			cleanup := c.interrupt()
			if err == nil {
				err = cleanup
			}
		}
	}()
	for {
		if c.terminal {
			fmt.Fprint(c.stderr, "any2jb> ")
		}
		line, readErr := c.readLine()
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		args, parseErr := cliArguments(line)
		if parseErr != nil {
			return parseErr
		}
		if len(args) == 1 && args[0] == "exit" {
			return nil
		}
		if err := c.execute(args); err != nil {
			if !c.terminal {
				return err
			}
			c.say(localize(err.Error()))
		}
	}
}

// Scripts accept whitespace-separated arguments, quotes and backslash escapes,
// or a JSON argv array for lossless paths. There is no expansion or shell execution.
func cliArguments(line string) ([]string, error) {
	if strings.HasPrefix(strings.TrimSpace(line), "[") {
		var args []string
		err := json.Unmarshal([]byte(line), &args)
		return args, err
	}
	var args []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	for _, r := range line {
		if escaped {
			word.WriteRune(r)
			escaped = false
			started = true
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote, started = r, true
		case ' ', '\t':
			if started {
				args = append(args, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if quote != 0 || escaped {
		return nil, errors.New(cliMessage("cliInvalidArguments"))
	}
	if started {
		args = append(args, word.String())
	}
	return args, nil
}
func runCLI(args []string) int {
	cliAttachConsole()
	c := newCommandLine(os.Stdin, os.Stdout, os.Stderr, cliTerminal(os.Stdin))
	c.signals = make(chan os.Signal, 1)
	signal.Notify(c.signals, os.Interrupt)
	defer signal.Stop(c.signals)
	defer c.close()
	err := c.execute(args)
	if err != nil {
		c.say(localize(err.Error()))
	}
	return cliExitCode(err)
}
