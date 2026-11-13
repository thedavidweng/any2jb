# macOS packages and authenticated updates

## Release status

This is a development package, not a distributable P1a release. The app identity
is `io.github.thedavidweng.any2jb`; `mygo.json` owns the application version.
`CHANGELOG.md` has the matching version section.

Production update public key: **NOT CONFIGURED, RELEASE BLOCKER**.
Production update channel: **NOT CONFIGURED, RELEASE BLOCKER**.
The `updates` object is deliberately absent from `mygo.json`. MyGo therefore
embeds no update feed or key, reports updates disabled and rejects `Check` with
`ErrUpdatesDisabled`. There is no unsigned update or source-download route.
Do not use a dummy valid public key or a test key in a production package.

All engines, bootstraps, USB binaries and drivers remain absent. The only
runtime payload is the pinned official Node binary and any2jb bridge. Notices
for Node (including its bundled libraries) and MyGo are included. The Go module
dependency notice inventory remains a release requirement, as does the
[redistribution review](redistribution.md).

## Reproduce a local package

Build on Apple Silicon macOS with Go **1.27.3** and the MyGo library/CLI revision
in `go.mod`, currently `a4b6734f7715c9f73709092f7bae80d861572c03`.
Node **24.22.1** is fetched as an official prebuilt archive by the build hook;
its archive, binary and notice digests live only in `manifest/profiles.json`.
Neither a system Node nor Bun is used to build or run the native app.
Xcode Command Line Tools provide `codesign`, `iconutil` and `hdiutil`.
The local validation toolchain was Xcode **27.2**, build **27B52**.
The full developer regression check also requires **Zig 0.15.2** for pepsipu's
payload rebuild. It is not part of the shipped runtime and is not needed just
to package the app. Use the official prebuilt compiler (verify its minisign
signature), managed by the existing `mise` installation if available. `cmd/check`
finds that install itself and verifies its version; `-zig /absolute/path/to/zig`
overrides the lookup. No global activation is necessary.

```sh
go test ./...
go tool mygo i18n check
go tool mygo vet .
go vet ./...
go tool mygo build -platform darwin/arm64 -sign - -skip-notarize
go run ./cmd/check-package -merged build/darwin-arm64/any2jb.app/Contents/Resources
build/darwin-arm64/any2jb.app/Contents/MacOS/any2jb --self-test
codesign --verify --deep --strict build/darwin-arm64/any2jb.app
codesign -d --entitlements - build/darwin-arm64/any2jb.app/Contents/Resources/runtime/node
go run ./cmd/check -reference-root /absolute/path/to/pinned/mac2jb \
  -runtime-assets "$PWD/resources/darwin-arm64"
```

The build runs `prepare-runtime` then `check-package`; do not bypass it with
`-skip-build-command`. That gate runs before MyGo assembles the bundle, so the
release steps also run it over the merged `Contents/Resources` of the app. The
merged gate resolves each component at its packaged path instead of its source
platform directory, accepts the icon, asset catalog and language files MyGo
writes into the bundle itself, and rejects a blocked or excluded component, a
blocked host, an unrecorded resource, a changed digest, and a bundled component
without its recorded third-party notice or with a notice file that is not
bundled. A first build requires HTTPS access to the pinned Node
archive. The verified archive cache and generated Node binary are ignored by
Git. Reproducible here means pinned inputs and repeatable checks, not
byte-identical code signatures, timestamps or DMG filesystem metadata.

The official Node executable is already signed with JIT entitlements.
Ad-hoc builds preserve those bytes and entitlements. Do not add a
`macos.helperEntitlements` override just to repeat `allow-jit`: MyGo re-signs
an explicitly overridden helper even in an ad-hoc build, changing its digest.
The final package must retain executable permissions, `allow-jit` and the
manifest-pinned Node digest.

### Signing and notarization

Local ad-hoc signing is not Developer ID signing and does not establish
Gatekeeper acceptance on another Mac. No Developer ID Application identity was
available during ticket 25 validation; notarization was explicitly skipped.
No signing credentials, update private keys or published artifacts belong in
the repository.

**Developer ID release blocker:** MyGo re-signs nested Mach-O files when given
a Developer ID. That changes Node's whole-file digest, which runtime staging
correctly rejects. Before a real signed release, implement and review a
post-sign payload identity strategy that preserves the verified upstream
identity and authenticates the packaged bytes. Do not weaken `manifest.Verify`,
silently update the canonical upstream digest, or claim a signed build passes
until its packaged self-test actually passes.

Candidate strategies for that review are (1) protect the official, unchanged
Node inside a verified archive and extract it with its original signature, or
(2) generate a separate authenticated post-sign package inventory while retaining
the canonical upstream digest/provenance. Neither is implemented. Changing the
canonical upstream digest to whatever signing produced is not a strategy.

After that blocker is resolved and the owner authorizes credential use,
configure `macos.signingIdentity` and `macos.notarize.keychainProfile` using
MyGo's documented configuration, then build without `-sign -` or
`-skip-notarize`. MyGo signs nested code inside out with hardened runtime,
submits the DMG and staples the result. A release must pass `codesign --verify`,
`xcrun stapler validate` and `spctl --assess`, plus the packaged self-test.
Do not create/export identities or notary credentials as part of an ordinary
build. If credentials are absent, explicitly record that Developer ID signing
and notarization were skipped.

Signing/notarizing the user-space application does **not** approve an unsigned
kernel extension or make it usable under default macOS security policy. No
build step changes SIP, Recovery or startup security. See
[driver security](driver-security.md).

## Update release gate

Use MyGo's updater, not a second signature format or downloader. An authorized
release setup must add the real base64 Ed25519 `updates.publicKey` and a
reviewed HTTPS `updates.url` or `updates.github` to `mygo.json`. Keep the
production private key outside the repository; only authorized release
infrastructure supplies it to MyGo. No key setup or upload is part of local
validation.

MyGo signs the SHA-256 of each update archive. Its `Update.Install` downloads
to a temporary location, verifies the configured public key before extracting
or replacing the app, and refuses wrong-key, unsigned or modified bytes.
Application update delivery does not authorize any engine/payload update:
each changed upstream revision, file digest and redistribution decision still
requires review in the canonical manifest. Never fetch an upstream branch
automatically. Existing immutable per-version runtime stages are not replaced
in place.

`go test . -run TestMyGoUpdateSignatureBoundary -v` builds an isolated temporary
test `.app` with a throwaway Ed25519 key, serves local fixture bytes through
MyGo's `UpdateSource`, and exercises the real `Check` / `Install` boundary.
Wrong-key and modified archives fail authentication; an unsigned manifest is
rejected. A correctly signed archive reaches bundle validation, then deliberately
fails because it contains no `.app`. Nothing is installed or relaunched. This
checks signature enforcement, not a complete successful update installation.
The private test key stays in the test's temporary directory and is removed
with it. Production builds still have no update key or channel.

**Session integration release blocker:** before exposing installation, wire
a native update action through the app's session owner. It must require no
active operation, transport, host, retained RPC or tool lease, prevent a new
session while installation is in flight, then call MyGo's verified installer
and relaunch only after successful installation and cleanup. Do not enable the
background updater plugin, whose install/relaunch path does not know session
ownership. A missing key/channel must remain visibly unavailable, not be
reported as “up to date.”

## Supported hardware matrix

| Target | Package | Console / USB evidence |
| --- | --- | --- |
| Apple Silicon, macOS 14+ | Phase 1 target, arm64 Node only | Not verified; engine/USB redistribution blocked |
| Intel macOS | Unsupported; no universal release | Not verified |
| Linux amd64/arm64 | Phase 2 target, GTK3 (`webview: false`) + x64/arm64 Node 24.22.1 | Not verified; ConfigFS HID needs real `/sys/class/udc` hardware; engine redistribution blocked |
| Windows amd64/arm64 | Phase 3, not a release target | Not verified; ordinary USB-C is not device-mode HID |

The package smoke test proves local integrity and bundled runtime execution
only. It never opens a console session, transmits commands, accesses USB or
validates an exploit. Hardware acceptance remains a separate release gate.

## Local evidence (2026-11-10)

On an arm64 Mac with the toolchain above:

- `CGO_ENABLED=0 go test ./...` passed, also with
  `ANY2JB_PACKAGED_RESOURCES` set to the built app's resource directory.
- MyGo i18n/vet and `go vet ./...` passed.
- Bundled Node ran `engine/bridge.test.cjs` and `manifest/profiles.test.cjs`:
  3 tests passed.
- The full `cmd/check` harness passed with Zig 0.15.2, including all six
  source/bootstrap inventories, the real-engine loopback partial-reply test,
  simulated controller tests, upstream pepsipu tests, payload rebuild and four
  native helper offline checks. The rebuilt payload was 4757 bytes and matched
  SHA-256 `d590cbfc5d9152c24d5a36f083f8470a2f3164be6b78130129d44132b939701a`.
  The optional local OS inspection and physical hardware checks were skipped.
- The explicit ad-hoc build above produced the app and DMG;
  `codesign --verify --deep --strict` passed. Node's preserved
  `com.apple.security.cs.allow-jit` entitlement was present.
- The packaged binary printed the following actual self-test result:

```text
any2jb self-test passed: integrity, Node and bridge; no console or USB access
```

Developer ID signing, notarization, Gatekeeper acceptance on a second Mac,
successful update replacement/relaunch, console behavior and USB hardware were
**not verified**. No artifact was published.
