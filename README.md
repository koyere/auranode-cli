<div align="center">

# AuraNode CLI

`auranode` — la línea de comandos de [AuraNode](https://auranode.app) para gestionar
tus VPS, ejecutar comandos, abrir túneles y más desde la terminal.

[![release](https://img.shields.io/github/v/release/koyere/auranode-cli)](https://github.com/koyere/auranode-cli/releases)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

</div>

## Instalación

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/koyere/auranode-cli/main/install.sh | bash
```

Descarga el binario del último release, **verifica su SHA256** e instala `auranode`
en `/usr/local/bin`.

### go install

```bash
go install github.com/koyere/auranode-cli@latest
```

### Manual / Windows

Descarga el archivo de tu plataforma desde
[Releases](https://github.com/koyere/auranode-cli/releases)
(`.tar.gz` en Linux/macOS, `.zip` en Windows), verifica el checksum y coloca el
binario `auranode` en tu `PATH`.

## Uso

```bash
auranode auth login                      # inicia sesión (email/contraseña + 2FA)
auranode servers list                    # lista tus VPS
auranode status --all                    # estado global
auranode exec --tag production "uptime"  # ejecuta en varios VPS
auranode tunnel open <ref>               # port-forwarding local
auranode --help                          # todos los comandos
```

La configuración (perfiles) vive en `~/.auranode/config.yaml` (permisos `0600`).
Para CI/CD usa las variables `AURANODE_TOKEN` y `AURANODE_API_URL`.

## Desarrollo

```bash
go build -o auranode .
go test ./...
```

Los releases se publican con [GoReleaser](https://goreleaser.com) al empujar un tag
`v*` (ver [`.github/workflows/release.yml`](.github/workflows/release.yml)).

## Licencia

[MIT](LICENSE) © Koyere Dev
