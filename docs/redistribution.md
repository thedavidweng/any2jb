# Redistribution gate

This is a record of supplied license text and unresolved release requirements,
not legal clearance. Public source access, retained attribution, a digest, or
another distributor's permission is not a redistribution grant to any2jb.

## Canonical inventory

[`manifest/profiles.json`](../manifest/profiles.json) records each component's
origin, exact revision, digest, supplied license/notice, `bundled` flag, decision,
and reason. Profile `sourceSha256` covers the source inventory;
`bootstrapSha256` separately identifies the bootstrap. Component `sha256`
identifies only its recorded file, not a whole repository or compiled binary.

- `may-ship`: the recorded file has a supplied grant, subject to its conditions.
  This is not clearance for its dependencies, a derived binary, or game data.
- `blocked`: permission or distribution obligations remain unresolved.
- `excluded`: deliberately absent from every package.
- `bundled: false`: absent even when the source file has a usable grant.

Current inventory:

- **pepsipu, kurumuz, Cellenseres, BermTheSequel and Astral:** blocked, not
  bundled. Their pinned upstreams supply no root license. Astral inherits
  kurumuz sources; custom changes do not supply the missing grant.
- **IZMYSH host:** blocked, not bundled. Its `LICENSE-SCOPE.md` grants
  GPL-2.0-or-later only for independently authored code and explicitly preserves
  unresolved inherited rights in `THIRD_PARTY.json`. That is not a blanket grant.
- **sender.c, commands.c, keyboard.c, civswitch-hid.c:** MIT original mac2jb
  source, may-ship with copyright and permission notice, but not bundled.
  These are source identities, not authorization for compiled USB helpers,
  libusbd, the driver, or everything in mac2jb's vendor tree.
- **usbgadget driver and installer** (`usbgadget-binary`, `usbgadget-info`,
  `usbgadget-checksums`, `driver-installer`): blocked, not bundled. See
  [driver security](driver-security.md).
- **Browser research probe set:** excluded, not bundled. The recorded digest
  identifies its `LICENSE-NOTICE.md`, which says no open-source license has
  been selected and public availability grants no redistribution/reuse.
  mac2jb's provenance reports permission for mac2jb, not for any2jb.
- **OunceCake and the Super File Bros. SD browser:** blocked, not bundled.
  Their pinned trees are recorded as tool sources (inventory digest, file count
  and provenance/adapter artifacts) so a developer can stage and verify them
  locally. OunceCake supplies GPL v2 text whose exact grant, corresponding
  source, modifications and notices are unreviewed; the SD browser declares
  GPL-2.0-only with unresolved inherited module rights.
- **any2jb tool host:** may-ship, bundled. `runtime/toolhost.cjs` is original
  any2jb code that only drives separately staged tool sources.
- **MyGo placeholder icon and MIT notice:** may-ship, bundled. The complete
  framework copyright and permission notice is in
  [`resources/licenses/mygo.txt`](../resources/licenses/mygo.txt).

Node 24.22.1 darwin-arm64 and the original any2jb structured profile bridge
are bundled. Node's complete release license is retained in
`resources/licenses/node.txt`. Its archive and binary digests are pinned in
`manifest/profiles.json`; the build downloads into an ignored cache and fails
closed on changed bytes. The binary itself is not committed to Git.

No host, bootstrap, USB binary, driver, launcher save, ROM or
research payload is currently bundled. This development shell passing the
resource gate does **not** mean a functional P1a release is distributable.

## Assessment of proposed distribution forms

The following source locations are in
[mac2jb ce1ce5f](https://github.com/cpypwn/mac2jb/tree/ce1ce5f4f40b412cf83ba0a4f10ac53a850ed308)
unless an upstream is named. They are review evidence, not packaged files.
Every unrecorded resource fails the package gate.

### GPL version 2 family

- **OunceCake** (`vendor/OunceCake/LICENSE`,
  `packaging/OunceCake-provenance.json`) and **ns2jb-modloader**
  (`integrations/ns2jb-modloader/LICENSE`) supply GPL v2 text. Do not infer
  an "or later" grant from that text alone. Proposed form: separate pinned
  sidecars, not copied/linked into the Go UI. Distribution remains blocked
  until the precise grant, corresponding source, modifications, build
  materials and notices for the actual package are reviewed. GPL v2 §§1–3
  cover notices, modified works and source/object-code distribution.
  Modloader's zstd, game-files and WASI subtrees need their own notices.
- **IZMYSH / IZlauncher / File Exchange / BlitBun / VacooTube inherited
  integrations:** GPL-2.0-or-later authored code is scoped by `LICENSE-SCOPE.md`
  and `THIRD_PARTY.json`; SDK and other explicit MIT subtrees keep their terms.
  Separate-sidecar packaging does not cure missing inherited permissions.
  VacooTube's separate MIT app is not a grant for all its integrations.
- **NS2-Manager**, pinned at
  [3870a5e](https://github.com/Gamingwithgoose/NS2-Manager/tree/3870a5e278a800daa683b5a277ac94426fbdf348),
  declares GPL-2.0-only in `NOTICE.md`. It is a behavior reference only.
  No .NET source or binary is copied into this application.

### GPL version 3 family

**Balatro RCE** (`integrations/balatro-rce/LICENSE`) and **Gravity Circuit RCE**
(`integrations/gravcirc-rce/LICENSE`) supply GPL v3 text. Proposed form:
separate launcher-save archives and their supporting tools. They remain absent
pending review of each archive's contents, provenance, grant and corresponding
source/build materials. GPL v3 §§4–6 address source and non-source conveyance;
§6 includes Installation Information when its stated conditions apply.
Whether those conditions apply to the selected distribution needs review.
Game-owned save contents/assets are not licensed merely by the exploit code's
GPL file. This is a separate assessment from the GPL v2 sidecars above.

Subprocess separation alone is not proof of compliance for either GPL family.
No combined-work compatibility conclusion is made here.

### Other release requirements

- **Artemis launcher saves/source:** no supplied root license at the spec pin
  [ae02495](https://github.com/alula/switch-artemis-launcher/tree/ae0249547c1e05df46a63a5c05818914b3ae0706).
  mac2jb's separately pinned `packaging/ArtemisLauncher-v2.json` also says
  "not specified upstream". Both remain blocked, not silently interchanged.
- **libusbd and usbgadget:** MIT grants coexist with Apple-derived APSL
  notices (`vendor/libusbd/LICENSE-APSL-2.0.txt`, `drivers/LICENSE` and
  `drivers/LICENSE-APSL-2.0.txt`). Record exact built artifacts, retain all
  notices and review applicable modified-source obligations before bundling.
  Code signing is unrelated to license permission or driver approval.
- **CivSwitch, SaveKit, native HBLauncher and NSP-Box integration:** supplied
  MIT grants do not clear every dependent engine, save, font, ROM or extracted
  content. Review their `NOTICE`, `THIRD_PARTY.md` and subtree notices and add
  exact artifacts to the manifest before packaging. The CivSwitch dependent
  actions add three recorded source identities, none bundled:
  `civswitch-backup-payload` (original mac2jb MIT source, may-ship),
  `civswitch-installer` (blocked: its derived save template, compressor and
  vendored JavaScript dependencies are unreviewed) and
  `civswitch-control-center` (blocked: a separately authored MIT release whose
  complete inventory and notices are unreviewed). No Python interpreter,
  installer, save template or Control Center payload is packaged.
- **GPUBrewLauncher, legacy HBLauncher and MintBrew:** no blanket license
  found for their complete vendored trees. Credits or a licensed HID subfolder
  are insufficient. Keep blocked pending per-component review.
- **Super File Bros / SD browser:** inspect `THIRD_PARTY_NOTICES.md` and the
  inherited held-run/OunceCake rights before enabling the packaged route.
- **NUP:** `packaging/NUP-source.json` is provenance, not a blanket grant.
  Fetched source and local zstandard need their own grants/notices.
- **Stremio:** separately user-installed app; no bundled app/artwork clearance.
- **DOOM engine, shareware data, Freedoom and Diamond Chase:** separate
  licenses. An engine grant does not cover commercial game data or every ROM.
  No ROM or game data is approved by this ticket.
- **Node, Python, Pillow, DejaVu and shader tools:** retain the actual runtime's
  full license inventory, including bundled libraries. Node's full license
  file and mac2jb's `packaging/homebrew-runtime.lock.json` are starting points,
  not an approval of a future build. The Go binary's transitive modules also
  need a release notice inventory. MyGo's MIT notice alone does not cover them.

These are open release requirements, not permission requests already sent.

## Package and UI enforcement

`go tool mygo build` runs `go run ./cmd/prepare-runtime` followed by
`go run ./cmd/check-package` before compilation and
packaging. It checks the entire `resources/` tree, including platform folders:
only explicitly bundled `may-ship` files with matching digests pass. Missing
bundled notices, unknown/renamed files, blocked/excluded files and symlinks fail.
Additional MyGo `resources`, `frontendDist` and custom `icon` routes are refused
until an audited staging step covers them. Do not use `-skip-build-command`
for release builds.

An immutable, already staged **resource** tree can be checked separately:

```sh
go run ./cmd/check-package /absolute/path/to/staged/resources
```

The assembled resource root of a built app is checked after packaging, where
MyGo has merged the platform directories and written its own icon, asset
catalog and language files:

```sh
go run ./cmd/check-package -merged build/darwin-arm64/any2jb.app/Contents/Resources
```

This is not a verifier for a complete `.app`: MyGo generates metadata and the
executable outside the resource inventory. Signing, generated output, embedded
Go dependencies and final release inspection remain packaging gates.
Do not modify staged resources after this check.

Views and tool actions must consult `m.DistributionFor(componentID)`, not
upstream host capability booleans. `Available()` is false for unknown,
unbundled, blocked and excluded components (unknown IDs return an error).
The Advanced Tools view shows the unavailable reason from
`DistributionFor("browser-research")` and gives it no control; it does not
advertise an included probe set or allow execution/import as a way around this
exclusion. Its OunceCake, SD-browser and system-code entries show the same kind
of recorded reason unless their pinned sources were staged locally by a
developer, as described in [retained tools](retained-tools.md).

## Alternate acquisition

The canonical host `upstream` URLs identify where a user can inspect or obtain
sources independently. Artemis and research upstream links above identify
their authors; request explicit permission before redistribution.
An upstream download is not permission to rebundle it. No end-user engine
auto-download or source-import route is implemented. Local developers may
explicitly stage a selected canonical engine profile from a pinned mac2jb
clone after full integrity verification, as described in
[runtime staging](runtime-staging.md#explicit-local-developer-staging).
That isolated stage is marked developer-staged, not redistributable. It does
not change any package decision or unlock excluded tools.

See the [parity ledger's release blockers](parity-ledger.md#redistribution-release-blockers).
