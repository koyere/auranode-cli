# Changelog

The format follows [Keep a Changelog](https://keepachangelog.com/) and
[SemVer](https://semver.org/).

## [1.1.2] — 2026-06-22

### Changed
- The CLI is now fully in English: user-facing output, command help and error
  messages (no functional changes).

## [1.1.1] — 2026-06-20

### Fixed
- **Tunnel half-close deadlock:** when one direction closed, the credits of the
  still-active direction were lost and the connection hung. Fixed (true half-close).

## [1.1.0] — 2026-06-20

### Improved
- **Credit-based flow control on tunnels:** real backpressure when the consumer is
  slower than the producer (previously the stream could reset). Negotiated with the
  remote end; falls back to the previous mode with older versions.

## [1.0.0] — 2026-06-20

First public release of the AuraNode CLI.

### Added
- `auth` (login with email/password + `--totp`, logout, status, token; honors
  `AURANODE_TOKEN`/`AURANODE_API_URL` for CI/CD).
- `servers` (list with `--tag/--status`, show with `--metrics`).
- `status` (global view + live CPU/RAM).
- `exec` (remote execution with polling until terminal state).
- `tunnel` (Type 1 local and Type 2 reverse dest=CLI port-forwarding: list/create/open/expose/rm).
- `config`, `version`. `table`/`json` output (`-o`), multi-environment profiles.
- Binaries for linux/macOS/windows (amd64/arm64) with SHA256 verification.
