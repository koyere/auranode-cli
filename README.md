<div align="center">

# AuraNode CLI

`auranode` — the command line for [AuraNode](https://auranode.app) to manage your
VPS, run commands, open tunnels and more from the terminal.

[![release](https://img.shields.io/github/v/release/koyere/auranode-cli)](https://github.com/koyere/auranode-cli/releases)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

</div>

## Installation

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/koyere/auranode-cli/main/install.sh | bash
```

Downloads the binary from the latest release, **verifies its SHA256** and installs
`auranode` in `/usr/local/bin`.

### go install

```bash
go install github.com/koyere/auranode-cli@latest
```

### Manual / Windows

Download the archive for your platform from
[Releases](https://github.com/koyere/auranode-cli/releases)
(`.tar.gz` on Linux/macOS, `.zip` on Windows), verify the checksum and place the
`auranode` binary on your `PATH`.

## Usage

```bash
auranode auth login                      # sign in (email/password + 2FA)
auranode servers list                    # list your VPS
auranode status --all                    # global status
auranode exec --tag production "uptime"  # run on multiple VPS
auranode tunnel open <ref>               # local port-forwarding
auranode --help                          # all commands
```

Configuration (profiles) lives in `~/.auranode/config.yaml` (permissions `0600`).
For CI/CD use the `AURANODE_TOKEN` and `AURANODE_API_URL` variables.

## Development

```bash
go build -o auranode .
go test ./...
```

Releases are published with [GoReleaser](https://goreleaser.com) when a `v*` tag is
pushed (see [`.github/workflows/release.yml`](.github/workflows/release.yml)).

## License

[MIT](LICENSE) © Koyere Dev
