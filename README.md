tfctx
=====

Jump between Terraform back-end configurations with a single command, and
show the active environment in your prompt. Works in zsh and bash, with or
without oh-my-zsh.

```text
envs/
├── dev/
│   ├── config.hcl
│   └── variables.tfvars
└── prod/
    ├── config.hcl
    └── variables.tfvars
```

# Features

- `tfctx <env>` switches the `-backend-config` and `-var-file` arguments Terraform uses, by setting `TF_CLI_ARGS_*`.
- Optional automatic `terraform init` (enabled by default).
- Detects the environment Terraform was last initialized with whenever you `cd` into a project.
- Tab completion of environments.
- `$TFCTX_ENV` holds the active environment for your prompt, and `tfctx_prompt_info` prints it ready-formatted.

# Installation

`tfctx` is a single binary plus a few lines of shell setup. It needs Terraform 0.14 or later.

Download the archive for your platform from the [releases page](https://github.com/theREDspace/tfctx/releases) and put the `tfctx` binary on your `PATH`. Or, with Go 1.25 or later:

```
go install github.com/theREDspace/tfctx@latest
```

## zsh

Add to `~/.zshrc`:

```
eval "$(tfctx init zsh)"
```

### oh-my-zsh

```
git clone https://github.com/theREDspace/tfctx.git $ZSH/custom/plugins/tfctx
```

and add `plugins+=(tfctx)`. The plugin runs the same `eval` for you, so the binary still needs to be on your `PATH`.

## bash

Add to `~/.bashrc`:

```
eval "$(tfctx init bash)"
```

Bash has no hook for directory changes, so tfctx checks for one each time the prompt is drawn. A command on the same line as a `cd`, such as `cd other-project && terraform plan`, still runs with the previous project's context.

# Prompt

`$TFCTX_ENV` is the active environment, or unset when there is none. Any prompt can show it, including powerlevel10k and starship.

`tfctx_prompt_info` prints it in brackets. In zsh it is colored for use in `PROMPT`, which needs `setopt prompt_subst` (oh-my-zsh sets this for you):

```
PROMPT='$(tfctx_prompt_info) '$PROMPT
```

In bash:

```
PS1='$(tfctx_prompt_info) '$PS1
```

# Configuration

Settings are read by the `tfctx` binary, so they must be exported.

```
export TFCTX_ROOT="terraform/envs"         # default: envs, relative to the detected Terraform root
export TFCTX_BACKEND="config.hcl"          # backend config filename
export TFCTX_CONFIG_KEY="key"              # backend setting that identifies an environment (e.g. path for the local backend)
export TFCTX_VARS="variables.tfvars"       # variables file filename
export TFCTX_INIT_OPTS=""                  # extra flags for every init
export TFCTX_VAR_OPTS=""                   # extra flags for plan/apply/destroy
export TFCTX_AUTO_INIT=0                   # disable auto-init
export TFCTX_DEBUG=1                       # enables debug logging
```

# Environment detection

When you `cd` into a project, tfctx reads the backend settings Terraform saved in `.terraform/terraform.tfstate` at its last init. It then looks for the environment whose backend config has the same `TFCTX_CONFIG_KEY` value. The default is `key`, the state path used by the S3 backend.

## Custom environment detection

To use your own detection instead, define a `tfctx_detect_env` shell function. Define it before the `eval "$(tfctx init ...)"` line (or before oh-my-zsh loads its plugins) so it also applies to the first check when the shell starts.

It is called only when the Terraform root has a `.terraform/terraform.tfstate` file, with two arguments:

1. the path to that `terraform.tfstate` file
2. the Terraform root directory

It should print the environment name, or nothing if no environment matches.

```
tfctx_detect_env() { echo "dev"; }
```

`tfctx root` prints the Terraform root for the current directory, which can help when writing one.

# Releasing

Push a version tag and the release workflow publishes binaries for macOS, Linux and Windows with [GoReleaser](https://goreleaser.com):

```
git tag v1.0.0 && git push origin v1.0.0
```

To try a release build locally: `goreleaser release --snapshot --clean` (output goes to `dist/`).
