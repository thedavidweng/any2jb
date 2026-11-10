# any2jb

A desktop manager that prepares, launches and supervises an ns2jb session on a Nintendo Switch 2, and exposes post-exploit tools only once that session is proven.

## Session

**Verified session**:
A run whose host reported the complete upstream result contract (exploit verified, filesystem verified, restored and cleaned up). Nothing short of that is called verified.
_Avoid_: success, jailbroken, done

**Indeterminate session**:
A run that stopped after a side effect on the console (partial typing, pairing published, exploit progress) whose console state cannot be known from the host.
_Avoid_: failed, cancelled

**Guided manual launch**:
A launch where the user types session-bound commands on the console themselves because no capable input transport is available.
_Avoid_: one-click, fallback mode

**Host profile**:
One pinned ns2jb host variant together with its matching bootstrap, save name, command arity, port and capabilities.
_Avoid_: host version, fork

**Saved bootstrap**:
A bootstrap program the user saved on the console, trusted for reuse only through a recorded successful setup for the same host profile and edition, or explicit user confirmation.
_Avoid_: installed bootstrap

**Runtime engine**:
A pinned upstream Node.js session host (`pepsipu/ns2jb` or a compatible profile variant) staged once per app version as a read-only, digest-verified directory.
_Avoid_: mac2jb GUI scripts, mutable workspace

**Session workspace**:
A private per-run directory holding ephemeral pairing parameters, process metadata and `runs/` logs for a single session.
_Avoid_: runtime directory, shared cache

**Cancel**:
Ending a session before any side effect reaches the console, returning directly to `ready`.
_Avoid_: stop, abort

**Stop**:
Terminating an active session after console typing, pairing publication or runtime execution has begun, leaving the session `indeterminate` until the console is restarted.
_Avoid_: cancel, finish

**Finish**:
Closing a `verified` or `retained_rpc` session cleanly, releasing child processes and cleaning up the session workspace.
_Avoid_: stop, cancel

## Readiness

**Retained RPC lease**:
The live RPC endpoint of a verified held session, owned by one verified run, re-checked against its private evidence and a fresh loopback request before each tool.
_Avoid_: open port, connection

**Attached session**:
A verified retained session another app owns, which this app connects to for read-only tools and later disconnects from, leaving the original host running.
_Avoid_: adopted session, reconnect

**Read-only tool**:
A tool that observes or exports console data without any write path: RPC status, the system-code capture, the OunceCake exports and the `/sd` export.
_Avoid_: safe tool

**Readiness check**:
One independently observed prerequisite with a state of `requires attention`, `available but not verified` or `verified`, plus the next action.
_Avoid_: status light, health

## Parity tracking

**Parity ledger**:
The list of every user-visible mac2jb capability and how any2jb accounts for it.
_Avoid_: feature list, checklist

**Disposition**:
How a ledger row is carried into any2jb: `reused`, `reimplemented`, `gated` or `excluded`.
_Avoid_: status

**Evidence level**:
The strongest proof that a ledger row works: `not verified`, `unit-tested`, `integration-tested`, `hardware-verified` or `blocked`.
_Avoid_: status, verified (alone)

**Hardware evidence log**:
The validated JSON record a human tester files in `docs/hardware-evidence/` after running the hardware acceptance protocol on a real rig; the only basis for a `hardware-verified` ledger row.
_Avoid_: test result, manual check

**Experimental**:
A label on a ledger row whose upstream marks it as a prototype or hardware-unverified; independent of disposition and evidence level.

## Releases

**P1a**:
The first macOS release: core ns2jb flow with manual and USB input, diagnostics, retained RPC and read-only tools.

**P1b**:
The remaining Phase 1 macOS scope: save-writing tools, launcher saves, homebrew launching and CivSwitch, tracked in the parity ledger but not blocking P1a.
