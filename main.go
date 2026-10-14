// Command tfctx switches the Terraform backend config and variable file used
// by the current shell, one environment folder at a time.
//
// A program can't change its parent shell's variables, so the subcommands the
// shell integration calls (hook, use) print shell code for it to eval. See
// "tfctx init".
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
)

// version is set by goreleaser with -ldflags "-X main.version=...".
var version = "dev"

// commands are the subcommands the shell wrapper passes straight through to
// the binary. Any other first argument is an environment name to switch to.
var commands = []string{"ls", "list", "create", "root", "init", "hook", "use", "help", "version"}

// exitError is an error with the exit status tfctx should return for it.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func errorf(code int, format string, args ...any) error {
	return &exitError{code, fmt.Sprintf(format, args...)}
}

func main() {
	err := run(os.Args[1:], os.Stdout)
	if err == nil {
		return
	}
	code := 1
	var ee *exitError
	if errors.As(err, &ee) {
		code = ee.code
	}
	if msg := err.Error(); msg != "" {
		fmt.Fprintln(os.Stderr, "tfctx:", msg)
	}
	os.Exit(code)
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		usage(os.Stderr)
		return errorf(1, "")
	}
	c := loadConfig()
	cmd, args := args[0], args[1:]
	switch cmd {
	case "init":
		if len(args) != 1 {
			return errorf(1, "usage: tfctx init <zsh|bash>")
		}
		return writeInit(stdout, args[0])
	case "hook":
		// --env passes in the result of the user's tfctx_detect_env shell
		// function, which replaces the built-in detection.
		detect := c.detectEnv
		switch {
		case len(args) == 3 && isShell(args[0]) && args[1] == "--env":
			env := args[2]
			detect = func(string) string { return env }
		case len(args) != 1 || !isShell(args[0]):
			return errorf(1, "usage: tfctx hook <zsh|bash> [--env <environment>]")
		}
		return c.hook(stdout, detect)
	case "root":
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		root, ok := findRoot(cwd)
		if !ok {
			return errorf(1, "no .terraform directory at or above %s", cwd)
		}
		fmt.Fprintln(stdout, root)
		return nil
	case "use":
		if len(args) != 2 || !isShell(args[0]) {
			return errorf(1, "usage: tfctx use <zsh|bash> <environment> (called by the shell integration; run tfctx <environment>)")
		}
		return c.use(stdout, args[1])
	case "ls", "list":
		quiet := len(args) == 1 && (args[0] == "-q" || args[0] == "--quiet")
		return c.list(stdout, quiet)
	case "create":
		if len(args) != 1 {
			return errorf(1, "usage: tfctx create <environment>")
		}
		return c.create(stdout, args[0])
	case "version":
		fmt.Fprintln(stdout, buildVersion())
		return nil
	case "help", "-h", "--help":
		usage(stdout)
		return nil
	}
	return errorf(1, "unknown command %q. To switch environments, load the shell integration first: eval \"$(tfctx init zsh)\"", cmd)
}

// buildVersion is the goreleaser version, or for `go install` builds the
// module version Go records in the binary.
func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

func usage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  tfctx <environment>     Switch to an environment
  tfctx ls [-q]           List environments (-q: names only)
  tfctx create <env>      Create an environment from templates
  tfctx root              Print the initialized Terraform root (nearest .terraform)
  tfctx init <zsh|bash>   Print the shell integration; load it with
                          eval "$(tfctx init zsh)" in your shell's rc file
  tfctx version           Print the version
`)
}
