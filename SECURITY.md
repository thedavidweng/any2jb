# Security Policy

## Reporting a Vulnerability

Please report security vulnerabilities privately through [GitHub Private Vulnerability Reporting](https://github.com/thedavidweng/any2jb/security/advisories/new) rather than opening a public issue.

Include affected versions or commit SHAs, reproduction steps, and impact (for example, SHA-256 verification bypass, path traversal in save/export staging, privilege escalation in USB gadget helpers, or unintended network exposure beyond loopback).

## Security Boundaries

- Bundled runtime components and all 29 on-demand plugins are verified against SHA-256 digests pinned in `internal/manifest/profiles.json` before staging or execution.
- `any2jb` never modifies macOS SIP, Recovery, or startup security settings automatically; see [`docs/driver-security.md`](docs/driver-security.md).
- Console mutations (save writes, modloader transfers, arbitrary JavaScript execution) require explicit per-action confirmation and verified backups where applicable.
