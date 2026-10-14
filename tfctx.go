package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// config holds the TFCTX_* settings, read from the environment.
type config struct {
	envsDir   string // TFCTX_ROOT: folder with one subfolder per environment, relative to the Terraform root
	backend   string // TFCTX_BACKEND: backend config file name in an environment folder
	vars      string // TFCTX_VARS: variable file name in an environment folder
	configKey string // TFCTX_CONFIG_KEY: backend setting that identifies an environment
	initOpts  string // TFCTX_INIT_OPTS: extra flags for terraform init
	varOpts   string // TFCTX_VAR_OPTS: extra flags for plan, apply and destroy
	autoInit  bool   // TFCTX_AUTO_INIT: run terraform init when switching
	debug     bool   // TFCTX_DEBUG: log decisions to stderr
}

func loadConfig() config {
	return config{
		envsDir:   getenv("TFCTX_ROOT", "envs"),
		backend:   getenv("TFCTX_BACKEND", "config.hcl"),
		vars:      getenv("TFCTX_VARS", "variables.tfvars"),
		configKey: getenv("TFCTX_CONFIG_KEY", "key"),
		initOpts:  os.Getenv("TFCTX_INIT_OPTS"),
		varOpts:   os.Getenv("TFCTX_VAR_OPTS"),
		autoInit:  getenv("TFCTX_AUTO_INIT", "1") != "0",
		debug:     getenv("TFCTX_DEBUG", "0") != "0",
	}
}

// getenv returns the value of key, or def when it is unset or empty.
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (c config) debugf(format string, args ...any) {
	if c.debug {
		fmt.Fprintf(os.Stderr, "tfctx: "+format+"\n", args...)
	}
}

// contextVars are the variables tfctx exports for an environment, and unsets
// when it clears one.
var contextVars = []string{"TFCTX_ENV", "TFCTX_ROOT_DIR", "TF_CLI_ARGS_init", "TF_CLI_ARGS_plan", "TF_CLI_ARGS_apply", "TF_CLI_ARGS_destroy"}

// envVar is one variable assignment for the calling shell.
type envVar struct{ name, value string }

// findRoot returns the nearest directory at or above dir that contains a
// .terraform directory.
func findRoot(dir string) (string, bool) {
	for {
		if info, err := os.Stat(filepath.Join(dir, ".terraform")); err == nil && info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// rootFor returns the Terraform root for dir, or dir itself for a project
// that hasn't been initialized yet.
func rootFor(dir string) string {
	if root, ok := findRoot(dir); ok {
		return root
	}
	return dir
}

func (c config) envDir(root, env string) string {
	return filepath.Join(root, c.envsDir, env)
}

// envNames returns the names of the environment folders under root, sorted.
func (c config) envNames(root string) ([]string, error) {
	dir := filepath.Join(root, c.envsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		// Stat follows symlinks, so a linked environment folder still counts.
		if info, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && info.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// detectEnv returns the environment whose backend config matches the backend
// Terraform was last initialized with in root, or "" if none does.
func (c config) detectEnv(root string) string {
	want := c.initializedKey(root)
	if want == "" {
		return ""
	}
	names, _ := c.envNames(root)
	for _, name := range names {
		if c.backendKey(filepath.Join(c.envDir(root, name), c.backend)) == want {
			c.debugf("detected %s in %s", name, root)
			return name
		}
	}
	c.debugf("no environment in %s has %s = %q", root, c.configKey, want)
	return ""
}

// initializedKey returns the configKey setting Terraform recorded for the
// backend at its last init in root, or "" if there is none.
func (c config) initializedKey(root string) string {
	data, err := os.ReadFile(filepath.Join(root, ".terraform", "terraform.tfstate"))
	if err != nil {
		return ""
	}
	var state struct {
		Backend struct {
			Config map[string]any `json:"config"`
		} `json:"backend"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		c.debugf("reading backend state in %s: %v", root, err)
		return ""
	}
	key, _ := state.Backend.Config[c.configKey].(string)
	return key
}

// backendKey returns the configKey string set in the backend config file at
// path, or "" if the file is missing or invalid or doesn't set it to a
// literal string.
func (c config) backendKey(path string) string {
	src, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	file, diags := hclsyntax.ParseConfig(src, path, hcl.InitialPos)
	if diags.HasErrors() {
		c.debugf("parsing %s: %s", path, diags.Error())
		return ""
	}
	attr, ok := file.Body.(*hclsyntax.Body).Attributes[c.configKey]
	if !ok {
		return ""
	}
	val, diags := attr.Expr.Value(nil)
	if diags.HasErrors() || !val.IsKnown() || val.IsNull() || val.Type() != cty.String {
		return ""
	}
	return val.AsString()
}

// contextFor returns the variables that select env in root, after checking
// that its backend config and variable file exist.
func (c config) contextFor(root, env string) ([]envVar, error) {
	dir := c.envDir(root, env)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, errorf(2, "no environment %q in %s (see tfctx ls)", env, filepath.Join(root, c.envsDir))
	}
	backend := filepath.Join(dir, c.backend)
	vars := filepath.Join(dir, c.vars)
	if !isFile(backend) {
		return nil, errorf(2, "backend config missing: %s", backend)
	}
	if !isFile(vars) {
		return nil, errorf(2, "var file missing: %s", vars)
	}
	// Terraform splits TF_CLI_ARGS_* the way a shell would, so quote the
	// paths in case they contain spaces.
	varArgs := joinArgs("-var-file="+quote(vars), c.varOpts)
	return []envVar{
		{"TFCTX_ENV", env},
		{"TFCTX_ROOT_DIR", root},
		{"TF_CLI_ARGS_init", joinArgs("-backend-config="+quote(backend), "-reconfigure", c.initOpts)},
		{"TF_CLI_ARGS_plan", varArgs},
		{"TF_CLI_ARGS_apply", varArgs},
		{"TF_CLI_ARGS_destroy", varArgs},
	}, nil
}

// joinArgs joins the non-empty args with spaces.
func joinArgs(args ...string) string {
	return strings.Join(slices.DeleteFunc(args, func(s string) bool { return s == "" }), " ")
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// hook writes the shell code that brings the calling shell's context in line
// with the Terraform root containing the working directory: the variables for
// the environment detect returns for that root, an unset when it returns "",
// or nothing at all when the shell is already up to date.
func (c config) hook(w io.Writer, detect func(root string) string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	var env, root string
	if r, ok := findRoot(cwd); ok {
		if env = detect(r); env != "" {
			root = r
		}
	}
	if env == os.Getenv("TFCTX_ENV") && root == os.Getenv("TFCTX_ROOT_DIR") {
		return nil
	}
	if env == "" {
		c.debugf("clearing context")
		writeUnset(w)
		return nil
	}
	vars, err := c.contextFor(root, env)
	if err != nil {
		writeUnset(w)
		return err
	}
	writeExports(w, vars)
	return nil
}

// use writes the shell code that switches the calling shell to env, after
// running terraform init when auto-init is on. If init fails it clears the
// context instead, because the backend may be left half-switched.
func (c config) use(w io.Writer, env string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root := rootFor(cwd)
	vars, err := c.contextFor(root, env)
	if err != nil {
		return err
	}
	if c.autoInit {
		if err := terraformInit(root, vars); err != nil {
			writeUnset(w)
			return err
		}
	}
	writeExports(w, vars)
	return nil
}

// terraformInit runs terraform init in root with vars added to its
// environment. Terraform's output is shown only if it fails.
func terraformInit(root string, vars []envVar) error {
	cmd := exec.Command("terraform", "-chdir="+root, "init")
	cmd.Env = os.Environ()
	for _, v := range vars {
		cmd.Env = append(cmd.Env, v.name+"="+v.value)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return errorf(3, "terraform init failed (%v) — see log below\n%s", err, out)
	}
	return nil
}

// list writes the environments for the working directory's Terraform root,
// marking the current one. With quiet it writes bare names, for completion.
func (c config) list(w io.Writer, quiet bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root := rootFor(cwd)
	dir := filepath.Join(root, c.envsDir)
	names, err := c.envNames(root)
	if err != nil {
		return errorf(1, "no environments folder at %s", dir)
	}
	if quiet {
		for _, name := range names {
			fmt.Fprintln(w, name)
		}
		return nil
	}
	current := ""
	if os.Getenv("TFCTX_ROOT_DIR") == root {
		current = os.Getenv("TFCTX_ENV")
	}
	fmt.Fprintf(w, "Available environments in %s:\n", shorten(dir, 30))
	for _, name := range names {
		if name == current {
			name += " (current)"
		}
		fmt.Fprintf(w, "  %s\n", name)
	}
	return nil
}

// shorten trims s to its last limit characters, starting with "...".
func shorten(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return "..." + string(r[len(r)-(limit-3):])
}

const backendTemplate = `# Backend configuration for %[1]s environment
bucket = "your-terraform-state-bucket"
key    = "environments/%[1]s/terraform.tfstate"
region = "us-east-1"
`

const varsTemplate = `# Variables for %[1]s environment
environment = "%[1]s"

# Add your environment-specific variables here
`

// create makes a new environment folder with template backend config and
// variable files.
func (c config) create(w io.Writer, env string) error {
	// The shell wrapper sends these to the binary as commands, so an
	// environment with one of these names could never be switched to.
	if env == "" || strings.HasPrefix(env, "-") || slices.Contains(commands, env) {
		return errorf(1, "%q can't be an environment name", env)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	dir := c.envDir(rootFor(cwd), env)
	if _, err := os.Stat(dir); err == nil {
		return errorf(1, "environment '%s' already exists", env)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errorf(1, "failed to create directory %s: %v", dir, err)
	}
	backend := filepath.Join(dir, c.backend)
	vars := filepath.Join(dir, c.vars)
	if err := os.WriteFile(backend, fmt.Appendf(nil, backendTemplate, env), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(vars, fmt.Appendf(nil, varsTemplate, env), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(w, "Created environment '%s' in %s\n", env, dir)
	fmt.Fprintln(w, "Please edit the following files to match your expectation:")
	fmt.Fprintf(w, "  - %s (backend configuration)\n", backend)
	fmt.Fprintf(w, "  - %s (environment variables)\n", vars)
	return nil
}
