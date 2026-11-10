# Runtime staging

`engine.Stage` stages the bundled runtime once under
`<app support>/runtime/<app version>/`. Call it outside the UI thread at startup.
Pass the actual MyGo merged resource root as `Options.Assets`, the app support
path, app version and `manifest.Load()` result. Development tests must inject
the resource root: `go run` and `go test` do not merge platform overlays.
The current UI does not yet call this API.

The returned root contains `runtime/node`, `runtime/bridge.cjs` and a read-only
`profiles.json` serialized from the same canonical manifest loaded by Go.
This generated copy is not a second editable configuration. Pass that path
and a profile ID to the bridge. The bridge emits JSON profile metadata or a
structured error; engine process launch and supervision belong to the host
runner, not this bridge.

Staging verifies component SHA-256 digests and native arm64 Mach-O headers
before atomic activation. Node's exact version is pinned by its binary digest
and release metadata, without executing unverified payloads. Directories and
executables become `0500`; the bridge and generated manifest become `0400`.
Reactivation verifies bytes, manifest content and non-writable permissions
without copying source assets again. A corrupted stage is discarded and
reported, not silently repaired during that activation.

This prevents accidental mutation, not a same-user adversary who can modify
the app and change permissions. Signing and safe child-process supervision
remain separate release gates.

`engine.NewWorkspace(appSupport)` exclusively creates a fresh
`runs/run-<random>/` directory with mode `0700`. Store pairing parameters and
other sensitive artifacts as `0600` files. Never use a prior run's directory.
The session owner stops its children before removing its own workspace.

## Node acquisition

The MyGo build hook runs `go run ./cmd/prepare-runtime` before the package gate.
It downloads the exact official Node 24.22.1 darwin-arm64 tar.gz into `.cache/`.
The archive digest was checked against the release's
`https://nodejs.org/dist/v24.22.1/SHASUMS256.txt` on 2026-11-10:

```text
89c682593d76cd44233fa53b1563264e7de9cbb189cad169135c5b4fadfbc0a7
```

The canonical manifest pins that digest plus the extracted binary and complete
license digests. A corrupt cache fails closed; it is never silently replaced.
Only `bin/node` and `LICENSE` are extracted. The binary and cache are ignored
by Git. No system Node is substituted. Release checksum signature validation
is not implemented; the digest was obtained from the official HTTPS endpoint.

The package gate checks the pre-merge `resources/` source layout.
`resourcePlatform: darwin-arm64` maps canonical `runtime/node` to the source
file `resources/darwin-arm64/runtime/node`. MyGo removes that platform prefix
when packaging; do not add it to runtime paths.

## Distribution and transport gates

All six engine profiles remain blocked and uncopied. Their source trees,
bootstraps and checksum inventories must not be staged until redistribution
is resolved and each permitted packaged file is recorded in the canonical
manifest. `Stage` preparing Node is not proof that an engine can launch.
The host runner must check `DistributionFor(profileID).Available()` and
`VerifyProfile` before execution or bootstrap transfer.

The four USB binaries remain unbundled pending the libusbd/APSL/source
obligations review. Their existing `.c` records identify source, not binaries.
`engine.VerifyUSB` is a separate transport gate. Unbundled helpers block only
USB input, not core runtime staging or guided manual launch. If a helper is
marked bundled, a missing/tampered helper is a corrupt payload and is rejected.
No `gui-host.cjs` or `host-profiles.json` is copied from mac2jb.

Zig 0.15.2 payload rebuild and all console/USB hardware checks remain
**not verified**. Nothing here clears an engine's redistribution blocker.

## Checks

```sh
go test ./...
node --test engine/bridge.test.cjs manifest/profiles.test.cjs
go run ./cmd/prepare-runtime
go run ./cmd/check-package
ANY2JB_RUNTIME_ASSETS="$PWD/resources/darwin-arm64" go test ./engine -run TestBundledNodeRuntime -v
```

The last check is opt-in, local Apple Silicon integration: stage the verified
Node binary, check `--version`, and exercise the bridge's blocked-profile
response. It does not execute an exploit or communicate with hardware.
