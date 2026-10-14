package main

import (
	_ "embed"
	"fmt"
	"io"
	"strings"
)

//go:embed shell/tfctx.zsh
var zshInit string

//go:embed shell/tfctx.bash
var bashInit string

// initScripts are the shell integrations "tfctx init" prints, by shell. The
// hook and use commands print POSIX export and unset lines, which both shells
// eval as-is.
var initScripts = map[string]string{
	"zsh":  zshInit,
	"bash": bashInit,
}

func isShell(name string) bool {
	_, ok := initScripts[name]
	return ok
}

// writeInit writes the integration script for sh, with the wrapper's list of
// pass-through commands filled in from commands.
func writeInit(w io.Writer, sh string) error {
	script, ok := initScripts[sh]
	if !ok {
		return errorf(1, "unsupported shell %q (supported: zsh, bash)", sh)
	}
	_, err := io.WriteString(w, strings.ReplaceAll(script, "@COMMANDS@", strings.Join(commands, "|")))
	return err
}

func writeExports(w io.Writer, vars []envVar) {
	for _, v := range vars {
		fmt.Fprintf(w, "export %s=%s\n", v.name, quote(v.value))
	}
}

func writeUnset(w io.Writer) {
	fmt.Fprintf(w, "unset %s\n", strings.Join(contextVars, " "))
}

// quote single-quotes s for POSIX shells. Terraform splits TF_CLI_ARGS_* with
// the same rules, so it is used for paths inside those values too.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
