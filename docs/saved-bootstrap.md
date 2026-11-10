# Saved bootstrap identity

Completing either guided manual or supervised USB setup creates a
`session.BootstrapRecord` only after the user confirms the console finished
saving and adapter cleanup succeeds. SAVE submission and USB report
acknowledgements are not proof that the console saved anything.

The native window's session watcher writes the record to
`os.UserConfigDir()/any2jb/bootstrap.json`, beside `preferences.json`, through
a private temporary file and atomic rename. The record contains profile ID,
pinned revision, source SHA-256, bootstrap SHA-256, save name, edition,
confirmation and a UTC timestamp. It contains no pairing data or console save
contents. An I/O failure is visible and must not be treated as persisted evidence.

On relaunch, `initializeSession` loads it and calls `UseSavedBootstrap`.
All identity fields must match; equal bootstrap bytes across hosts are not
interchangeable. A mismatched record leaves launch blocked and asks for matching
setup or explicit confirmation. Malformed records fail visibly rather than
silently manufacturing a replacement. Profile/edition changes invalidate the
in-memory assumption. Readiness checks, a host process and retained RPC never
survive relaunch.

If a program was saved outside the app, **Use saved bootstrap** displays the
exact profile, edition, name, revision and bootstrap digest. **I confirm this
exact saved bootstrap** explicitly records the user's evidence. The app cannot
read arbitrary SmileBASIC program contents and does not infer their identity
from a filename.

Before submitting SAVE, the user must explicitly authorize replacement if that
program already exists. Neither input adapter accepts an overwrite prompt.
The user handles the console prompt and then chooses **I saved the bootstrap**.
Full uses `SAVE "<saveName>"`; demo uses the source-collection `SAVEV "TXT:..."`
sequence, as documented in [guided manual setup](guided-manual.md).

A compatible record plus fresh verified readiness enables **Start Jailbreak**
on keyboard HID. The host must listen and supply valid session-bound pairing
before LOAD, its separate confirmation, RUN, REMOVE and START. The existing
console acknowledgements remain mandatory; “one-click” never means blindly
accepting an unobserved prompt. Guided manual mode remains labelled separately.
Only the structured host result can verify the exploit.

## Evidence

- `bootstrap_test.go`: confirmed setup, saved identity reload, fresh readiness,
  profile invalidation and saved launch without repeating setup.
- `bootstrap_view_test.go`: explicit reuse and overwrite permission.
- `transport/saved_bootstrap_test.go`: literal full/demo SAVE reports for every
  supported profile, one Enter only and no overwrite acceptance.
- `transport/typing_test.go`: LOAD prompt and command ordering.
- `manual_view_test.go`: native UI with the structured fake-engine fixture.

No console saves, USB enumeration or exploit execution have been hardware
verified. These tests use fakes and offline fixtures only.
