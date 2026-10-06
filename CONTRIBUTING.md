# Development Guide

## Requirements

[mise](https://github.com/jdx/mise) is the only direct requirement, which is used for managing the other development tools.

The development tools required by this project are declared in [mise.toml](mise.toml) and [mise.lock](mise.lock). Run `mise install` to install them.

## Principles

run `hk install` to configure pre-commit hook to make sure `mise run check` passes before finishing any changes

Keep the reusable command and toolchains definitions in mise, to make sure the CI CD and local development environments are consistent

When `mise run check` failed, you can try to run `mise run fix` to fix the issues automatically. If the check still fails, you may need to fix the issues manually. The CI will also run the same check.

## Common Commands

Run `mise tasks` to see the full task list.

### CLI

```bash
# Run the CLI
mise run cli
# Install the CLI into your local Go bin directory
mise run cli:install
# Run common check
mise run check
# Try to automatically fix issues found by `check`
mise run fix
# Build a local executable. Build artifacts are written to the `bin/` directory.
mise run build
```

### Documentation Site

```bash
# Start the local documentation development server
mise run docs:dev
```

### GitHub Actions Maintenance

Run mise tasks to update GitHub Actions workflows and actions.

```bash
mise run actions:update
```

Read [mise.toml](mise.toml) for the task definitions.
