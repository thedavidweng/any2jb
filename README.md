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

## UI strings and assets

Add every app-visible string to `locales/en.json`, then run
`go tool mygo generate`. Commit `locales/catalog_gen.go` so plain Go builds
and tests work without generation. MyGo follows the system language among
the shipped catalogs, currently English only.

`resources/icon.png` is the generated MyGo default icon, retained as an
explicit placeholder until any2jb artwork is ready.

## Architecture and terminology

- [Domain glossary](GLOSSARY.md)
- [Direct Go orchestration over upstream engines](docs/adr/0001-direct-go-orchestration-over-upstream-engines.md)
- [Phase 1 release boundaries](docs/adr/0002-split-phase1-release-into-p1a-and-p1b.md)

The shell has no exploit behavior. Future views will call the Go session
controller rather than invoking platform helpers directly.
