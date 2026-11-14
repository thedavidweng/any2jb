# Command-line parity

Every interactive GUI control has a command-line equivalent, or a stated
reason it has none. `TestCLIParity` fails when a control in the GUI source has
no row here, when a row names a control that no longer exists, or when a row
names a command or flag that the `any2jb` command tree does not have.

Commands marked *session* run inside `any2jb session` (interactive, piped or
`--script`). The others also run as one-shot commands. See [the CLI guide](cli.md).

| UI key | CLI |
| --- | --- |
| about | `settings about` |
| about-github | `about` |
| about-open | `about` |
| apple-recovery | n/a: a link to Apple's documentation |
| apple-sip | n/a: a link to Apple's documentation |
| apple-startup-security | n/a: a link to Apple's documentation |
| apply-preferences | `settings set` |
| artemis-backup | `artemis backup` |
| artemis-catalog | `artemis catalog` |
| artemis-consent | `artemis eval --consent` |
| artemis-discover | `artemis discover` |
| artemis-eval | `artemis eval` |
| artemis-fs-host | `artemis fs --host` |
| artemis-fs-start | `artemis fs` |
| artemis-fs-stop | n/a: sending SIGINT (Ctrl-C) stops `artemis fs` |
| artemis-get | `artemis get` |
| artemis-health | `artemis health` |
| artemis-host | `artemis status --console` |
| artemis-local-path | `artemis get --out` |
| artemis-lua | `artemis eval --code` |
| artemis-port | `artemis status --port` |
| artemis-probe | `artemis probe` |
| artemis-put | `artemis put` |
| artemis-remote-path | `artemis stat --path` |
| artemis-rm | `artemis rm` |
| artemis-stat | `artemis stat` |
| artemis-status | `artemis status` |
| artemis-subnet | `artemis discover --subnet` |
| bootstrap | `prepare` |
| bootstrap-overwrite | `save --consent` |
| bootstrap-reuse | `use-saved` |
| bootstrap-reuse-confirm | `use-saved --confirm` |
| bundle-open | n/a: `diagnostics export` prints the bundle path |
| cancel | `cancel` |
| capability-demo | `settings set --edition` |
| capability-modloader | `settings profiles` |
| capability-native | `settings profiles` |
| capability-rpc | `settings profiles` |
| capability-savekit | `settings profiles` |
| civ-address | `civswitch launch --console` |
| civ-attach | `civswitch connect` |
| civ-back | `civswitch leave` |
| civ-backup-open | n/a: `status --json` prints `CivSwitchActions.BackupPath` |
| civ-cancel | `cancel` |
| civ-cartridge | `civswitch launch --confirm` |
| civ-chat | `civswitch launch --confirm` |
| civ-consent-code | `civswitch send-js --consent` |
| civ-consent-saves | `civswitch install-trigger --consent` |
| civ-finish | `finish` |
| civ-fs-host | `civswitch fs start --host` |
| civ-fs-start | `civswitch fs start` |
| civ-fs-stop | `civswitch fs stop` |
| civ-fs-wait | `civswitch fs start --wait` |
| civ-hb-consent | `civswitch homebrew install-trigger --consent` |
| civ-hb-install-trigger | `civswitch homebrew install-trigger` |
| civ-hb-launch | `civswitch homebrew launch` |
| civ-hb-pkg-file | `civswitch homebrew install-package --file` |
| civ-hb-pkg-install | `civswitch homebrew install-package` |
| civ-hb-pkg-select | `civswitch homebrew install-package --package` |
| civ-hb-probe | `civswitch homebrew probe` |
| civ-hb-restore-trigger | `civswitch homebrew restore-trigger --consent` |
| civ-hb-shutdown | `civswitch homebrew shutdown` |
| civ-hb-stop | `civswitch homebrew stop` |
| civ-js | `civswitch send-js` |
| civ-js-file | `civswitch send-js` |
| civ-language | `civswitch launch --confirm` |
| civ-open | `civswitch availability` |
| civ-recheck | `civswitch recheck` |
| civ-start | `civswitch launch` |
| civ-stop | `stop` |
| civ-trigger | `civswitch install-trigger` |
| cleanup-cancel | n/a: `diagnostics cleanup` without `--consent` only previews |
| cleanup-confirm | `diagnostics cleanup --consent` |
| cleanup-preview | `diagnostics cleanup` |
| confirm-save | `confirm-save --confirm` |
| console-restart | `restart-confirm --confirm` |
| developer-options | `settings developer` |
| diagnostics-open | `diagnostics` |
| dnswitch-address | `dnswitch start --address` |
| dnswitch-dns-port | `dnswitch start --dns-port` |
| dnswitch-hostname | `dnswitch start --hostname` |
| dnswitch-http-port | `dnswitch start --http-port` |
| dnswitch-start | `dnswitch start` |
| dnswitch-status | `dnswitch status` |
| dnswitch-stop | `dnswitch stop` |
| dnswitch-url | `dnswitch start --url` |
| driver-approve | `driver install` |
| driver-back | n/a: answering N at the prompt declines |
| driver-confirm | `driver install --consent` |
| driver-decline | n/a: answering N at the prompt declines |
| driver-install | `driver install` |
| driver-manual | `settings set --input` |
| dump-open | n/a: `tool state` prints the export path |
| finish | `finish` |
| flow-back-detection | n/a: commands are not staged pages; the controller gates their order |
| flow-back-prepare | n/a: commands are not staged pages; the controller gates their order |
| flow-back-selection | n/a: commands are not staged pages; the controller gates their order |
| flow-civ-launch | `civswitch launch` |
| flow-manual | `settings set --input` |
| flow-next-launch | `launch` |
| flow-next-prepare | n/a: commands are not staged pages; the controller gates their order |
| flow-next-selection | `detect` |
| hb-app-folder | `tool run --app-folder` |
| hb-custom | `tool run` |
| hb-custom-consent | `tool run --consent` |
| hb-custom-folder | `tool run --custom-folder` |
| hb-install | `tool install-homebrew` |
| hb-mintbrew | `tool run` |
| hb-native | `tool run` |
| hb-native-consent | `tool run --consent` |
| hb-sd-folder | `tool run --sd-folder` |
| hb-stremio-addons | `tool run --stremio-addon` |
| hb-stremio-quality | `tool run --stremio-quality` |
| host-profile | `settings set --profile` |
| inspect | `detect` |
| lan-override | `settings set --lan` |
| language-select | `settings set --language` |
| launcher-account | `launcher install --account` |
| launcher-confirm | `launcher install --consent` |
| launcher-fullgame-export | `launcher install --backup-folder` |
| launcher-install | `launcher install` |
| launcher-package | `launcher list` |
| launcher-physical | `launcher install --physical-cartridge` |
| launcher-preview | `launcher preview` |
| launcher-version | `launcher install --version` |
| logs-acquisition | `diagnostics logs --source` |
| logs-all | `diagnostics logs --source` |
| logs-export | `diagnostics export` |
| logs-follow | n/a: run `diagnostics logs` again to read new output |
| logs-host | `diagnostics logs --source` |
| logs-open | `diagnostics logs --json` |
| logs-pipeline | `diagnostics logs --source` |
| logs-source | `diagnostics logs --source` |
| logs-usb | `diagnostics logs --source` |
| manual-acknowledge | `ack --confirm` |
| manual-copy | n/a: the console text is printed to the terminal |
| manual-text | `wait` |
| mod-consent | `tool run --consent` |
| mod-folder | `tool run --mods-folder` |
| mod-install | `tool run` |
| mod-source | `tool run --mods-folder` |
| nsp-artwork | `nsp-box build --artwork` |
| nsp-base-game | `nsp-box build --base-game` |
| nsp-build | `nsp-box build` |
| nsp-dlc | `nsp-box build --dlc` |
| nsp-output | `nsp-box build --out` |
| nsp-source | `nsp-box build --source` |
| nsp-title | `nsp-box build --title` |
| nsp-title-id | `nsp-box build --title-id` |
| nsp-version | `nsp-box build --version` |
| nup-action | `nup` |
| nup-input | `nup info --in` |
| nup-output | `nup pack --out` |
| nup-run | `nup` |
| nxformat-action | `nxformat` |
| nxformat-input | `nxformat info --in` |
| nxformat-output | `nxformat pack --out` |
| nxformat-run | `nxformat` |
| ouncecake-ftp | `tool run --console` |
| ouncecake-game | `tool state` |
| ouncecake-select | `tool select` |
| ouncecake-webdav | `tool run --console` |
| plugin-category | `plugins list --category` |
| plugin-install | `plugins install` |
| plugin-install-all | `plugins install --all` |
| plugin-install-category | `plugins install --category` |
| plugin-select | `plugins list` |
| plugin-uninstall | `plugins uninstall` |
| plugin-upstream | `plugins list --json` |
| plugins-open | `plugins list` |
| prep-cable | n/a: a console-side fact this app cannot observe; the command line leaves it to the operator |
| prep-direct | n/a: a console-side fact this app cannot observe; the command line leaves it to the operator |
| prep-guide | n/a: a link to the 90dns setup guide |
| prep-keyboard | n/a: a console-side fact this app cannot observe; the command line leaves it to the operator |
| prep-network | `civswitch launch --confirm` |
| prep-smilebasic | n/a: a console-side fact this app cannot observe; the command line leaves it to the operator |
| prep-updates | n/a: a console-side fact this app cannot observe; the command line leaves it to the operator |
| prep-version | n/a: a console-side fact this app cannot observe; the command line leaves it to the operator |
| probes-address | `browser-probes serve --address` |
| probes-port | `browser-probes serve --port` |
| probes-select | `browser-probes serve --probe` |
| probes-source | `browser-probes serve --source` |
| probes-start | `browser-probes serve` |
| probes-stop | n/a: sending SIGINT (Ctrl-C) stops `browser-probes serve` |
| recovery-steps | n/a: read-only Recovery instructions the user follows; no command changes security settings |
| route | `launch` |
| rpc-attach | `connect` |
| rpc-choose | `discover` |
| rpc-detach | `disconnect` |
| rpc-find | `discover` |
| rpc-status | `tool run` |
| save | `save --consent` |
| savekit-account | `savekit inspect --account` |
| savekit-accounts | `savekit accounts` |
| savekit-all | `savekit backup --all` |
| savekit-allow-replace | `savekit write --replace` |
| savekit-archive | `savekit inspect --archive` |
| savekit-back | n/a: SaveKit commands are not a separate page |
| savekit-backup | `savekit backup` |
| savekit-compare | `savekit verify` |
| savekit-compare-source | `savekit verify --expected` |
| savekit-confirm-delete | `savekit delete --consent` |
| savekit-confirm-restore | `savekit restore --consent` |
| savekit-confirm-write | `savekit write --consent` |
| savekit-delete | `savekit delete` |
| savekit-delete-name | `savekit delete --file` |
| savekit-filetime | `savekit filetime` |
| savekit-inspect | `savekit inspect` |
| savekit-inventory | `savekit inventory` |
| savekit-open | `savekit` |
| savekit-open-backup | n/a: `tool state` prints the backup path |
| savekit-open-export | n/a: `tool state` prints the export path |
| savekit-preview-restore | `savekit preview --source` |
| savekit-preview-write | `savekit preview --source` |
| savekit-restore | `savekit restore` |
| savekit-restore-source | `savekit restore --source` |
| savekit-title-id | `savekit inspect --title` |
| savekit-titles | `savekit titles` |
| savekit-write | `savekit write` |
| savekit-write-source | `savekit write --source` |
| selection-attach | `discover` |
| selection-details | `settings profiles` |
| selection-expert | `settings set --profile` |
| selection-inspect | `detect` |
| settings-driver | `driver status` |
| settings-keyboard | `keyboard` |
| settings-open | `settings get` |
| smile-route | `launch` |
| smilebasic-edition | `settings set --edition` |
| start | `launch` |
| stop | `stop` |
| system-dump | `tool run` |
| tool-console | `tool run --console` |
| tool-stop | `tool stop` |
| transport-mode | `settings set --input` |
| usb-apply | `keyboard check` |
| usb-capture | `keyboard report` |
| usb-close-panel | `keyboard close` |
| usb-confirm-console | `ack --confirm` |
| usb-controller | `settings set --usb` |
| usb-enter | `settings set --usb` |
| usb-enumeration | `settings set --usb` |
| usb-gap | `settings set --usb` |
| usb-hold | `settings set --usb` |
| usb-keyboard | `keyboard` |
| usb-load-wait | `settings set --usb` |
| usb-remove-wait | `settings set --usb` |
| usb-run-wait | `settings set --usb` |
| usb-start-forwarding | `keyboard open` |
| usb-stock-defaults | `usb-defaults` |
| usb-stop-forwarding | `keyboard close` |
| util-tab | n/a: selects the active sub-tool tab in the GUI Utilities view |
| utilities-open | n/a: navigates to the GUI Utilities view |
| verified-details | `status --json` |
