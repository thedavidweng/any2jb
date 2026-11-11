# Explicit local developer launch

Packaged engines, native USB helpers and several tools remain redistribution
blocked. Developer staging is a local opt-in, not permission to redistribute
those assets or a statement of hardware support.

Create an untracked configuration file outside the repository:

```json
{
  "sourceRoot": "/absolute/path/to/pinned/mac2jb",
  "assets": "/absolute/path/to/any2jb/resources/darwin-arm64",
  "installerPython": "/absolute/path/to/python3"
}
```

- `sourceRoot` is the existing mac2jb checkout at the canonical pinned revision,
  including the selected engine and integration assets.
- `assets` is the prepared runtime resource root. `go run` does not merge MyGo
  platform overlays; use `resources/darwin-arm64` after preparing the pinned Node
  runtime with `go run ./cmd/prepare-runtime`.
- `installerPython` is optional. When provided it must be an absolute executable
  file. It is passed only to the CivSwitch persistence installer; omitting it
  leaves that action unavailable. No interpreter is searched for on PATH.

Both required directories and the config file must exist and use absolute paths.
Invalid/missing paths stop launch visibly. Paths are local configuration and
must not be committed or shared in diagnostics.

Launch on Apple Silicon macOS:

```sh
ANY2JB_DEVELOPER_CONFIG=/absolute/path/to/developer.json go run .
```

`main` is the only reader of `ANY2JB_DEVELOPER_CONFIG`, through
`internal/devlaunch`. It reads it only when `mygo.IsDev()` is true **and**
`mygo.App.IsPackaged()` is false. A production binary or any packaged bundle
ignores the option, including malformed paths. `developer_launch_test.go` builds
the probe in `testdata/devlaunch` both ways and checks both outcomes.
No adapter reads an environment variable to authorize itself.

The UI visibly labels the entire launch **developer-staged, not redistributable**.
The selected profile at startup becomes `engine.Options.DeveloperProfileID`;
`sourceRoot` becomes `DeveloperSourceRoot`. The host and bootstrap loader still
require successful `Stage`, staged provenance and `VerifyProfile`. Apply another
profile and relaunch with the same option to stage that profile; switching the
selection alone never authorizes a different engine.

Startup compiles and seals the USB helpers once through
`transport.BuildDeveloperHelpers`, which verifies the pinned source and never
initializes USB. Keyboard input receives that in-memory build authorization.
CivSwitch receives the same build's composite helper path and digest and checks
them again before use, alongside its source/payload checks. The driver gate
receives the explicit source root, but still requires informed consent before
privileged installation. CivSwitch also receives `DeveloperInstallerPython`.
Retained tool staging plugs into the single production composition point using
the same `engine.Options.DeveloperSourceRoot`, never another launch switch.

The launch option does not initiate USB, sudo, a console connection, or a tool.
User actions and their normal readiness/confirmation gates still control them.
Do not use those actions for hardware-free validation. The automatic tests use
fake helpers and the structured fake-engine fixture, not a console emulator.
