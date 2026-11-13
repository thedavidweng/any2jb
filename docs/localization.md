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
- Adapter-produced readiness checks, distribution decisions, and availability
  errors carry structured `locales.Text` values (`locales.Key`,
  `locales.Format`, `locales.Join`, `locales.WrapError`, and
  `locales.DistributionReason` in `locales/text.go`). `session.Check` keeps
  deterministic English in `Message` and `NextAction` for JSON snapshots, logs,
  and diagnostic bundles while carrying `MessageText` and `NextActionText` for
  localized GUI and CLI presentation without English substring replacement.
- `TestManifestDistributionsMatchCatalog` verifies that every profile and
  component in `internal/manifest/profiles.json` maps to a catalog entry whose source
  text matches `distribution.reason` and whose `zh-CN` translation is present.
- `TestAdapterMessagesAreLocalized` walks non-test source files to verify that
  every `locales.Key`, `locales.Format`, and `locales.WrapError` literal exists
  and is translated, that `session.Check` literals pair `Message`/`NextAction`
  with `MessageText`/`NextActionText`, and that availability methods return
  structured catalog errors rather than raw English strings.

Apple's UI names follow Apple's own pages in each language (for example
“启动安全性实用工具”, “降低安全性”, “正在加载启动选项”). A catalog may carry a
URL, so a link opens the page for the app's language.

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
| Session tools | 会话工具 |
| Developer options | 开发者选项 |
| Settings | 设置 |
| Check this Mac | 检查这台 Mac |
| Check again | 重新检查 |
| Continue | 继续 |
| Type the commands yourself instead | 改为自己手动输入命令 |
| Needs action | 需要处理 |
| Route and setup | 路线与设置 |
| Launch | 启动 |
| Tools | 工具 |
| Support bundle | 支持包 |
| Read-only tool | 只读工具 |
| Developer-staged, not redistributable | 开发者暂存，不可再分发 |

Product and command names stay in English: SmileBASIC, CivSwitch, SaveKit,
OunceCake, HBLauncher, Payload Mode, Control Center, Direct mode, and the
console commands RUN, LOAD, LOADV, REMOVE, SAVE, START. Technical
abbreviations (USB, IPv4, RPC, HID, SIP, MIT, GPL) stay as-is.
