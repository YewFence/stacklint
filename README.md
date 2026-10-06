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
stacklint check
stacklint env generate stacks/example
stacklint env check
stacklint images digests --full
stacklint render stacks/example
```

Most commands are not implemented yet. See the [requirements document](https://YewFence.github.io/stacklint/design/requirements) for planned features.

## Documentation

See the [documentation site](https://YewFence.github.io/stacklint/) for setup, usage, and configuration guidance.

## Contributing

If you have suggestions or find a bug, please [open an issue](https://github.com/YewFence/stacklint/issues).

Pull requests are welcome. See the [Contributing Guide](CONTRIBUTING.md).

## License

[MIT License](LICENSE)
