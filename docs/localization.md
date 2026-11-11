# Localization

Catalogs live in `locales/`: `en.json` is the source, `zh-CN.json` is the
Simplified Chinese translation. After editing either, run
`go tool mygo generate` and commit `locales/catalog_gen.go`. MyGo picks the
catalog matching the system language; `mygo.App.Locale()` handles the default
detection, no app code needed.

## Enforcement

- `TestCatalogCompleteness` requires identical key sets and placeholders in
  `en` and `zh-CN`, so any string added later without its translation fails
  the test suite.
- `TestNoHardcodedUIStrings` rejects string literals passed to UI widget
  constructors, `Label` and `Placeholder`: new view text goes through the
  catalog.
- Text produced by the session, readiness, driver, retained, diagnostics and
  manifest adapters is mapped segment-wise in `localize.go`
  (`dynamicSegments`). Add one entry per adapter sentence, keeping `source`
  byte-identical to the adapter's English literal;
  `TestDynamicSegmentsMatchSourceCatalog` fails on drift. Entries are sorted
  longest-first at init, so a sentence matches its longest source.
- `TestAdapterMessagesAreLocalized` reads the adapters' own literals — every
  `Check` message and next action, and the errors `Availability`,
  `Unavailable` and `CivSwitchUnavailable` return — and requires each to be a
  segment or to be built from segments around its format verbs. Adapter text
  that the views pass through `localize` therefore cannot fall through in
  English.

## zh-CN term list

Use these consistently; never soften a risk statement.

| English | zh-CN |
| --- | --- |
| Verified session | 已验证会话 |
| Indeterminate session | 状态未知的会话 |
| Guided manual launch | 引导式手动启动 |
| Cancel | 取消 |
| Stop | 停止 |
| Finish | 结束会话 |
| Verified | 已验证 |
| Available but not verified | 可用但未验证 |
| Requires attention | 需要处理 |
| Readiness | 就绪状态 |
| Host profile | 主机配置 |
| Bootstrap | 引导程序 |
| Saved bootstrap | 已保存的引导程序 |
| Retained RPC | 保留 RPC |
| Console | 游戏机 |
| Advanced Tools | 高级工具 |
| Developer Options | 开发者选项 |
| Support bundle | 支持包 |
| Read-only tool | 只读工具 |
| Developer-staged, not redistributable | 开发者暂存，不可再分发 |

Product and command names stay in English: SmileBASIC, CivSwitch, SaveKit,
OunceCake, HBLauncher, Payload Mode, Control Center, Direct mode, and the
console commands RUN, LOAD, LOADV, REMOVE, SAVE, START. Technical
abbreviations (USB, IPv4, RPC, HID, SIP, MIT, GPL) stay as-is.
