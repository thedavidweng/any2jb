# Host profile choices

The default comes from `manifest/profiles.json` (`kurumuz`). Developer Options
exposes all six pinned host IDs, the editions each supports, its START argument
count, port and save name. The `manifest.Profile.SessionProfile()` projection
supplies these values to the session controller. It does not format commands,
provide transport discovery, grant redistribution permission or prove hardware
compatibility.

Unsupported demo, retained RPC, SaveKit, native HBLauncher and modloader options
have an explanation. The read-only tools of a retained-RPC host are integrated;
see [retained tools](retained-tools.md). SaveKit, native HBLauncher and
modloader controls are offered only on hosts that declare the capability, and
each stays disabled with its recorded reason until its components are staged.
Capability permission is not permission to write saves or launch unvalidated
software.

IZMYSH remains selectable for inventory, but cannot launch even from a developer
stage. Its nine-argument native qualification contract is not the ns2jb
filesystem verifier. The UI and readiness explain that its separate adapter is
not implemented; the runner refuses it before spawning any engine.

## Preferences

`os.UserConfigDir()/any2jb/preferences.json` stores only the selected profile ID,
edition (`full` or `demo`), optional LAN IPv4 override and input transport mode.
Apply validates them against the canonical manifest and writes through a private
temporary file and rename. Malformed or incompatible preferences report an error;
they are not silently replaced. A transport choice is not an observed capability.
Blank IPv4 requests network discovery, not an invented address.

When real adapters become available, the app calls `initializeSession` with that
environment. It restores the choices into a fresh controller, not readiness,
verification or a previous RPC lease. Any pending settings edit disables launch
and bootstrap preparation until Apply succeeds.

## Saved bootstrap integration

`SessionController.UseSavedBootstrap` checks `BootstrapRecord` against the exact
profile ID, revision, source digest, bootstrap digest, save name and edition, plus
successful setup or explicit console confirmation. Equal bootstrap bytes across
two host profiles do not make their records interchangeable. Profile selection
invalidates saved-bootstrap assumptions and readiness.

Canonical profiles cannot Start without a matching confirmed bootstrap.
[Saved-bootstrap persistence and setup](saved-bootstrap.md) record identity
only after the console save prompt is handled, then use this controller check on
reload. Do not treat the desktop preference file as proof of a console save.
The engine/input adapters consume profile-specific command metadata, never a
global START arity, port or save name.

## Host profile tests

- `manifest/session_test.go`: six-profile capability, edition, arity, port and
  save-name matrix against known pinned contracts.
- `session/profile_test.go`: unsupported editions, unconfirmed launch rejection,
  cross-profile saved-bootstrap rejection and invalidation, including hosts
  sharing the same bootstrap digest.
- `preferences_test.go`: default selection, validated save/reload and fresh
  controller configuration, without restoring readiness.
- `profile_view_test.go`: progressive disclosure, accessible disabled controls,
  explanations, stable controls across extra frames and unapplied-host launch
  prevention.

Windowless MyGo rendering was inspected at 640×720. No Switch, USB device-mode,
native packaged app or real console workflow was verified.
