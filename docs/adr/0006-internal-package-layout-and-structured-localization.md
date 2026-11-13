# 0006: Standard internal package layout and structured catalog localization

Status: accepted.

## Context

`package main` at the repository root mixed the desktop entrypoint, preferences,
production adapter composition, 15 native MyGo view files, 8 Cobra CLI files, a
155-rule English substring localizer (`localize.go`), generated `locales.ts`,
and 24 root test files alongside top-level domain packages (`session`, `engine`,
`retained`, `transport`, `readiness`, `civswitch`, `savekit`, `launcher`,
`driver`, `diagnostics`, `manifest`). Subsystem boundaries leaked in four
places:

1. `session` carried per-tool/folder regexes, a `savekit` import, and runtime
   type assertions (`ParameterizedToolRunner`, CivSwitch optional interfaces),
   while `launcher` acted as a pass-through wrapper over `savekit` and
   `manifest`.
2. Four packages (`engine`, `retained`, `savekit`, `transport`) duplicated
   process-group pipe startup, bounded monotonic NDJSON reading, and graceful
   `SIGTERM`-to-`SIGKILL` teardown.
3. Readiness check identifiers, blocking predicates, and Stage 1 row projection
   were split across string literals in `session`, `readiness`, UI views, and
   CLI commands, while `transport.Linux` forwarded eight methods over
   `transport.Darwin` and `usbgadget` depended upward on `readiness` for gadget
   naming rules.
4. `localize.go` translated adapter and controller errors by matching English
   prefixes and substrings in a fragile fixed order.

## Decision

1. **Repository layout and presentation seams**: Keep only `cmd/` (`any2jb`,
   `prepare-runtime`, `check-package`, `check`, `evidence`), `internal/*`,
   `locales/`, `resources/`, `docs/`, and `scripts/` at the top level. Split the
   former root `main` package into `cmd/any2jb` (entrypoint), `internal/app`
   (shared preferences, saved-bootstrap state, and production composition),
   `internal/ui` (native MyGo views and `ui.Tester` suites), `internal/cli`
   (Cobra command tree and session shell), and `internal/parity` (AST control
   inventory and differential parity helpers). Set `bindings` in `mygo.json` to
   `build/mygo.ts` so `mygo generate` and `mygo build` keep the repository root
   free of generated TypeScript files.
2. **Pure `internal/session` seam and folded `launcher`**: Keep
   `internal/session` restricted to the Go standard library with explicit
   `ToolRunner.Validate`, `ToolRunner.RunWith`, and `CivSwitchAdapter` methods
   instead of runtime type assertions. Fold `launcher` archive validation and
   preparation into `internal/retained`.
3. **Canonical readiness model and Stage 1 projection**: Define `Check*` and
   `GateUSBHID` constants plus route/blocking predicates on `session.Check`, and
   project the four Stage 1 installer rows once in
   `internal/readiness/projection.go`.
4. **Shared process supervision and unified USB HID host**: Centralize
   supervised child process-group startup, bounded NDJSON framing, and
   termination in `internal/procgroup`. Move `OwnGadgetPrefix` and
   `ValidUDCName` into `internal/usbgadget`, and configure Linux ConfigFS USB
   HID through OS hooks on `internal/transport` rather than a forwarding wrapper
   struct.
5. **Structured catalog localization**: Replace `localize.go` substring
   rewriting with parameterized catalog messages in `locales/en.json` and
   `locales/zh-CN.json` backed by `locales.Text`, `locales.WrapError`, and
   `locales.LocalizeError` in `locales/text.go`.

## Consequences and evidence

`CGO_ENABLED=0 go test ./...`, `-race` suites, `go tool mygo i18n check`,
`go tool mygo vet .`, `./scripts/check-platforms.sh`, `cmd/check` 3-way
differential parity, and `any2jb --self-test` on the packaged bundle verify
that session safety gates, UI/CLI parity, localization completeness, and wire
compatibility remain unchanged.
