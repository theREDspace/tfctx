package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestMain lets the end-to-end tests run this test binary as the tfctx CLI.
func TestMain(m *testing.M) {
	if os.Getenv("TFCTX_TEST_AS_CLI") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// newProject creates a Terraform root with dev and prod environments. If
// initEnv is set, the root looks initialized against that environment's
// backend.
func newProject(t *testing.T, initEnv string) string {
	t.Helper()
	// Resolve symlinks (macOS temp dirs live under one) so paths compare
	// equal to what the shells report.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "main.tf"), "")
	writeFile(t, filepath.Join(root, "envs", "dev", "config.hcl"), `
bucket = "b"
key    = "environments/dev/terraform.tfstate"
`)
	// Comments and blocks are fine: the file is parsed as HCL.
	writeFile(t, filepath.Join(root, "envs", "prod", "config.hcl"), `
# Production
bucket = "b"
key="environments/prod/terraform.tfstate" # trailing comment
assume_role {
  role_arn = "arn:aws:iam::123456789012:role/terraform"
}
`)
	for _, env := range []string{"dev", "prod"} {
		writeFile(t, filepath.Join(root, "envs", env, "variables.tfvars"), "")
	}
	if initEnv != "" {
		writeFile(t, filepath.Join(root, ".terraform", "terraform.tfstate"), fmt.Sprintf(
			`{"version":3,"backend":{"type":"s3","config":{"bucket":"b","key":"environments/%s/terraform.tfstate"}}}`, initEnv))
	}
	return root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// clearEnv blanks every variable tfctx reads, so the developer's own shell
// context can't leak into a test.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, name := range append([]string{"TFCTX_ROOT", "TFCTX_BACKEND", "TFCTX_VARS", "TFCTX_CONFIG_KEY",
		"TFCTX_INIT_OPTS", "TFCTX_VAR_OPTS", "TFCTX_AUTO_INIT", "TFCTX_DEBUG"}, contextVars...) {
		t.Setenv(name, "")
	}
}

func exitCode(err error) int {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return -1
}

const unsetLine = "unset TFCTX_ENV TFCTX_ROOT_DIR TF_CLI_ARGS_init TF_CLI_ARGS_plan TF_CLI_ARGS_apply TF_CLI_ARGS_destroy\n"

func TestDetectEnv(t *testing.T) {
	clearEnv(t)
	c := loadConfig()
	for _, env := range []string{"dev", "prod"} {
		if got := c.detectEnv(newProject(t, env)); got != env {
			t.Errorf("initialized for %s: detectEnv = %q", env, got)
		}
	}
	if got := c.detectEnv(newProject(t, "staging")); got != "" {
		t.Errorf("unmatched backend key: detectEnv = %q, want none", got)
	}
}

func TestHook(t *testing.T) {
	clearEnv(t)
	root := newProject(t, "prod")
	outside := t.TempDir()
	tests := []struct {
		name             string
		dir              string
		curEnv, curRoot  string
		detect           func(root string) string // nil: built-in detection
		wantExport       string                   // environment the output should switch to
		wantUnset, quiet bool
	}{
		{name: "detects from a subfolder", dir: filepath.Join(root, "envs"), wantExport: "prod"},
		{name: "already current", dir: root, curEnv: "prod", curRoot: root, quiet: true},
		{name: "corrects a stale env", dir: root, curEnv: "dev", curRoot: root, wantExport: "prod"},
		{name: "clears on leaving the project", dir: outside, curEnv: "prod", curRoot: root, wantUnset: true},
		{name: "nothing to clear", dir: outside, quiet: true},
		{name: "custom detection wins", dir: root, detect: func(string) string { return "dev" }, wantExport: "dev"},
		{name: "custom detection finds nothing", dir: root, curEnv: "prod", curRoot: root, detect: func(string) string { return "" }, wantUnset: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(tt.dir)
			t.Setenv("TFCTX_ENV", tt.curEnv)
			t.Setenv("TFCTX_ROOT_DIR", tt.curRoot)
			c := loadConfig()
			detect := tt.detect
			if detect == nil {
				detect = c.detectEnv
			}
			var out bytes.Buffer
			if err := c.hook(&out, detect); err != nil {
				t.Fatal(err)
			}
			got := out.String()
			switch {
			case tt.quiet && got != "":
				t.Errorf("want no output, got:\n%s", got)
			case tt.wantUnset && got != unsetLine:
				t.Errorf("want unset, got:\n%s", got)
			case tt.wantExport != "":
				for _, line := range []string{
					"export TFCTX_ENV=" + quote(tt.wantExport) + "\n",
					"export TFCTX_ROOT_DIR=" + quote(root) + "\n",
				} {
					if !strings.Contains(got, line) {
						t.Errorf("output missing %q, got:\n%s", line, got)
					}
				}
			}
		})
	}
}

// fakeTerraform puts a terraform on PATH that records how it was called and
// exits with the given status.
func fakeTerraform(t *testing.T, status int) (logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake terraform is a shell script")
	}
	bin := t.TempDir()
	logPath = filepath.Join(bin, "log")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" \"$TF_CLI_ARGS_init\" > %s\nexit %d\n", quote(logPath), status)
	if err := os.WriteFile(filepath.Join(bin, "terraform"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func TestUse(t *testing.T) {
	clearEnv(t)
	root := newProject(t, "")
	t.Chdir(root)

	t.Run("unknown environment", func(t *testing.T) {
		var out bytes.Buffer
		err := loadConfig().use(&out, "nope")
		if exitCode(err) != 2 || out.Len() != 0 {
			t.Errorf("got code %d, output %q; want code 2 and no output", exitCode(err), out.String())
		}
	})

	t.Run("init runs with the new context", func(t *testing.T) {
		log := fakeTerraform(t, 0)
		var out bytes.Buffer
		if err := loadConfig().use(&out, "dev"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "export TFCTX_ENV='dev'\n") {
			t.Errorf("output doesn't switch to dev:\n%s", out.String())
		}
		calls, _ := os.ReadFile(log)
		want := fmt.Sprintf("-chdir=%s init\n-backend-config=%s -reconfigure\n",
			root, quote(filepath.Join(root, "envs", "dev", "config.hcl")))
		if string(calls) != want {
			t.Errorf("terraform called with:\n%s\nwant:\n%s", calls, want)
		}
	})

	t.Run("failed init clears the context", func(t *testing.T) {
		fakeTerraform(t, 1)
		var out bytes.Buffer
		err := loadConfig().use(&out, "dev")
		if exitCode(err) != 3 || out.String() != unsetLine {
			t.Errorf("got code %d, output %q; want code 3 and an unset", exitCode(err), out.String())
		}
	})
}

func TestCreate(t *testing.T) {
	clearEnv(t)
	t.Chdir(newProject(t, ""))
	c := loadConfig()
	if err := c.create(&bytes.Buffer{}, "staging"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.contextFor(rootFor("."), "staging"); err != nil {
		t.Errorf("created environment isn't usable: %v", err)
	}
	for _, env := range []string{"staging", "ls", "-x", ""} {
		if err := c.create(&bytes.Buffer{}, env); err == nil {
			t.Errorf("create(%q) succeeded, want an error", env)
		}
	}
}

func TestQuote(t *testing.T) {
	for _, s := range []string{"plain", "with space", "it's", "$HOME `x`", "back\\slash", "new\nline", ""} {
		out, err := exec.Command("sh", "-c", "printf %s "+quote(s)).Output()
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != s {
			t.Errorf("sh read quote(%q) as %q", s, out)
		}
	}
}
