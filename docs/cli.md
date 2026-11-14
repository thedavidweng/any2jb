# Command line

The app binary is also its command line. With no arguments it opens the GUI.
With arguments it runs a [Cobra](https://github.com/spf13/cobra) command and
exits. `any2jb --self-test` works as before.

The command line is another presentation of the same code as the GUI. It calls
the same `SessionController`, retained-session manager, readiness checks,
diagnostics store, preferences and saved-bootstrap record. It holds no session
state of its own and adds no safety or cleanup rules. Every GUI control has a
command, or a stated reason it has none, in [command-line parity](cli-parity.md).

## Reaching the binary

| Platform | Binary | Notes |
| --- | --- | --- |
| macOS | `/Applications/any2jb.app/Contents/MacOS/any2jb` | A symlink works: `ln -s /Applications/any2jb.app/Contents/MacOS/any2jb ~/.local/bin/any2jb`. Resources still resolve from the bundle; `--self-test` and `settings get` pass through a symlink. |
| Windows | `any2jb.exe` | MyGo links a GUI-subsystem program. Commands attach to the console of the shell that started them. Cross-compiled only; not run on Windows yet. |
| Linux | `any2jb` | The binary inherits the terminal. Cross-compiled only; there is no Linux package yet. |

Linux and Windows are not release targets in Phase 1, so these rows describe
the build, not a supported install.

**Why one binary.** A second executable would need its own packaging, signing
and resource lookup, and could drift from the GUI. MyGo needs nothing from the
process before `App.Run`, so `main` routes arguments to Cobra before the GUI
starts. A macOS Finder launch that passes a `-psn_` argument still opens the GUI.

## Sessions and ownership

Commands that only read preferences or the manifest (`settings get`,
`settings profiles`, `settings about`, `settings developer`, `settings updates`,
`language`, `usb-defaults`, `launcher list`, `savekit preview`) run at any time.

Every other command composes the production session, so it takes the session
lock, `app-owner.lock` in the app support folder, first. It is separate from
the host's per-run `session.lock` lease, whose presence means an interrupted run. The GUI takes the same lock at startup.
While one holds it, the other refuses with exit code 3:

```text
$ (sleep 3 | any2jb session) & sleep 1; any2jb detect
Another any2jb process owns this session. Close it before continuing.
exit=3
```

The OS releases the lock when the owner exits, including a crash.

Read-only checks such as `status`, `detect`, `tool list`, `discover` and the
`diagnostics` commands also run as one-shot commands. Anything that starts or
drives a live operation runs inside `any2jb session`, because the host, input
and retained lease belong to the process that started them. A session reads
commands from a terminal, from a pipe or from `--script FILE`, one per line.
Blank lines and `#` comments are skipped. A line may also be a JSON array of
arguments for paths that are awkward to quote.

Without a terminal, a session stops at the first blocked or failed step and
exits with that step's code. At end of input, an operation still running is
cancelled or stopped as Ctrl-C would. A verified session is left running, as
when the GUI window closes; run `finish` to end it.

Developer staging behaves as in the GUI: `ANY2JB_DEVELOPER_CONFIG` is read only
by a development build, and the command line prints the same
"developer-staged, not redistributable" label.

## Consent

Each risky action prints its full risk text first. On a terminal it then asks
`Continue? [y/N]`, and only `y` continues. Without a terminal, including any
`--script` run, the action needs its own token; a token from another action is
refused. There is no global `--yes`, and a token never skips the terminal prompt.

| Action | Token |
| --- | --- |
| `save` (replace a saved program) | `--consent=replace-bootstrap` |
| `confirm-save` | `--confirm=console-saved` |
| `use-saved` | `--confirm=matching-bootstrap` |
| `ack` (a guided or USB console step) | `--confirm=console-step` |
| `restart-confirm` | `--confirm=console-restarted` |
| `tool run` for mods, custom homebrew or a native app folder | `--consent=run-user-code` |
| `savekit write`, `restore`, `delete` | `--consent=change-save` |
| `launcher install` | `--consent=install-launcher-save` |
| `diagnostics cleanup` | `--consent=delete-temporary-files` |
| `civswitch launch` (prerequisites) | `--confirm=civswitch-prerequisites` |
| `civswitch install-trigger` | `--consent=replace-civswitch-saves` |
| `civswitch send-js`, `stage-js` | `--consent=run-javascript` |
| `driver install` | `--consent=install-driver` |

SaveKit and launcher writes still go through the same preview digest, target
validation and verified backup as the GUI; the token only records consent.

```text
$ printf 'use-saved\n' | any2jb session
Confirm the console already contains NS2 for kurumuz (full), revision 81d5bb114c856109dadaf1077d927a9f297fe58d, bootstrap SHA-256 dca34ac7977cfd3f7cb657a79da925450472b6527ea83d022a9aa25510b168d5. The app cannot read saved program contents. If unsure, prepare and save the matching bootstrap instead.
Explicit per-action consent is required: matching-bootstrap
exit=2
```

## Cancel, Stop and Ctrl-C

Ctrl-C during an operation that has not reached the console is Cancel, and the
session returns to ready. After anything reached the console it is Stop: the
result is indeterminate and `restart-confirm` is required. Without a running
operation, Ctrl-C only ends the command line. Nothing is retried automatically.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Error, including a failed session |
| 2 | Cancelled by the user, consent declined or a consent token missing |
| 3 | Blocked: readiness needs attention, the controller refused the command, the session is owned elsewhere, or a live command ran outside `session` |
| 4 | Indeterminate: the console must be restarted and `restart-confirm` run |

`status` reports and exits 0 in any stage; `detect` and the session commands
use the codes above.

## Output and language

Output is text by default. With `--json`, every one-shot command writes exactly
one JSON object to stdout, including blocked commands, argument errors and
recovery-required results. Object results keep their existing fields. Lists,
strings and other scalar results use `{"result":...}`; multiple results use
`{"results":[...]}`. Help uses `{"text":...}`. A successful command without a
result writes `{}`. A `session --json` streams one object per command rather
than wrapping the whole session. Progress, risk text and prompts go to stderr.

Failures add `code`, `exit` and a localized `message` to the result object.
The stable codes are `error` (1), `cancelled` (2), `blocked` (3) and
`recovery_required` (4). The process exit code is unchanged. A command that
reports state before being blocked, such as `detect`, includes both in one
object. Recovery-required failures also include:

```json
{"code":"recovery_required","exit":4,"message":"Console recovery required","recovery":{"required":true,"stage":"indeterminate_console_restart_required"},"next":"any2jb session, then restart-confirm --confirm=console-restarted"}
```

This is a schema example, not a hardware observation. The recovery gate still
requires a console restart and explicit confirmation in a session.

Text comes from the same en/zh-CN catalog as the GUI. The language is `--lang`
when given, otherwise the first of `MYGO_LOCALE`, `LC_ALL`, `LC_MESSAGES` and
`LANG` that names a supported language, otherwise English. The GUI's system
language comes from MyGo's main thread, which a command-line process never
starts, so these variables stand in for it. `TestCLICommandsLocalized` requires
every command's description in both catalogs.

## Command tree

```text
any2jb                      GUI with no arguments
├── status                  stage, readiness checks, startup-security note
├── detect                  readiness for the selected input method, incl. SIP
├── session [--script FILE] own an interactive or scripted session
│   ├── prepare · save · confirm-save · use-saved
│   ├── launch · wait · ack · cancel · stop · finish · restart-confirm
│   ├── connect ID · disconnect
│   ├── tool run TOOL · tool stop · tool select TITLE
│   ├── savekit accounts|titles|inventory|inspect|backup|filetime|verify|write|restore|delete
│   ├── launcher preview|install PACKAGE
│   ├── civswitch launch|connect IPV4|recheck|install-trigger|send-js FILE|stage-js FILE|control-center|stop-control-center|leave
│   ├── keyboard check|open|report HEX|close
│   └── driver install
├── settings get|set|profiles|developer|about|updates
├── language · usb-defaults
├── tool list|state · discover
├── savekit preview ACTION · launcher list
├── civswitch availability · driver status
└── diagnostics logs|export|folders|cleanup
```

`settings set` takes `--profile`, `--edition`, `--input`
(`manual`, `keyboard`, `composite`), `--lan` and `--usb` (a
JSON object with `controller` and `timing`). It validates and applies them
through the same code as the GUI's Apply.

## Examples

These excerpts came from the packaged `build/darwin-arm64/any2jb.app` on
2026-11-11. Its recovery report was caused by the former app-owner lock sharing
the host's `session.lock` filename, not evidence of a real interrupted console
run. The locks now use separate filenames. Recovery behavior is covered by the
simulated environment in `internal/cli/cli_test.go`.

```text
$ any2jb status
Console recovery required
Host profile: kurumuz
How commands reach the console: manual
Inspect and clear any partial console input. Restart the console before another attempt. Do not blindly retry.
After restarting the console, run: any2jb session, then restart-confirm --confirm=console-restarted
exit=0

$ cat steps.txt
# Show state, then try to launch.
status
launch
settings get
$ any2jb session --script steps.txt
Console recovery required
...
Console recovery required
Inspect and clear any partial console input. Restart the console before another attempt. Do not blindly retry.
After restarting the console, run: any2jb session, then restart-confirm --confirm=console-restarted
exit=4

$ any2jb tool list | head -3
rpc-status	available
system-code-dump	unavailable: Inherited ns2jb redistribution permission is unresolved. No host or bootstrap is bundled.
ouncecake-webdav	unavailable: Unavailable: OunceCake's GPL v2-family obligations (exact grant, corresponding source, modifications and notices) are unresolved for any2jb. A developer may stage these pinned sources locally; no package includes them.

$ any2jb --lang zh-CN status
游戏机需要恢复
主机配置: kurumuz
命令如何送到游戏机: manual
请检查并清除游戏机上任何未完成的输入。再次尝试前，请重启游戏机。切勿盲目重试。
重启游戏机后运行：any2jb session，然后运行 restart-confirm --confirm=console-restarted
```

The script stopped at `launch`, so `settings get` never ran. The `...` marks
lines trimmed from the `status` output above.

A complete guided setup and launch is covered against the simulated session
environment, not a console, in `internal/cli/cli_test.go`. For example, this script reaches
a finished session there:

```text
detect
use-saved --confirm=matching-bootstrap
launch
finish
```

No console, USB device or driver was used for any of this.
