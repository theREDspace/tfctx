# tfctx shell integration for zsh, printed by `tfctx init zsh`.
# Load it from ~/.zshrc with: eval "$(tfctx init zsh)"

# The binary can't set this shell's variables, so switching environments
# prints shell code for this wrapper to eval.
tfctx() {
  emulate -L zsh
  case "${1-}" in
    ''|-*|@COMMANDS@) command tfctx "$@" ;;
    *)
      local out rc
      out="$(command tfctx use zsh "$@")"
      rc=$?
      eval "$out"
      return $rc
      ;;
  esac
}

# If you define tfctx_detect_env, it replaces the built-in detection. It is
# called with the path to .terraform/terraform.tfstate and the Terraform root,
# and prints the environment name, or nothing if there isn't one.
_tfctx_hook() {
  emulate -L zsh
  if (( $+functions[tfctx_detect_env] )); then
    local root env
    if root=$(command tfctx root 2>/dev/null) && [[ -f $root/.terraform/terraform.tfstate ]]; then
      env=$(tfctx_detect_env "$root/.terraform/terraform.tfstate" "$root")
    fi
    eval "$(command tfctx hook zsh --env "$env")"
  else
    eval "$(command tfctx hook zsh)"
  fi
}

tfctx_prompt_info() {
  [[ -n "${TFCTX_ENV-}" ]] && printf "%%F{cyan}[%%F{white}%s%%F{cyan}]%%f" "${TFCTX_ENV}"
}

# Runs terraform plan and copies it to the clipboard (with pbcopy) as a
# collapsed block to paste into a GitHub PR comment.
tfctx_pr_plan() {
  emulate -L zsh
  local plan rc
  plan=$(mktemp "${TMPDIR:-/tmp}/tf.plan.XXXXXX") || return
  terraform plan -out "$plan" &&
    printf '<details>\n<summary>terraform plan (%s)</summary>\n```\n%s\n```\n</details>\n' \
      "${TFCTX_ENV-}" "$(terraform show -no-color "$plan")" | pbcopy &&
    printf '\n✅ Copied GitHub Markdown for terraform plan for %s to clipboard\n' "${TFCTX_ENV-}"
  rc=$?
  rm -f "$plan"
  return $rc
}

_tfctx() {
  (( CURRENT == 2 )) || return 1
  local -a envs cmds
  local ret=1
  envs=(${(f)"$(command tfctx ls --quiet 2>/dev/null)"})
  envs=(${envs//:/\\:})
  cmds=('ls:List environments' 'create:Create a new environment')
  _describe -t environments 'environment' envs && ret=0
  _describe -t commands 'command' cmds && ret=0
  return ret
}

autoload -Uz add-zsh-hook
add-zsh-hook chpwd _tfctx_hook
_tfctx_hook

# compdef only exists once compinit has run. If it hasn't yet, register on the
# first prompt instead.
if (( $+functions[compdef] )); then
  compdef _tfctx tfctx
else
  _tfctx_compdef() {
    add-zsh-hook -d precmd _tfctx_compdef
    (( $+functions[compdef] )) && compdef _tfctx tfctx
  }
  add-zsh-hook precmd _tfctx_compdef
fi
