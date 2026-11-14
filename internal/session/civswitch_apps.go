package session

import "context"

// CivSwitchFSHost is the optional CivSwitch FS supervisor surface. A route
// adapter that only probes the loader does not implement it.
type CivSwitchFSHost interface {
	StartCivSwitchFSHost(ctx context.Context, request CivSwitchFSRequest, event func(CivSwitchFSEvent)) error
}

// CivSwitchFSRequest selects both LAN peers of one CivSwitch FS run.
type CivSwitchFSRequest struct {
	SwitchIPv4    string
	HostIPv4      string
	WaitForLoader bool
}

// CivSwitchFSEvent is one verified supervisor observation. Only fixed fields
// are carried: console and host output is never forwarded as a message.
type CivSwitchFSEvent struct {
	Type    string
	Message string
	RunPath string
	LogPath string
}

// CivSwitchApps is the optional CivHomebrew and .civapp surface of the
// CivSwitch route. Each action still needs the verified loader and the
// companion on the console; none of them is implied by the loader alone.
type CivSwitchApps interface {
	CivHomebrewProbe(ctx context.Context, address string) (CivHomebrewState, error)
	CivHomebrewLaunch(ctx context.Context, address string, event func(CivHomebrewEvent)) error
	CivHomebrewInstallTrigger(ctx context.Context, address string, confirmedSaveReplacement bool, event func(CivHomebrewEvent)) (string, error)
	CivHomebrewRestoreTrigger(ctx context.Context, address string, event func(CivHomebrewEvent)) error
	CivHomebrewShutdown(ctx context.Context, address string) (CivHomebrewStop, error)
	CivHomebrewInstallPackage(ctx context.Context, address, packageSpec string, event func(CivHomebrewEvent)) (CivHomebrewApp, error)
}

// CivHomebrewState is the companion's own report on the console.
type CivHomebrewState struct {
	Mode     string
	Launcher bool
	FTP      bool
	Busy     bool
	Closed   bool
	Message  string
}

// CivHomebrewEvent is one fixed companion or installer observation.
type CivHomebrewEvent struct {
	Type    string
	Message string
}

// CivHomebrewStop reports whether the companion stopped or refused to stop
// because an app is in the foreground.
type CivHomebrewStop struct {
	Stopped bool
	Blocked bool
	Message string
}

// CivHomebrewApp identifies one installed CivHomebrew package.
type CivHomebrewApp struct {
	ID     string
	Name   string
	File   string
	Pinned bool
}

// StartCivSwitchFSHost runs the pinned CivSwitch FS host for one console and
// one host address. It returns once the supervisor is verified or failed; the
// held run path is recorded as soon as the FS and RPC proof arrives, and a
// deliberate stop releases both.
func (c *SessionController) StartCivSwitchFSHost(hostIPv4 string, waitForLoader bool) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.s.CivSwitch || !c.s.LoaderVerified || !c.eligible(Verified) {
		return c.reject(InvalidState)
	}
	if !validConsoleIPv4(hostIPv4) {
		return c.configurationError(InvalidConfiguration, "Enter the Mac or PC IPv4 address on the same reachable LAN as the console.")
	}
	if err := c.civSwitchAvailability(); err != nil {
		return c.configurationCause(Unsupported, err)
	}
	host, ok := c.env.CivSwitch.(CivSwitchFSHost)
	if !ok {
		return c.configurationError(Unsupported, "The CivSwitch FS host is not staged; install the CivSwitch FS plugin first.")
	}
	request := CivSwitchFSRequest{SwitchIPv4: c.s.ConsoleIPv4, HostIPv4: hostIPv4, WaitForLoader: waitForLoader}
	c.s.CivSwitchActions.FSHostLive, c.s.CivSwitchActions.FSRunPath = true, ""
	return c.civSwitchRun(func(ctx context.Context, _ string) error {
		err := host.StartCivSwitchFSHost(ctx, request, c.observeCivSwitchFS)
		c.mu.Lock()
		c.s.CivSwitchActions.FSHostLive = false
		c.mu.Unlock()
		if err != nil && ctx.Err() != nil {
			// A deliberate stop releases the held RPC and cleans the staged FS
			// itself; anything else is a real failure the supervisor reported.
			return nil
		}
		return err
	}), nil
}

// StopCivSwitchFSHost asks the supervisor to stop at its next safe checkpoint.
func (c *SessionController) StopCivSwitchFSHost() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.s.CivSwitchActions.FSHostLive || !c.s.Busy || c.requested != "" {
		return c.reject(InvalidState)
	}
	c.cancel()
	c.emit(Info, "", "Stopping the CivSwitch FS host at the next safe checkpoint.")
	return c.snapshot(), nil
}

func (c *SessionController) observeCivSwitchFS(event CivSwitchFSEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if event.RunPath != "" {
		c.s.CivSwitchActions.FSRunPath = event.RunPath
	}
	c.emit(Info, "", "civswitch-fs-"+event.Type)
}

// ProbeCivHomebrew verifies the companion on port 9027 and refreshes the
// launcher catalog when the console is idle in its launcher.
func (c *SessionController) ProbeCivHomebrew() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.civHomebrewDependent(func(ctx context.Context, apps CivSwitchApps, address string) error {
		state, err := apps.CivHomebrewProbe(ctx, address)
		if err != nil {
			return err
		}
		c.mu.Lock()
		c.s.CivSwitchActions.CivHomebrew = state
		c.mu.Unlock()
		return nil
	})
}

// LaunchCivHomebrew sends the temporary launcher to the open loader and keeps it
// running until StopCivHomebrew or the console closes it.
func (c *SessionController) LaunchCivHomebrew() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.civHomebrewDependent(func(ctx context.Context, apps CivSwitchApps, address string) error {
		c.mu.Lock()
		c.s.CivSwitchActions.CivHomebrewLive = true
		c.mu.Unlock()
		err := apps.CivHomebrewLaunch(ctx, address, c.observeCivHomebrew)
		c.mu.Lock()
		c.s.CivSwitchActions.CivHomebrewLive = false
		c.mu.Unlock()
		if err != nil && ctx.Err() != nil {
			// A deliberate stop shut the companion down and reaped the sender.
			return nil
		}
		return err
	})
}

// StopCivHomebrew asks the running launcher to close on the console.
func (c *SessionController) StopCivHomebrew() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.s.CivSwitchActions.CivHomebrewLive || !c.s.Busy || c.requested != "" {
		return c.reject(InvalidState)
	}
	c.cancel()
	c.emit(Info, "", "Stopping CivHomebrew on the console.")
	return c.snapshot(), nil
}

// InstallCivHomebrewTrigger replaces the game's single-player saves with the
// persistent CivHomebrew boot trigger after backing them up on the console.
func (c *SessionController) InstallCivHomebrewTrigger(confirmedSaveReplacement bool) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !confirmedSaveReplacement {
		return c.configurationError(InvalidConfiguration, "Installing the CivHomebrew boot trigger replaces the game's single-player save files. Confirm the replacement; the console backs them up first.")
	}
	return c.civHomebrewDependent(func(ctx context.Context, apps CivSwitchApps, address string) error {
		backup, err := apps.CivHomebrewInstallTrigger(ctx, address, true, c.observeCivHomebrew)
		c.mu.Lock()
		defer c.mu.Unlock()
		if backup != "" {
			c.s.CivSwitchActions.CivHomebrewBackupPath = backup
		}
		return err
	})
}

// RestoreCivHomebrewTrigger puts the original CivSwitch trigger back.
func (c *SessionController) RestoreCivHomebrewTrigger() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.civHomebrewDependent(func(ctx context.Context, apps CivSwitchApps, address string) error {
		return apps.CivHomebrewRestoreTrigger(ctx, address, c.observeCivHomebrew)
	})
}

// ShutdownCivHomebrew stops the companion, or reports why it cannot stop yet.
func (c *SessionController) ShutdownCivHomebrew() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.civHomebrewDependent(func(ctx context.Context, apps CivSwitchApps, address string) error {
		stop, err := apps.CivHomebrewShutdown(ctx, address)
		if err != nil {
			return err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.s.CivSwitchActions.CivHomebrew.Closed = stop.Stopped
		c.s.CivSwitchActions.CivHomebrew.Message = stop.Message
		return nil
	})
}

// InstallCivHomebrewPackage installs a pinned catalog app or a .civapp file the
// user chose. The adapter verifies pinned digests before sending anything.
func (c *SessionController) InstallCivHomebrewPackage(packageSpec string) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if packageSpec == "" {
		return c.configurationError(InvalidConfiguration, "Choose a CivHomebrew app to install.")
	}
	return c.civHomebrewDependent(func(ctx context.Context, apps CivSwitchApps, address string) error {
		app, err := apps.CivHomebrewInstallPackage(ctx, address, packageSpec, c.observeCivHomebrew)
		if err != nil {
			return err
		}
		c.mu.Lock()
		c.s.CivSwitchActions.CivHomebrewPackage = app
		c.mu.Unlock()
		return nil
	})
}

// civHomebrewDependent gates the CivHomebrew actions like every other
// loader-dependent action, then requires the reporter to have the surface.
func (c *SessionController) civHomebrewDependent(fn func(context.Context, CivSwitchApps, string) error) (Snapshot, error) {
	if !c.s.CivSwitch || !c.s.LoaderVerified || !c.eligible(Verified) {
		return c.reject(InvalidState)
	}
	if err := c.civSwitchAvailability(); err != nil {
		return c.configurationCause(Unsupported, err)
	}
	apps, ok := c.env.CivSwitch.(CivSwitchApps)
	if !ok {
		return c.configurationError(Unsupported, "CivHomebrew is not staged; install the CivHomebrew plugin first.")
	}
	return c.civSwitchRun(func(ctx context.Context, address string) error {
		return fn(ctx, apps, address)
	}), nil
}

func (c *SessionController) observeCivHomebrew(event CivHomebrewEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if event.Message != "" {
		c.s.CivSwitchActions.CivHomebrew.Message = event.Message
	}
	c.emit(Info, "", "civhomebrew-"+event.Type)
}
