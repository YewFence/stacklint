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

Explicit approvals and exemptions live in the same file under typed tables such as `[[approvals.image]]`, `[[approvals.published_port]]`, and `[[approvals.env_exemption]]`. The `approvals` namespace is optional. Move entries from the former standalone `approvals.toml` into these tables and remove the old file; a leftover file is a configuration error. Unknown keys, duplicates, and missing targets are all hard errors — stacklint fails closed rather than guessing. The full schema and rule semantics are specified in the [requirements document](/design/requirements).

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

## Alternative stacks

When two stacks provide alternative implementations that must never run together in the same deployment environment, declare their relationship explicitly:

```toml
[[deployment.alternatives]]
name = "proxy"
stacks = ["stacks/edge/proxy", "stacks/nas/proxy"]
```

Each group permits at most one member, including the option to deploy none. This removes port and resource ownership conflicts between the alternatives while preserving checks against other stacks and within each stack. Every member still receives image, environment, and wiring checks. The declaration does not select or deploy a stack and does not alter the existing root groups.

Names must be nonempty and unique, each group needs at least two distinct discovered stack directories, and a stack can belong to only one group. Remove port approvals that become unnecessary after declaring alternatives; stale approval checks will report them.
