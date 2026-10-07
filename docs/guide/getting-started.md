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

[render]
# Static interpolation inputs, flattened with no precedence tiers
env_files = [".env.example", ".env.ci"]

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
