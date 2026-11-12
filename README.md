# any2jb

A native desktop manager for ns2jb, starting with Apple Silicon macOS 14+.
This is the development shell only. It does not launch a host, communicate
with a console, or check hardware readiness.

## Develop

Install Go **1.27.3**. No Bun or WebView runtime is needed.
The MyGo library and local CLI tool are pinned together to
`a4b6734f7715c9f73709092f7bae80d861572c03` in `go.mod`.

```sh
go tool mygo dev
```

For explicit local engine/helper staging, see
[developer launch](docs/developer-launch.md). Packaged builds ignore that option.

The same binary is a command line when given arguments, for example
`any2jb detect --json` or `any2jb session --script steps.txt`. See the
[command line](docs/cli.md).

Run the full check suite, including a windowless native UI test, and package
the app with one command:

```sh
go test ./... && go tool mygo i18n check && go tool mygo vet . && go vet ./... && go tool mygo build
```

Or run the main checks separately:

```sh
go test ./...
go tool mygo build
```

On Apple Silicon macOS the build produces an app and DMG in
`build/darwin-arm64/`. Local builds use ad-hoc signing, not Developer ID
signing or notarization. Linux and Windows are later product targets.
Console and USB hardware behavior is **not verified**.

The build runs a fail-closed redistribution check over packaged input resources.
It first fetches the pinned official Node 24.22.1 arm64 archive into `.cache/`,
verifies its release digest, and stages the binary and full license notice.
Node is not committed to Git. Ordinary Go tests stay offline.
All six host profiles remain blocked by unresolved inherited rights; no engine
or bootstrap is bundled. See the [redistribution assessment](docs/redistribution.md)
and [release blockers](docs/parity-ledger.md#redistribution-release-blockers).
Do not bypass the build command for release packages.
See [runtime staging](docs/runtime-staging.md) for the engine adapter boundary
and remaining payload blockers.
See [packaging and updates](docs/releases.md) for reproducible arm64 builds,
the hardware-free packaged `--self-test`, signing status and update release gates.

Run Go tests and the hardware-free upstream regression harness together with
`go run ./cmd/check -reference-root /absolute/path/to/pinned/mac2jb`; it finds
the prepared Zig 0.15.2 install and verifies its version on its own.
See [regression checks](docs/regression-checks.md) for runtime assets, Zig
prerequisites, native dry-runs and explicit skip reasons.

## UI strings and assets

Add every app-visible string to `locales/en.json` and its translation to
`locales/zh-CN.json`, then run `go tool mygo generate`. Commit
`locales/catalog_gen.go` so plain Go builds and tests work without
generation. MyGo follows the system language among the shipped catalogs
(English and Simplified Chinese). See [localization](docs/localization.md)
for the catalog rules and zh-CN term list, and
[accessibility](docs/accessibility.md) for the view rules every screen
follows.

`resources/icon.png` is the generated MyGo default icon, retained as an
explicit placeholder until any2jb artwork is ready.

## Architecture and terminology

- [Domain glossary](GLOSSARY.md)
- [Direct Go orchestration over upstream engines](docs/adr/0001-direct-go-orchestration-over-upstream-engines.md)
- [Phase 1 release boundaries](docs/adr/0002-split-phase1-release-into-p1a-and-p1b.md)

The shell has no exploit behavior. Future views will call the Go session
controller rather than invoking platform helpers directly.
