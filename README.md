tfctx
=====

Simple oh-my-zsh plugin that lets you jump between Terraform back-end
configurations with a single command and shows the active environment
in your prompt.

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

- tfctx <env>: switches both the -backend-config and -var-file arguments for you.
- optional automatic `terraform init` (enabled by default)
- tab-completion of available environments
- $(tfctx_prompt_info) helper so your prompt will show the active tfenv.

# Installation

## Manual

```
git clone https://github.com/theREDspace/tfctx.git $ZSH/custom/plugins/tfctx
```

Add

```
plugins+=(tfctx)
```

# Configuration

```
export TFCTX_ROOT="$HOME/terraform/envs"   # default: envs
export TFCTX_BACKEND="config.hcl"          # backend config filename
export TFCTX_VARS="variables.tfvars"       # variables file filename
export TFCTX_INIT_OPTS=""                  # extra flags for every init
export TFCTX_VAR_OPTS=""                   # extra flags for plan/apply
export TFCTX_AUTO_INIT=0                   # disable auto-init
```