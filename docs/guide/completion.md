# Shell Completion

stacklint uses Cobra's native completion support and does not require an additional generator.

## Generate Completion Scripts

After building the binary, use the `completion` subcommand to generate completion scripts for Bash, Zsh, Fish, and PowerShell.

```bash
mise run build
./bin/stacklint completion zsh > _stacklint
./bin/stacklint completion bash > stacklint.bash
./bin/stacklint completion fish > stacklint.fish
./bin/stacklint completion powershell > stacklint.ps1
```

## Installation Examples

For Zsh, place the generated `_stacklint` file in an existing directory from `$fpath`, or place it in a custom directory and add that directory in `~/.zshrc`.

```bash
mkdir -p ~/.zsh/completions
./bin/stacklint completion zsh > ~/.zsh/completions/_stacklint
```

```zsh
fpath=(~/.zsh/completions $fpath)
autoload -Uz compinit
compinit
```

For Bash, place the completion script in a local directory and source it manually, or let the system bash-completion directory manage it.

```bash
mkdir -p ~/.bash_completion.d
./bin/stacklint completion bash > ~/.bash_completion.d/stacklint.bash
source ~/.bash_completion.d/stacklint.bash
```

For Fish, write the script directly to the user completion directory.

```bash
mkdir -p ~/.config/fish/completions
./bin/stacklint completion fish > ~/.config/fish/completions/stacklint.fish
```
