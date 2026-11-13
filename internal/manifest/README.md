# Source manifest

`profiles.json` is the only editable host profile and component lock. Go embeds
it through `manifest.Load()`. The runtime bridge reads a serialized, immutable
copy of that same loaded manifest. Other Node callers can use `profiles.cjs`
and `profileById(id)` directly from this directory.
Do not copy `mac2jb`'s `host-profiles.json` into the app or runtime.

The six profile IDs are case-sensitive. Capabilities describe upstream host
support, not readiness, verified sessions, or redistribution permission.
Astral is mac2jb's custom `0.2.0-experimental` kurumuz derivative, not a separate
upstream repository. All six profiles retain an unresolved inherited ns2jb
distribution gate.

## Before packaging or exposing a tool

Each record's `distribution` object stores `decision` (`may-ship`, `blocked`,
`excluded`), `bundled`, the supplied `notice`, and the user-facing `reason`.
Use `m.DistributionFor(id)` and its `Available()` method as a separate
capability gate. Host support flags never override a distribution blocker.
Unknown IDs return an error. A source record with `bundled: false` is not a
working installed capability.

`m.CheckPackage(stagedResourceRoot)` rejects every unrecorded resource,
unbundled/denied file, symlink, digest mismatch and missing bundled artifact.
The MyGo `buildCommand` invokes this gate over `resources/` automatically.
Profiles are not packaging allowlists: before any host is shipped its rights
must be resolved and its individual packaged files recorded as components.
See [the redistribution assessment](../docs/redistribution.md) for scope,
GPL v2/v3 assessments, release blockers and alternate acquisition boundaries.

## Before execution or bootstrap transfer

Load the manifest once, select with `m.Profile(id)`, and call
`m.VerifyProfile(runtimeRoot, id)` before launching the host or sending its
bootstrap. Call `m.Verify(runtimeRoot, componentID)` before executing a recorded
sidecar. Both return `*IntegrityError` identifying the component on integrity
failure; unknown profiles return `*UnknownProfileError`. Missing files retain
`os.ErrNotExist` through error unwrapping.

Paths are relative to one immutable staged runtime, preserving the source and
bootstrap layout recorded in the JSON. There is no alternate source directory
lookup. A profile check verifies:

- The exact distribution `SHA256SUMS` bytes, then every listed regular file.
- No duplicate, traversal, symlink, or unrecorded source files.
- mac2jb's sorted JSON `[relative path, file SHA-256]` source digest.
- Separate pristine upstream checksum and local ABI3 patch manifests, when
  present, without applying pristine checksums to patched distribution files.
- The selected bootstrap's digest and byte equality with its host's bootstrap.

The runtime must remain immutable from verification through process execution.
Integrity verification checks content, not file ownership, code signing,
license obligations, process launch, or hardware behavior. Distribution checks
enforce recorded decisions, not legal clearance. Launch adapters own both
availability and integrity checks.

The C helper component records pin four mac2jb **sources** only.
No host or helper binary is bundled by this package. Node 24.22.1 arm64 and its
complete notice are pinned here; the build fetches the official archive into
an ignored cache and verifies both archive and extracted file digests.
`resourcePlatform` describes the pre-build source folder only. Runtime paths
remain relative to MyGo's merged resource root. When other artifacts are built/staged,
add their exact paths and SHA-256 digests to
`components` in this same manifest. Unrecorded binaries are rejected, not
trusted from source digests or an installed system runtime.

## Checks

```sh
go test ./...
node --test manifest/profiles.test.cjs
ANY2JB_REFERENCE_ROOT=/path/to/pinned/mac2jb go test ./manifest -run TestPinnedSourceInventory -v
```

The last check is optional and read-only. Use mac2jb revision
`ce1ce5f4f40b412cf83ba0a4f10ac53a850ed308`. It verifies all six actual source
inventories, bootstraps, provenance manifests, four helper source digests and
the excluded research notice. MyGo icon/license records are checked separately
against this repository's resource tree.
It does not execute an exploit or verify a console.
