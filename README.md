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

- tfctx <env>: switches and controls both the -backend-config and -var-file arguments for you.
- optional automatic `terraform init` (enabled by default)
- tab-completion of available environments
- $(tfctx_prompt_info) helper to show in your terminal prompt will show the active tfenv.

# Custom enviornment detection

You can override the detection functionality to your own custom function using the below as an example.

Two arguments are passed to this function, the Terraform root directory path that was detected, and the path to the `terraform.tfstate` file.

```
tfctx_detect_env() { echo "dev"; }
```

But by default it uses jq, and compares the current `tfstate` key to your environment configurations to find a matching key used by your s3 remote.

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
export TFCTX_ROOT="terraform/envs"         # default: envs, defined relative to the detected terraform root.
export TFCTX_BACKEND="config.hcl"          # backend config filename
export TFCTX_CONFIG_KEY="bucket"           # if you use a different bucket
export TFCTX_VARS="variables.tfvars"       # variables file filename
export TFCTX_INIT_OPTS=""                  # extra flags for every init
export TFCTX_VAR_OPTS=""                   # extra flags for plan/apply
export TFCTX_AUTO_INIT=0                   # disable auto-init
export TFCTX_DEBUG=1                       # enables debug logging
```
