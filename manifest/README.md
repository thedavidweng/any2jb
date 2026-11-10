# Source manifest

`profiles.json` is the only editable host profile and component lock. Go embeds
it through `manifest.Load()`; the Node engine bridge uses `profiles.cjs` and
`profileById(id)`. Stage this directory's JSON and reader unchanged together.
Do not copy `mac2jb`'s `host-profiles.json` into the app or runtime.

The six profile IDs are case-sensitive. Capabilities describe upstream host
support, not readiness, verified sessions, or redistribution permission.
Astral is mac2jb's custom `0.2.0-experimental` kurumuz derivative, not a separate
upstream repository. All six profiles retain an unresolved inherited ns2jb
distribution gate.

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
This package verifies content, not file ownership, code signing, licenses,
process launch, or hardware behavior. Launch adapters own the verification call.

The initial component records pin the four mac2jb C helper **sources** only.
No host, helper binary, or Node runtime is bundled by this package. When those
artifacts are built/staged, add their exact paths and SHA-256 digests to
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
inventories, bootstraps, provenance manifests, and four helper source digests.
It does not execute an exploit or verify a console.
