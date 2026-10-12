# stacklint

[![Release](https://img.shields.io/github/v/release/YewFence/stacklint?sort=semver)](https://github.com/YewFence/stacklint/releases)
[![Docs](https://img.shields.io/badge/docs-online-blue)](https://YewFence.github.io/stacklint/)
[![License](https://img.shields.io/github/license/YewFence/stacklint)](LICENSE)

**stacklint** is the linter for my personal stacks repository — the repo where I deploy my self-hosted stack with Docker Compose. It is not a general-purpose Compose linter: it encodes my opinions about how a deployment repository should be run. The mechanics are fully config-driven (every policy comes from a `stacklint.toml`), so it can lint any Compose repository — but the rule set reflects my workflow, not a consensus.

> [!NOTE]
> This is a personal tool that I build for myself and share in the open. It changes as my needs change, v0.x makes no backward-compatibility promises, and **I don't accept pull requests**. Bug reports are welcome as issues; if you want it to behave differently, fork it — it's MIT.

## Why it exists

My stacks repo used Python tools that shelled out to `docker compose config` once per interpolation expression — over a hundred subprocess calls and several seconds per stack, plus regex approximations of YAML and dotenv semantics. stacklint renders everything in-process with [compose-go](https://github.com/compose-spec/compose-go), so a ~40-stack repository checks in seconds, and every rule reads Compose's real interpolation, merge, and include semantics instead of approximating them.

## What it does

- `stacklint check` — the house rules: images pinned to sha256 digests, published ports unique per group, host bind addresses through one designated variable, top-level volumes explicitly named, service `env_file` discipline, shared-module wiring completeness. Every exception is a recorded approval in `approvals.toml`, and approvals that stop matching reality fail the check instead of rotting silently.
- `stacklint env generate` / `stacklint env check` — generates each stack's `.env.example` override template from interpolation analysis (compose-go's template evaluator, not regex), and fails CI when the committed template drifts. Supports overlay variants and explicit exemptions for upstream-synced stacks.
- `stacklint images digests` — verifies pinned digests against remote registries (regclient, standard `DOCKER_CONFIG` credentials), warns when a tag has drifted but the digest still exists, and runs incrementally against a git base ref so pull requests only check what changed.
- `stacklint images list` / `stacklint render` — machine-readable JSON views (image inventory, rendered models) for other tooling. `--ref` reads history straight from git objects without touching the working tree.

Two principles run through everything: **fail closed** (when completeness can't be proven, report an error rather than emit something that looks complete) and **no hidden inputs** (the process environment and stray `.env` files never participate; every render input is a declared, version-controlled file).

## Installation

### Mise

```bash
mise use --global github:YewFence/stacklint
```

### Go

```bash
go install github.com/YewFence/stacklint/cmd/stacklint@latest
```

### Build and install from source locally

```bash
git clone https://github.com/YewFence/stacklint.git
cd stacklint
mise trust
mise install
mise run cli:install
```

## Usage

```bash
stacklint --help
stacklint check                              # all stacks, all variants
stacklint env generate                       # regenerate every non-exempt stack's template
stacklint env check --format json            # drift check, structured output
stacklint images digests                     # incremental against origin/main
stacklint images digests --full              # verify every reference
stacklint images list --format json          # image inventory for tooling
stacklint render stacks/example --format json  # omit the target to render all stacks
```

stacklint finds its configuration by walking up from the working directory to the `stacklint.toml` at the repository root (or via `--config-dir` / `STACKLINT_CONFIG_DIR`). Only `images digests` touches the network; everything else works offline. See the [documentation site](https://YewFence.github.io/stacklint/) for the configuration format, and the [requirements document](https://YewFence.github.io/stacklint/design/requirements) for the full rule semantics and safety contracts.

## Compose compatibility

The CLI embeds compose-go v2.16.1 and does not require Docker anywhere: analysis, CI, and deployment hosts all run without a Docker Compose installation. Byte-exact golden tests compare both the normalized and uninterpolated models for covered scenarios (`go test ./internal/loader`). A compose-go upgrade that changes interpolation, merging, normalization, or schema output in those scenarios fails CI and must be reviewed deliberately; the tests do not guarantee detection outside their coverage or compatibility with every Docker Compose CLI version. Generated dotenv assignments are verified by round-tripping through the compose-go parser before being written.

## Status

v0.1.0. Built for and dogfooded on my personal stacks repository (private), which is in the process of migrating its Python tooling over to it. JSON outputs carry a `version` field but no schema stability is promised before 1.0.

## Contributing

Issues are open — a well-described bug report is genuinely useful. Pull requests are not accepted; this tool follows my workflow and I want to keep the latitude to change it on a whim. If it almost fits your needs, fork it: the license is MIT and the policy layer is designed to be replaced.

If you fork it and want to hack on it, the [development guide](CONTRIBUTING.md) covers the toolchain.

## License

[MIT License](LICENSE)
