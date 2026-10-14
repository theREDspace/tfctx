# tfctx shell integration for bash (3.2 and later), printed by
# `tfctx init bash`. Load it from ~/.bashrc with: eval "$(tfctx init bash)"

# The binary can't set this shell's variables, so switching environments
# prints shell code for this wrapper to eval.
tfctx() {
  case "${1-}" in
    ''|-*|@COMMANDS@) command tfctx "$@" ;;
    *)
      local out rc
      out="$(command tfctx use bash "$@")"
      rc=$?
      eval "$out"
      return $rc
      ;;
  esac
}

# Bash has no directory-change hook, so this runs from PROMPT_COMMAND and only
# calls tfctx when $PWD has changed since the last prompt. It keeps $? intact
# for whatever else is in PROMPT_COMMAND.
#
# If you define tfctx_detect_env, it replaces the built-in detection. It is
# called with the path to .terraform/terraform.tfstate and the Terraform root,
# and prints the environment name, or nothing if there isn't one.
_tfctx_hook() {
  local rc=$?
  if [[ "$PWD" != "${_TFCTX_PWD-}" ]]; then
    _TFCTX_PWD=$PWD
    if declare -F tfctx_detect_env >/dev/null; then
      local root env=
      if root=$(command tfctx root 2>/dev/null) && [[ -f $root/.terraform/terraform.tfstate ]]; then
        env=$(tfctx_detect_env "$root/.terraform/terraform.tfstate" "$root")
      fi
      eval "$(command tfctx hook bash --env "$env")"
    else
      eval "$(command tfctx hook bash)"
    fi
  fi
  return $rc
}

tfctx_prompt_info() {
  [[ -n "${TFCTX_ENV-}" ]] && printf '[%s]' "$TFCTX_ENV"
}

# Runs terraform plan and copies it to the clipboard (with pbcopy) as a
# collapsed block to paste into a GitHub PR comment.
tfctx_pr_plan() {
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

_tfctx_complete() {
  COMPREPLY=()
  (( COMP_CWORD == 1 )) || return 0
  local cur=${COMP_WORDS[COMP_CWORD]}
  COMPREPLY=($(compgen -W "$(command tfctx ls --quiet 2>/dev/null) ls create" -- "$cur"))
}
complete -F _tfctx_complete tfctx

# Run first in PROMPT_COMMAND so the prompt sees the new context. Bash 5.1+
# lets PROMPT_COMMAND be an array; keep whichever form is in use.
_tfctx_register() {
  local IFS=';'
  case ";${PROMPT_COMMAND[*]-};" in *";_tfctx_hook;"*) return ;; esac
  if [[ "$(declare -p PROMPT_COMMAND 2>/dev/null)" == "declare -a"* ]]; then
    PROMPT_COMMAND=(_tfctx_hook "${PROMPT_COMMAND[@]}")
  else
    PROMPT_COMMAND="_tfctx_hook${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
  fi
}
_tfctx_register
unset -f _tfctx_register
