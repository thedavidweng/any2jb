# Runtime staging

`engine.Stage` stages the bundled runtime once under
`<app support>/runtime/<app version>/`. Call it outside the UI thread at startup.
Pass the actual MyGo merged resource root as `Options.Assets`, the app support
path, app version and `manifest.Load()` result. Development tests must inject
the resource root: `go run` and `go test` do not merge platform overlays.
The production entrypoint calls it off the UI thread with MyGo's
`PathResources`, `PathUserData` and `App.Version()`, then initializes the
controller with real adapters. Missing resources become a readiness blocker.

The returned root contains `runtime/node`, `runtime/bridge.cjs`, a provenance
record `stage.json`, and a read-only `profiles.json` serialized from the same
canonical manifest loaded by Go.
This generated copy is not a second editable configuration. Pass that path
and a profile ID to the bridge for metadata. `engine.NewHostRunner` alone
authorizes developer launches and supervises the process group. Its bridge
launch mode joins upstream connection/native/qualification APIs to a
workspace-local pipeline and structured JSON events. See [host runner](host-runner.md).

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

All six engine profiles remain blocked from packaging. Their source trees,
bootstraps and checksum inventories must not enter resources or app bundles
until redistribution is resolved and each permitted packaged file is recorded
in the canonical manifest. `Stage` preparing Node is not proof that an engine
can launch. Packaged host runners must check
`DistributionFor(profileID).Available()` and `VerifyProfile` before execution
or bootstrap transfer.

### Explicit local developer staging

For local development only, pass both `Options.DeveloperSourceRoot` (the pinned
mac2jb clone) and `Options.DeveloperProfileID` to `Stage`. A developer entrypoint
or test may obtain that explicit root from `ANY2JB_REFERENCE_ROOT`. `Stage`
never reads the environment itself. Packaged startup must leave both options
empty; the build hook does not set them and still rejects blocked engine files.

The developer stage lives separately at
`<app support>/runtime/developer/<app version>/<profile ID>/`. The selected
profile's entire verified source inventory, bootstrap, distribution checksum
manifest and upstream/local-patch provenance are copied with their canonical
relative paths. Other profile trees and mac2jb GUI scripts are not copied.
`VerifyProfile` must pass on the source before copying and on the copy before
activation. Reuse verifies the source, staged profile and sealed runtime again.

`engine.ReadRecord(root)` exposes `source: developer-staged`,
`redistributable: false`, profile ID, upstream, revision and source digest.
Diagnostics can therefore say **developer-staged, not redistributable** without
exposing a personal source path. Canonical distribution flags stay unchanged.
Developer host runners must explicitly request this path, obtain a successful
`Stage` result, match its selected profile, and call `VerifyProfile` before
execution. Reading a record alone is not authorization or integrity evidence.
The metadata-only bridge retains its packaged distribution gate; development
host execution belongs to the Go runner.

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
ANY2JB_REFERENCE_ROOT=/path/to/pinned/mac2jb go test ./engine -run TestPinnedDeveloperStaging -v
```

The Node check is opt-in, local Apple Silicon integration: stage the verified
Node binary, check `--version`, and exercise the bridge's blocked-profile
response. The pinned-source check stages all six selected profiles separately
using fixture native sidecars, verifies provenance, rejects modified source
metadata/tampered stages, and confirms packaging still rejects their bootstraps.
Both opt-in checks skip when their environment variable is absent. Neither
executes an exploit or communicates with hardware.
