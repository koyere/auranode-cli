# Changelog

El formato sigue [Keep a Changelog](https://keepachangelog.com/) y
[SemVer](https://semver.org/lang/es/).

## [1.0.0] — 2026-06-20

Primer release público del CLI de AuraNode.

### Añadido
- `auth` (login email/contraseña + `--totp`, logout, status, token; honra
  `AURANODE_TOKEN`/`AURANODE_API_URL` para CI/CD).
- `servers` (list con `--tag/--status`, show con `--metrics`).
- `status` (vista global + CPU/RAM en vivo).
- `exec` (ejecución remota con polling hasta estado terminal).
- `tunnel` (port-forwarding Tipo 1 local y Tipo 2 reverse dest=CLI: list/create/open/expose/rm).
- `config`, `version`. Salida `table`/`json` (`-o`), perfiles multi-entorno.
- Binarios para linux/macOS/windows (amd64/arm64) con verificación SHA256.
