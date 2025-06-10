# Layout expected:
#   envs/<ENV>/{config.hcl,variables.tfvars}
: ${TFCTX_ROOT:="envs"}             # parent dir holding env folders
: ${TFCTX_BACKEND:="config.hcl"}    # backend config filename
: ${TFCTX_VARS:="variables.tfvars"} # variables file filename
: ${TFCTX_INIT_OPTS:=""}            # extra flags for every terraform init
: ${TFCTX_VAR_OPTS:=""}             # extra flags for plan/apply/destroy
: ${TFCTX_AUTO_INIT:=1}             # 1 = run terraform init automatically
: ${TFCTX_DEBUG:=0}                 # 1 = enable debug logging

typeset -g TFCTX_ENV=""            # current environment for prompt

# Debug logging helper
_tfctx_debug() {
  (( TFCTX_DEBUG )) && echo "tfctx: $*" >&2
}

# Find the root directory containing .terraform (searching upward)
_tfctx_find_root() {
  local dir="$PWD"
  while [[ "$dir" != "/" ]]; do
    if [[ -d "$dir/.terraform" ]]; then
      echo "$dir"
      return 0
    fi
    dir="${dir:h}"  # Get parent directory
  done
  return 1
}

# Helper function to set context without auto-init
_tfctx_set_context() {
  local env="$1"
  local tf_root="${2:-$PWD}"
  local base="${tf_root}/${TFCTX_ROOT}/${env}"
  local backend="${base}/${TFCTX_BACKEND}"
  local vars="${base}/${TFCTX_VARS}"

  [[ -f "${backend}" ]] || { echo "tfctx: backend config missing: ${backend}" >&2; return 2 }
  [[ -f "${vars}"    ]] || { echo "tfctx: var file missing:     ${vars}" >&2; return 2 }

  export TFCTX_ENV="${env}"
  export TFCTX_ROOT_DIR="${tf_root}"
  export TF_CLI_ARGS_init="-backend-config=${backend} -reconfigure ${TFCTX_INIT_OPTS}"
  export TF_CLI_ARGS_plan="-var-file=${vars} ${TFCTX_VAR_OPTS}"
  export TF_CLI_ARGS_apply="${TF_CLI_ARGS_plan}"
  export TF_CLI_ARGS_destroy="${TF_CLI_ARGS_plan}"
}

_tfctx_clear_context() {
  _tfctx_debug "clearing context"
  unset TFCTX_ENV TFCTX_ROOT_DIR
  unset TF_CLI_ARGS_init TF_CLI_ARGS_plan TF_CLI_ARGS_apply TF_CLI_ARGS_destroy
}

# Detect environment from terraform state file
# Users can override this function for custom detection logic
# Arguments: $1 = path to terraform.tfstate file, $2 = terraform root directory
# Returns: environment name via stdout, or empty if not detected
tfctx_detect_env() {
  local tfstate_file="$1"
  local tf_root="$2"
  
  # Read the backend config from terraform state
  local backend_config
  if command -v jq >/dev/null 2>&1; then
    _tfctx_debug "using jq to parse backend config"
    backend_config=$(jq -r '.backend.config.key // empty' "$tfstate_file" 2>/dev/null)
  else
    # Fallback without jq - extract key from backend config
    _tfctx_debug "using grep to parse backend config"
    backend_config=$(grep -o '"key"[[:space:]]*:[[:space:]]*"[^"]*"' "$tfstate_file" 2>/dev/null | sed 's/.*"\([^"]*\)"/\1/')
  fi

  _tfctx_debug "detected backend config: $backend_config"
  
  # Extract environment from key path by checking config files
  if [[ -n "$backend_config" ]]; then
    _tfctx_debug "searching for matching environment config..."
    
    # Look through all environment config files to find matching key
    for env_dir in ${tf_root}/${TFCTX_ROOT}/*(N/); do
      local env_name="${env_dir:t}"  # Get just the directory name
      local config_file="${env_dir}/${TFCTX_BACKEND}"
      
      if [[ -f "$config_file" ]]; then
        # Extract key from config.hcl - more robust extraction
        local config_key=""
        config_key=$(grep -E '^\s*key\s*=' "$config_file" | sed -n 's/^[^"]*"\([^"]*\)".*/\1/p')
        
        _tfctx_debug "checking $env_name: config key '$config_key' vs backend key '$backend_config'"
        if [[ -n "$config_key" && "$config_key" == "$backend_config" ]]; then
          echo "$env_name"
          _tfctx_debug "found matching environment: $env_name"
          return 0
        fi
      fi
    done
  fi
  
  return 1
}

# Auto-detect environment when changing directories
_tfctx_chpwd() {
  _tfctx_debug "checking Terraform context in $(pwd)"
  # Only auto-detect if we're in a directory with Terraform files
  [[ -n *.tf(N) ]] || return 0
  _tfctx_debug "detected Terraform files, checking for context..."
  local detected_env=""
  local tf_root

  # Find the terraform root directory
  if ! tf_root=$(_tfctx_find_root); then
    _tfctx_debug "no .terraform directory found in current path"
    # Clear context if we're no longer in a Terraform project
    if [[ -n "$TFCTX_ENV" ]]; then
      _tfctx_clear_context
    fi
    return 0
  fi

  _tfctx_debug "found Terraform root at: $tf_root"

  # Check if terraform is initialized and extract backend config
  if [[ -d "$tf_root/.terraform" && -f "$tf_root/.terraform/terraform.tfstate" ]]; then
    detected_env=$(tfctx_detect_env "$tf_root/.terraform/terraform.tfstate" "$tf_root")
  fi
  
  # Switch context if we detected a different environment or root
  if [[ -n "$detected_env" && ( "$detected_env" != "$TFCTX_ENV" || "$tf_root" != "$TFCTX_ROOT_DIR" ) ]]; then
    _tfctx_set_context "$detected_env" "$tf_root"
  elif [[ -z "$detected_env" && -n "$TFCTX_ENV" ]]; then
    # Clear context if we're no longer in an initialized Terraform directory
    _tfctx_debug "no matching environment found, clearing context"
    unset TFCTX_ENV TFCTX_ROOT_DIR TF_CLI_ARGS_init TF_CLI_ARGS_plan TF_CLI_ARGS_apply TF_CLI_ARGS_destroy
  elif [[ -z "$detected_env" && "$tf_root" != "$TFCTX_ROOT_DIR" ]]; then
    # Clear context if we switched TF root but found no matching environment
    _tfctx_debug "no matching environment found, clearing context as we switched TF root"
    unset TFCTX_ENV TFCTX_ROOT_DIR TF_CLI_ARGS_init TF_CLI_ARGS_plan TF_CLI_ARGS_apply TF_CLI_ARGS_destroy
  fi
}

# Register the directory change hook
autoload -U add-zsh-hook
add-zsh-hook chpwd _tfctx_chpwd

# Run detection on plugin load for current directory
_tfctx_chpwd

# List available environments
tfctx_ls() {
  local tf_root="${TFCTX_ROOT_DIR:-$PWD}"
  
  # Try to find terraform root if not set
  if [[ -z "$TFCTX_ROOT_DIR" ]]; then
    if tf_root=$(_tfctx_find_root); then
      _tfctx_debug "using Terraform root: $tf_root"
    else
      _tfctx_debug "using current directory: $tf_root"
    fi
  fi
  
  local display_path="${tf_root}/${TFCTX_ROOT}"
  # Truncate path if longer than 30 characters
  if (( ${#display_path} > 30 )); then
    display_path="...${display_path: -27}"
  fi
  
  echo "Available environments in $display_path:"
  for env_dir in ${tf_root}/${TFCTX_ROOT}/*(N/); do
    local env_name="${env_dir:t}"
    local env_status=""
    if [[ "$env_name" == "$TFCTX_ENV" ]]; then
      env_status=" (current)"
    fi
    echo "  $env_name$env_status"
  done
}

# Create a new environment
tfctx_create() {
  local env="$1"
  [[ -z "$env" ]] && { echo "Usage: tfctx create <environment>" >&2; return 1 }
  
  local tf_root="${TFCTX_ROOT_DIR:-$PWD}"
  
  # Try to find terraform root if not set
  if [[ -z "$TFCTX_ROOT_DIR" ]]; then
    if tf_root=$(_tfctx_find_root); then
      _tfctx_debug "using Terraform root: $tf_root"
    else
      _tfctx_debug "using current directory: $tf_root"
    fi
  fi
  
  local env_dir="${tf_root}/${TFCTX_ROOT}/${env}"
  local backend_file="${env_dir}/${TFCTX_BACKEND}"
  local vars_file="${env_dir}/${TFCTX_VARS}"
  
  # Check if environment already exists
  if [[ -d "$env_dir" ]]; then
    echo "tfctx: environment '$env' already exists" >&2
    return 1
  fi
  
  # Create environment directory
  mkdir -p "$env_dir" || { echo "tfctx: failed to create directory $env_dir" >&2; return 1 }
  
  # Create backend config template
  cat > "$backend_file" << EOF
# Backend configuration for $env environment
bucket = "your-terraform-state-bucket"
key    = "environments/$env/terraform.tfstate"
region = "us-east-1"
EOF
  
  # Create variables template
  cat > "$vars_file" << EOF
# Variables for $env environment
environment = "$env"

# Add your environment-specific variables here
EOF
  
  echo "Created environment '$env' in $env_dir"
  echo "Please edit the following files to match your expectation:"
  echo "  - $backend_file (backend configuration)"
  echo "  - $vars_file (environment variables)"
}

_tfctx() {
  local cmd="$1"
  
  case "$cmd" in
    "ls"|"list")
      tfctx_ls
      ;;
    "create")
      shift
      tfctx_create "$@"
      ;;
    "")
      echo "Usage: tfctx <environment|create|ls>" >&2
      echo "Commands:" >&2
      echo "  tfctx <env>       Switch to environment" >&2
      echo "  tfctx create <env> Create new environment" >&2
      echo "  tfctx ls          List environments" >&2
      return 1
      ;;
    *)
      local env="$cmd"
      local tf_root="${TFCTX_ROOT_DIR:-$PWD}"
      
      # Try to find terraform root if not set
      if [[ -z "$TFCTX_ROOT_DIR" ]]; then
        if tf_root=$(_tfctx_find_root); then
          _tfctx_debug "using Terraform root: $tf_root"
        else
          _tfctx_debug "using current directory: $tf_root"
        fi
      fi
      
      # Set the context
      _tfctx_set_context "$env" "$tf_root"

      # Only run terraform init if explicitly called by user and auto-init is enabled
      if (( TFCTX_AUTO_INIT )); then
        local _log
        # Change to terraform root directory for init
        (
          cd "$tf_root"
          if ! _log=$(terraform init 2>&1); then
            echo "tfctx: terraform init failed — see log below" >&2
            printf '%s\n' "${_log}" >&2
            exit 3
          fi
        ) || return 3
      fi
      ;;
  esac
}

alias tfctx='_tfctx'

tfctx_prompt_info() {
  [[ -n "${TFCTX_ENV}" ]] && printf "%%F{cyan}[%%F{white}%s%%F{cyan}]%%f" "${TFCTX_ENV}"
}
