package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestShellIntegration loads `tfctx init` into real zsh and bash and checks
// that cd, switching and leaving a project update the shell's variables.
func TestShellIntegration(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(self, filepath.Join(bin, "tfctx")); err != nil {
		t.Fatal(err)
	}
	root := newProject(t, "prod")

	const prelude = `
eval "$(tfctx init $SHELL_NAME)"
show() { echo "$1 env=${TFCTX_ENV-} root=${TFCTX_ROOT_DIR-}"; }
`
	tests := []struct{ name, script, want string }{
		{
			name: "switching",
			script: `
cd "$PROJECT/envs"; prompt; show cd-in
tfctx dev; echo "use=$?"; show use
tfctx nope 2>/dev/null; echo "bad=$?"; show bad
cd /; prompt; show cd-out
`,
			want: `cd-in env=prod root=ROOT
use=0
use env=dev root=ROOT
bad=2
bad env=dev root=ROOT
cd-out env= root=
`,
		},
		{
			name: "tfctx_detect_env override",
			script: `
tfctx_detect_env() { echo "called with $1 $2" >&2; echo dev; }
cd "$PROJECT/envs"; prompt; show cd-in
cd /; prompt; show cd-out
`,
			want: `called with ROOT/.terraform/terraform.tfstate ROOT
cd-in env=dev root=ROOT
cd-out env= root=
`,
		},
	}

	// prompt stands in for the shell drawing a prompt: zsh's hook fires on
	// cd, bash's runs from PROMPT_COMMAND.
	for _, sh := range []struct{ name, prompt string }{
		{"zsh", "prompt() { :; }"},
		{"bash", `prompt() { eval "$PROMPT_COMMAND"; }`},
	} {
		path, err := exec.LookPath(sh.name)
		for _, tt := range tests {
			t.Run(sh.name+"/"+tt.name, func(t *testing.T) {
				if err != nil {
					t.Skipf("%s not installed", sh.name)
				}
				args := []string{"-f", "-c"}
				if sh.name == "bash" {
					args = []string{"--norc", "--noprofile", "-c"}
				}
				cmd := exec.Command(path, append(args, sh.prompt+prelude+tt.script)...)
				cmd.Env = append(cleanEnviron(),
					"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
					"TFCTX_TEST_AS_CLI=1",
					"TFCTX_AUTO_INIT=0",
					"SHELL_NAME="+sh.name,
					"PROJECT="+root,
				)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%v\n%s", err, out)
				}
				if want := strings.ReplaceAll(tt.want, "ROOT", root); string(out) != want {
					t.Errorf("got:\n%s\nwant:\n%s", out, want)
				}
			})
		}
	}
}

// cleanEnviron is the test's environment without any tfctx or Terraform
// context the developer's shell may have set.
func cleanEnviron() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TFCTX_") && !strings.HasPrefix(kv, "TF_CLI_ARGS") && !strings.HasPrefix(kv, "BASH_ENV=") {
			env = append(env, kv)
		}
	}
	return env
}
