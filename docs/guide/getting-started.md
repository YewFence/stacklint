# Getting Started

## Installation

With [mise](https://github.com/jdx/mise):

```bash
mise use --global github:YewFence/stacklint
```

With Go:

```bash
go install github.com/YewFence/stacklint/cmd/stacklint@latest
```

Prebuilt archives are attached to each [GitHub release](https://github.com/YewFence/stacklint/releases).

## Configuration

stacklint reads all repository policy from a `stacklint.toml` at the repository root. The CLI locates it by walking up from the working directory; `--config-dir` or `STACKLINT_CONFIG_DIR` override the search. A minimal configuration:

```toml
[repository]
# Directories under which every directory holding a Compose file is a stack
deployment_roots = ["stacks"]
compose_filenames = ["compose.yaml", "compose.yml"]

[env]
# Generated override template filename inside each stack
output = ".env.example"

[images]
floating_tags = ["latest", "main"]
```

Explicit approvals and exemptions live in an optional `approvals.toml` next to it. Unknown keys, duplicates, and missing targets are all hard errors — stacklint fails closed rather than guessing. The full schema and rule semantics are specified in the [requirements document](/design/requirements).

## Usage

```bash
stacklint check                    # lint every stack and registered variant
stacklint check stacks/edge/app    # lint one stack (directory, file, or unique fragment)
stacklint env check                # verify all templates are in sync
stacklint env generate stacks/edge/app
stacklint images digests           # verify references added since origin/main
stacklint images digests --full    # verify every reference
stacklint render stacks/edge/app --format json
```

Exit codes: `0` everything passed (warnings allowed), `1` check failures, `2` usage or configuration errors. Only `images digests` accesses the network.

## What goes in a Compose file

Variables in `compose.yaml` are documented and constrained by `@env-*` comment markers — `@env` (description), `@env-preamble` (group lead-in), `@env-source` (external provider), and `@env-example` (example value for checks). These markers are the only place stacklint learns variable intent; they live next to the expression they describe. See [Env Markers](/guide/env-markers) for the full concept guide.
