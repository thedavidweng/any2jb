# Changelog

## 0.1.0

- Native MyGo development app for Apple Silicon macOS 14+.
- Bundle pinned Node 24.22.1 and the integrity-checked any2jb engine bridge.
- Keep engines, bootstraps, USB helpers and drivers out of distribution until
  their release gates are cleared.
- Retained RPC lease with read-only tools: live status, connecting to an
  existing verified session, the system-code capture and the OunceCake and
  `/sd` exports, with one owner at a time and no console write path.
- Record OunceCake and the SD browser as pinned tool sources that only a
  developer may stage locally; packaged entries state their recorded reason.
- Gate the CivSwitch boot trigger, JavaScript send, Payload Mode staging and
  Control Center behind a rechecked loader, explicit per-action confirmation and
  a disk-verified save backup, with read-only exports retained outside sessions.
- Application updates remain disabled until a production public key and
  reviewed release channel are configured.
