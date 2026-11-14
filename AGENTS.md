# AGENTS.md

Canonical instructions for coding agents working in `any2jb`. Keep all repository-level agent rules in this file; never create `CLAUDE.md`, `.cursorrules`, `.windsurfrules`, `.clinerules`, `GEMINI.md`, or other tool-specific duplicates (enforced by `mise run conventions`).

## Overview

`any2jb` is a cross-platform (macOS Apple Silicon + Linux x64/arm64) native desktop (`github.com/egoist/mygo/ui`) and Cobra CLI fork of `cpypwn/mac2jb`.

- Core package layout: `cmd/any2jb` owns `mygo.App` startup; views live in `internal/ui` (`github.com/egoist/mygo/ui` only); CLI lives in `internal/cli`; domain and runtime packages (`internal/session`, `internal/engine`, `internal/plugins`, `internal/retained`, `internal/civswitch`, `internal/artemis`, `internal/devtools`, `internal/manifest`) never import MyGo.
- Upstream alignment & redistribution: `internal/manifest/profiles.json` is the single canonical lock for host profiles, bundled runtime sidecars, and all 29 unbundled on-demand plugins (`bundled: false`, SHA-256 verified before staging). Keep `docs/parity-ledger.md` and `docs/cli-parity.md` synchronized with UI controls, CLI flags, and upstream `cpypwn/mac2jb` revisions.
- Localization: `locales/en.json` is the source catalog across all 18 Nintendo Switch 2 system languages (`locales.Switch2Languages`). Regenerate typed accessors with `go tool mygo generate .` and verify completeness with `go tool mygo i18n check` and `go test ./internal/ui`.

## Toolchain & Verification

Tools are pinned in `mise.toml` (`go`, `bun`, `node`, `gofumpt`, `golangci-lint`, `jactionlint`) and `package.json` (`@biomejs/biome`).

- Full repository gate (Go + JS + i18n + cross-platform compile + workflows):
  ```sh
  mise run check
  ```
- JavaScript / JSON / runtime sidecar checks (`resources/runtime/*.cjs`, `internal/**/*.cjs`, `scripts/*.mjs`):
  ```sh
  bun lint
  bun check
  ```
- Upstream `cpypwn/mac2jb` & ecosystem pin check:
  ```sh
  bun run upstream:verify   # offline manifest/docs consistency gate
  bun run upstream:check    # live GitHub/GitLab drift report
  ```
- Native package gate:
  ```sh
  go run ./cmd/prepare-runtime
  go run ./cmd/check-package
  ```

Run `mise run check` before pushing changes. Architecture decisions belong in `docs/adr/`.
