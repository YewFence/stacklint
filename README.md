# stacklint

[![Release](https://img.shields.io/github/v/release/YewFence/stacklint?sort=semver)](https://github.com/YewFence/stacklint/releases)
[![Docs](https://img.shields.io/badge/docs-online-blue)](https://YewFence.github.io/stacklint/)
[![License](https://img.shields.io/github/license/YewFence/stacklint)](LICENSE)

Static analysis, environment template generation, and image checks for Docker Compose stack repositories

> [!NOTE]
> This project is in an early development stage. Core features may be missing, and backward compatibility is not guaranteed.

## Quick Start

### Installation

#### Mise

```bash
mise use --global github:YewFence/stacklint
```

#### Go

```bash
go install github.com/YewFence/stacklint/cmd/stacklint@latest
```

#### Build and install from source locally

```bash
git clone https://github.com/YewFence/stacklint.git
cd stacklint
mise trust
mise install
mise run cli:install
```

### Usage

```bash
stacklint --help
stacklint version
stacklint render stacks/example --format json
stacklint check
stacklint env generate stacks/example
stacklint env check stacks/example --variant local --format json
stacklint images list --format json
stacklint images digests --base origin/main
```

All commands are implemented. Only `images digests` accesses the network; everything else works offline. The CLI embeds compose-go v2.16.1 and does not require Docker for analysis; no behavioral divergence from recent Docker Compose plugin versions is known. Because the embedded compose-go may differ from the Compose plugin on a deployment host, consumer repositories compare `stacklint render --format json` against `docker compose config --format json` in CI, and generated dotenv assignments are verified by round-tripping through the compose-go parser before being written.

Repository policy comes from a root `stacklint.toml`. Environment templates support recursive includes and explicitly registered overlay variants; no adjacent overlay files are loaded implicitly. Generation requires explicit targets, while `env check` without targets checks all discovered stacks and registered variants. See the [requirements document](https://YewFence.github.io/stacklint/design/requirements) for configuration, supported input shapes, exemptions, and safety contracts.

Include modules with an implicit `.env` produce a warning and retain compose-go's native loading behavior. Environment templates and diagnostics do not expose those values; `render` outputs a configuration view and is not a redaction mechanism.

## Documentation

See the [documentation site](https://YewFence.github.io/stacklint/) for setup, usage, and configuration guidance.

## Contributing

If you have suggestions or find a bug, please [open an issue](https://github.com/YewFence/stacklint/issues).

Pull requests are welcome. See the [Contributing Guide](CONTRIBUTING.md).

## License

[MIT License](LICENSE)
