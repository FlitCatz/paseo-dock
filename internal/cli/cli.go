// Package cli implements host operations. Subprocesses receive argument arrays.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/charmbracelet/x/term"
	"io"
	"os"
	"os/exec"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/sandboxupdate"
	"paseo-sbx/internal/tui"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type App struct {
	Root      string
	Lock      config.Lock
	Catalog   config.Catalog
	Selection *config.Selection
	In        io.Reader
	Out, Err  io.Writer
}

func NewApp(root string) (*App, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	lock, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	catalog, err := config.LoadCatalog(root)
	return &App{Root: root, Lock: lock, Catalog: catalog, In: os.Stdin, Out: os.Stdout, Err: os.Stderr}, err
}
func (a *App) image(profile, namespace string) string {
	tag := a.Lock.Tag
	if a.Selection != nil {
		p, _ := a.Catalog.Pair(profile)
		s := *a.Selection
		s.Pairs = []config.Pair{p}
		tag = config.ImageTag(a.Lock, s, a.Catalog)
	}
	return namespace + "/" + profile + ":" + tag
}
func (a *App) profiles() []string {
	if a.Selection != nil {
		return a.Selection.Profiles()
	}
	return a.Catalog.AllProfiles()
}
func (a *App) BakeConfig(namespace, platforms string) map[string]any {
	targets := map[string]any{}
	for _, p := range a.profiles() {
		pair, _ := a.Catalog.Pair(p)
		agent, tool := pair.Agent, pair.Methodology
		item, _ := a.Catalog.Agent(agent)
		utilities := ""
		if a.Selection != nil {
			utilities = strings.Join(a.Selection.Utilities, ",")
		}
		methodInstaller := ""
		for _, m := range a.Catalog.Methodologies {
			if m.ID == tool {
				methodInstaller = m.Installer
			}
		}
		t := map[string]any{"context": a.Root, "dockerfile": "Dockerfile", "tags": []string{a.image(p, namespace)}, "args": map[string]string{"BASE_IMAGE": a.Lock.BaseImages[agent], "AGENT": item.Adapter, "AGENT_VERSION": a.Lock.Agents[agent], "TOOL": tool, "METHOD_INSTALLER": methodInstaller, "UTILITIES": utilities}}
		if platforms != "" {
			t["platforms"] = strings.Split(platforms, ",")
		}
		targets[p] = t
	}
	return map[string]any{"group": map[string]any{"default": map[string]any{"targets": a.profiles()}}, "target": targets}
}
func (a *App) run(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = a.Root
	cmd.Stdin = a.In
	cmd.Stdout = a.Out
	cmd.Stderr = a.Err
	return cmd.Run()
}
func (a *App) capture(args ...string) ([]byte, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = a.Root
	cmd.Stderr = a.Err
	return cmd.Output()
}
func (a *App) selectedRuntime(workspace string) (string, error) {
	if workspace != "" {
		w, err := config.Canonical(workspace)
		if err != nil {
			return "", err
		}
		return config.RuntimePath(w)
	}
	var state struct {
		Path string `json:"path"`
	}
	if err := config.ReadJSON(filepath.Join(a.Root, ".generated", "runtime.json"), &state); err != nil {
		return "", err
	}
	if state.Path == "" {
		return "", errors.New("missing runtime path; run create first")
	}
	return config.Canonical(state.Path)
}

// checkWorkspace rejects workspaces that overlap the trusted runtime area.
func checkWorkspace(base, w string) error {
	b, err := config.Canonical(base)
	if err != nil {
		return err
	}
	if config.Within(b, w) {
		return fmt.Errorf("workspace %s must not contain the trusted host runtime or user home", w)
	}
	if config.Within(w, b) {
		return fmt.Errorf("workspace %s must not be inside the trusted host runtime", w)
	}
	return nil
}

// installLauncher installs the shared launcher after checking that no
// sandbox-writable workspace contains its source.
func (a *App) installLauncher(base string, workspaces []string) error {
	source := filepath.Join(a.Root, "bin", "agent-wrapper")
	if exe, e := os.Executable(); e == nil {
		candidate := filepath.Join(filepath.Dir(exe), "agent-wrapper")
		if info, e := os.Stat(candidate); e == nil && info.Mode().IsRegular() {
			source = candidate
		}
	}
	dir, err := config.Canonical(filepath.Dir(source))
	if err != nil {
		return err
	}
	for _, w := range workspaces {
		if w, err = config.Canonical(w); err != nil {
			return err
		}
		if config.Within(dir, w) {
			return fmt.Errorf("workspace %s must not contain the agent-wrapper source", w)
		}
	}
	b, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("build agent-wrapper with make build: %w", err)
	}
	target := config.LauncherPath(base)
	for _, d := range []string{base, filepath.Dir(target)} {
		if err = os.MkdirAll(d, 0700); err != nil {
			return err
		}
		if err = os.Chmod(d, 0700); err != nil {
			return err
		}
	}
	f, err := os.CreateTemp(filepath.Dir(target), ".agent-wrapper-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Chmod(0700); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), target)
}
func (a *App) create(workspace, namespace, memory string) error {
	w, err := config.Canonical(workspace)
	if err != nil {
		return err
	}
	runtime, err := config.RuntimePath(w)
	if err != nil {
		return err
	}
	base := filepath.Dir(runtime)
	if err = checkWorkspace(base, w); err != nil {
		return err
	}
	// The source root supplies the host-executed agent-wrapper; keep it outside
	// the sandbox-writable workspace.
	root, err := config.Canonical(a.Root)
	if err != nil {
		return err
	}
	if config.Within(root, w) {
		return errors.New("workspace must not contain the paseo-sbx source root")
	}
	if err = os.MkdirAll(w, 0755); err != nil {
		return err
	}
	saved := config.Registry{}
	err = config.ReadJSON(filepath.Join(runtime, "registry.json"), &saved)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	b, err := a.capture("sbx", "ls", "--json")
	if err != nil {
		return err
	}
	var rows []map[string]any
	if err = json.Unmarshal(b, &rows); err != nil {
		var wrapped struct {
			Sandboxes []map[string]any `json:"sandboxes"`
		}
		if err = json.Unmarshal(b, &wrapped); err != nil {
			return err
		}
		rows = wrapped.Sandboxes
	}
	names := map[string]bool{}
	for _, row := range rows {
		name, _ := row["name"].(string)
		names[name] = true
	}
	if saved.Workspace != "" && saved.Workspace != w {
		return errors.New("registry workspace mismatch")
	}
	registry := config.Registry{Workspace: w, Profiles: map[string]config.Entry{}, Active: saved.ActiveProfiles(), Catalog: saved.Catalog, Selection: saved.Selection}
	for p, e := range saved.Profiles {
		registry.Profiles[p] = e
	}
	planned := map[string]config.Entry{}
	// Validate every reused sandbox before writing or creating anything. Keep
	// untouched entries during progressive writes so partial failures can retry.
	for _, p := range a.profiles() {
		pair, _ := a.Catalog.Pair(p)
		agent, tool := pair.Agent, pair.Methodology
		adapter, _ := a.Catalog.Agent(agent)
		name := "paseo-" + p + "-" + filepath.Base(runtime)
		entry := config.Entry{Agent: adapter.Adapter, Tool: tool, Workspace: w, Sandbox: name, Image: a.image(p, namespace)}
		planned[p] = entry
		if names[name] && saved.Profiles[p] != entry {
			return fmt.Errorf("existing sandbox %s does not match the selected image/settings; stop and replace it explicitly or use another workspace (existing sandbox preserved)", name)
		}
		if prior, exists := saved.Profiles[p]; exists {
			registry.Profiles[p] = prior
		}
	}
	for _, p := range a.profiles() {
		entry := planned[p]
		name := entry.Sandbox
		if names[name] {
			fmt.Fprintln(a.Out, "Reusing", name)
		} else {
			command := []string{"sbx", "create", "--name", name, "--pull", "never", "--skills", "off", "--cpus", "2", "--memory", memory, "--template", entry.Image}
			if entry.Agent == "antigravity-cli" {
				command = append(command, "--env", "GEMINI_API_KEY=proxy-managed")
			}
			command = append(command, config.SandboxAgent(entry.Agent), w)
			if err = a.run(command...); err != nil {
				return err
			}
		}
		registry.Profiles[p] = entry
		if err = config.WriteJSON(filepath.Join(runtime, "registry.json"), registry); err != nil {
			return err
		}
	}
	// OpenSpec writes project-specific integrations. Initialize the selected
	// tools together, inside one selected sandbox, before committing activation.
	var openspecSandbox string
	var openspecTools []string
	for _, p := range a.profiles() {
		pair, _ := a.Catalog.Pair(p)
		for _, method := range a.Catalog.Methodologies {
			if method.ID != pair.Methodology || method.Installer != "openspec" {
				continue
			}
			entry := planned[p]
			tool := entry.Agent
			if tool == "antigravity-cli" {
				tool = "antigravity"
			}
			if !slices.Contains(openspecTools, tool) {
				openspecTools = append(openspecTools, tool)
			}
			if openspecSandbox == "" {
				openspecSandbox = entry.Sandbox
			}
		}
	}
	if openspecSandbox != "" {
		slices.Sort(openspecTools)
		fmt.Fprintln(a.Out, "Initializing OpenSpec in", w)
		if err = a.run("sbx", "exec", "--env", "OPENSPEC_TELEMETRY=0", "--workdir", w, openspecSandbox, "openspec", "init", w, "--tools", strings.Join(openspecTools, ","), "--profile", "core", "--no-animation"); err != nil {
			return fmt.Errorf("OpenSpec initialization failed; sandbox registry retained for retry: %w", err)
		}
	}
	if err = a.installLauncher(base, []string{w}); err != nil {
		return err
	}
	if a.Selection != nil {
		if err = config.WriteJSON(filepath.Join(runtime, "selection.json"), a.Selection); err != nil {
			return err
		}
	}
	// Active profiles and their saved selection have a single atomic commit.
	// Progressive registry writes above deliberately retain the previous state.
	registry.Active = a.profiles()
	registry.Catalog = &a.Catalog
	registry.Selection = a.Selection
	if err = config.WriteJSON(filepath.Join(runtime, "registry.json"), registry); err != nil {
		return err
	}
	if err = config.WriteJSON(filepath.Join(a.Root, ".generated", "runtime.json"), map[string]string{"path": runtime}); err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "Host runtime:", runtime)
	return nil
}

var disabledTools = []string{"create_workspace", "list_workspaces", "archive_workspace", "rename_workspace", "list_workspace_scripts", "start_workspace_script", "stop_workspace_script", "list_terminals", "create_terminal", "kill_terminal", "capture_terminal", "send_terminal_keys", "create_schedule", "create_heartbeat", "delete_heartbeat", "list_schedules", "inspect_schedule", "pause_schedule", "resume_schedule", "delete_schedule", "update_schedule", "schedule_logs", "run_schedule_once", "speak", "archive_agent", "kill_agent", "update_agent", "set_agent_mode", "list_pending_permissions", "respond_to_permission", "list_profiles", "inspect_provider"}

// ActiveAdapters maps every profile active in any workspace to its agent adapter.
func ActiveAdapters(installed []config.Installed) map[string]string {
	adapters := map[string]string{}
	for _, i := range installed {
		for _, p := range i.Registry.ActiveProfiles() {
			if e, ok := i.Registry.Profiles[p]; ok {
				adapters[p] = e.Agent
			}
		}
	}
	return adapters
}

// ProviderConfig names providers by profile only; the shared launcher picks
// the workspace sandbox from the session cwd at launch.
func ProviderConfig(launcher string, adapters map[string]string) map[string]any {
	providers := map[string]any{}
	for p, agent := range adapters {
		if agent == "antigravity-cli" {
			// The existing plugin owns this provider. Per-agent options select SBX.
			continue
		}
		command := []string{launcher, p}
		provider := map[string]any{"extends": config.ProviderAdapter(agent), "label": "SBX " + p, "enabled": true, "command": command, "paseoTools": map[string]any{"enabled": true, "disabledTools": disabledTools}}
		providers[p] = provider
	}
	return map[string]any{"version": 1, "daemon": map[string]any{"mcp": map[string]any{"enabled": true, "injectIntoAgents": true}}, "agents": map[string]any{"providers": providers}}
}
func object(parent map[string]any, key string) map[string]any {
	v, ok := parent[key].(map[string]any)
	if !ok {
		v = map[string]any{}
		parent[key] = v
	}
	return v
}
func MergeConfig(current, generated map[string]any) {
	for _, keys := range [][]string{{"daemon", "mcp"}, {"agents", "providers"}} {
		dest := object(object(current, keys[0]), keys[1])
		src := object(object(generated, keys[0]), keys[1])
		for k, v := range src {
			dest[k] = v
		}
	}
}

// ownedCommand reports the shared launcher or a legacy per-workspace launcher under base.
func ownedCommand(command string, base string) bool {
	if command == config.LauncherPath(base) {
		return true
	}
	return filepath.Base(command) == "agent-wrapper" && filepath.Dir(filepath.Dir(command)) == base
}

// Prune only entries whose exact executable belongs to the trusted runtime area.
func MergeManagedConfig(current, generated map[string]any, base string) {
	providers := object(object(current, "agents"), "providers")
	wanted := object(object(generated, "agents"), "providers")
	for p, value := range providers {
		entry, ok := value.(map[string]any)
		if !ok {
			continue
		}
		var command []string
		switch v := entry["command"].(type) {
		case []any:
			for _, arg := range v {
				s, ok := arg.(string)
				if !ok {
					command = nil
					break
				}
				command = append(command, s)
			}
		case []string:
			command = v
		}
		managed := len(command) == 2 || (len(command) == 3 && command[2] == "--acp" && strings.HasPrefix(p, "antigravity-cli-"))
		if managed && ownedCommand(command[0], base) && command[1] == p {
			if _, ok := wanted[p]; !ok {
				delete(providers, p)
			}
		}
	}
	MergeConfig(current, generated)
}
func (a *App) register(runtime, home string, apply bool) error {
	var reg config.Registry
	if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
		return err
	}
	if len(reg.ActiveProfiles()) == 0 {
		return errors.New("no complete active selection; finish create before registering")
	}
	return a.syncProviders(home, apply)
}

// unregister removes a workspace from launch routing; its sandboxes and saved
// selection are kept so a later create can reactivate them.
func (a *App) unregister(workspace, home string) error {
	w, err := config.Canonical(workspace)
	if err != nil {
		return err
	}
	runtime, err := config.RuntimePath(w)
	if err != nil {
		return err
	}
	path := filepath.Join(runtime, "registry.json")
	var reg config.Registry
	if err = config.ReadJSON(path, &reg); err != nil {
		return err
	}
	reg.Active = []string{}
	if err = config.WriteJSON(path, reg); err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "Unregistered workspace:", w)
	return a.syncProviders(home, true)
}
func (a *App) listAll() error {
	base, err := config.BaseDir()
	if err != nil {
		return err
	}
	installed, err := config.Registries(base)
	if err != nil {
		return err
	}
	type row struct {
		Workspace string   `json:"workspace"`
		Runtime   string   `json:"runtime"`
		Active    []string `json:"active_profiles"`
		Missing   bool     `json:"missing,omitempty"`
	}
	rows := []row{}
	for _, i := range installed {
		rows = append(rows, row{i.Registry.Workspace, i.Runtime, i.Registry.ActiveProfiles(), i.Missing()})
	}
	enc := json.NewEncoder(a.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

// prune forgets registries of deleted workspaces. Sandboxes hold login state
// and sessions, so removing them is left to an explicit `sbx rm`.
func (a *App) prune(home string, dryRun bool) error {
	base, err := config.BaseDir()
	if err != nil {
		return err
	}
	if base, err = config.Canonical(base); err != nil {
		return err
	}
	installed, err := config.Registries(base)
	if err != nil {
		return err
	}
	var missing []config.Installed
	var sandboxes []string
	for _, i := range installed {
		if !i.Missing() {
			continue
		}
		missing = append(missing, i)
		names := []string{}
		for _, e := range i.Registry.Profiles {
			if e.Sandbox != "" {
				names = append(names, e.Sandbox)
			}
		}
		slices.Sort(names)
		sandboxes = append(sandboxes, names...)
		fmt.Fprintf(a.Out, "Missing workspace: %s\n  registry: %s\n  sandboxes: %s\n", i.Registry.Workspace, i.Runtime, strings.Join(names, ", "))
	}
	if len(missing) == 0 {
		fmt.Fprintln(a.Out, "No missing workspaces.")
		return nil
	}
	if dryRun {
		return nil
	}
	// Providers already ignore missing workspaces; sync first so a failure
	// leaves every registry in place.
	if err = a.syncProviders(home, true); err != nil {
		return err
	}
	for _, i := range missing {
		if filepath.Dir(i.Runtime) != base {
			return fmt.Errorf("refusing to remove %s outside %s", i.Runtime, base)
		}
		if err = os.RemoveAll(i.Runtime); err != nil {
			return err
		}
		fmt.Fprintln(a.Out, "Removed registry:", i.Runtime)
	}
	if len(sandboxes) > 0 {
		fmt.Fprintln(a.Out, "Sandboxes were kept. To delete them (irreversible):")
		fmt.Fprintln(a.Out, "  sbx rm", strings.Join(sandboxes, " "))
	}
	return nil
}

// syncProviders registers the union of active profiles across all workspaces.
func (a *App) syncProviders(home string, apply bool) error {
	base, err := config.BaseDir()
	if err != nil {
		return err
	}
	if base, err = config.Canonical(base); err != nil {
		return err
	}
	installed, err := config.Registries(base)
	if err != nil {
		return err
	}
	// Deleted workspaces keep their registry until prune but get no providers.
	workspaces, live := []string{}, []config.Installed{}
	for _, i := range installed {
		if i.Missing() {
			continue
		}
		live = append(live, i)
		w, err := config.Canonical(i.Registry.Workspace)
		if err != nil {
			return err
		}
		if err = checkWorkspace(base, w); err != nil {
			return err
		}
		workspaces = append(workspaces, w)
	}
	if err = a.installLauncher(base, workspaces); err != nil {
		return err
	}
	generated := ProviderConfig(config.LauncherPath(base), ActiveAdapters(live))
	path := filepath.Join(a.Root, ".generated", "paseo.providers.json")
	if err := config.WriteJSON(path, generated); err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "Provider configuration:", path)
	if !apply {
		b, _ := os.ReadFile(path)
		_, err := a.Out.Write(b)
		return err
	}
	home, err = config.Canonical(home)
	if err != nil {
		return err
	}
	path = filepath.Join(home, "config.json")
	current := map[string]any{"version": 1}
	original, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if err = json.Unmarshal(original, &current); err != nil {
			return err
		}
		if current == nil {
			return errors.New("Paseo config must be an object")
		}
		backup := path + ".backup-" + time.Now().UTC().Format("20060102T150405.000000000Z")
		if err = os.WriteFile(backup, original, 0600); err != nil {
			return err
		}
		fmt.Fprintln(a.Out, "Existing configuration backed up:", backup)
	}
	MergeManagedConfig(current, generated, base)
	if err = config.WriteJSON(path, current); err != nil {
		return err
	}
	// Paseo now points at the shared launcher; retire per-workspace copies.
	for _, i := range installed {
		if err = os.Remove(filepath.Join(i.Runtime, "agent-wrapper")); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	b, err := a.capture("paseo", "--home", home, "status", "--json")
	if err != nil {
		return err
	}
	var status map[string]any
	if err = json.Unmarshal(b, &status); err != nil {
		return err
	}
	action := "start"
	if status["localDaemon"] == "running" {
		action = "reload"
	}
	return a.run("paseo", "--home", home, action)
}
func Run(root string, args []string, in io.Reader, out, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: paseo-sbx {build|load|create|register|run|unregister|prune|list|shell|stop|update} [options]")
		return 2
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Fprintln(out, "usage: paseo-sbx {build|load|create|register|run|unregister|prune|list|shell|stop|update} [options]\nRun paseo-sbx COMMAND --help for command options.")
		return 0
	}
	app, err := NewApp(root)
	if err != nil {
		fmt.Fprintln(stderr, "paseo-sbx:", err)
		return 1
	}
	app.In, app.Out, app.Err = in, out, stderr
	if err = app.execute(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "paseo-sbx:", err)
		return 1
	}
	return 0
}
func (a *App) execute(args []string) error {
	command := args[0]
	if command == "run" {
		return a.runPlugin(args[1:])
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(a.Err)
	var profile, workspace, platforms, profiles, utilities, selectionPath, agents, methods string
	namespace := "paseo-sbx"
	memory := "2g"
	home := "~/.paseo"
	var push, keep, apply, printBake, buildFirst, interactive, skipBuild, all, dryRun bool
	if command == "build" || command == "load" || command == "create" {
		f.StringVar(&profiles, "profiles", "", "comma-separated agent-methodology pairs")
		f.StringVar(&profile, "profile", "", "select one agent-methodology pair")
		f.StringVar(&utilities, "utilities", "", "comma-separated utilities; empty selects none")
		f.StringVar(&selectionPath, "selection", "", "read selection JSON file")
		f.StringVar(&agents, "agents", "", "comma-separated agents (requires --methodologies)")
		f.StringVar(&methods, "methodologies", "", "comma-separated methodologies (requires --agents)")
		f.StringVar(&workspace, "workspace", "", "workspace path for saved selection")
	}
	switch command {
	case "build", "load":
		f.StringVar(&namespace, "namespace", namespace, "image namespace")
		if command == "build" {
			f.StringVar(&platforms, "platforms", "", "comma-separated platforms")
			f.BoolVar(&push, "push", false, "publish images")
			f.BoolVar(&printBake, "print-bake", false, "print Bake JSON without running Docker")
		} else {
			f.BoolVar(&keep, "keep-archive", false, "retain image archives")
		}
	case "create":
		f.StringVar(&memory, "memory", memory, "memory per sandbox")
		f.StringVar(&namespace, "namespace", namespace, "image namespace")
		f.BoolVar(&buildFirst, "build", false, "build and load selected images before creating")
		f.BoolVar(&apply, "apply", false, "register selected providers and reload Paseo after creating")
		f.StringVar(&home, "paseo-home", home, "Paseo configuration home")
		f.BoolVar(&interactive, "interactive", false, "open the selection TUI (requires a terminal)")
		f.BoolVar(&skipBuild, "skip-build", false, "use existing loaded templates in interactive mode")
	case "register":
		f.StringVar(&workspace, "workspace", "", "workspace path")
		f.StringVar(&home, "paseo-home", home, "Paseo configuration home")
		f.BoolVar(&apply, "apply", false, "merge config and reload Paseo")
	case "unregister":
		f.StringVar(&workspace, "workspace", "", "workspace path (required)")
		f.StringVar(&home, "paseo-home", home, "Paseo configuration home")
	case "prune":
		f.StringVar(&home, "paseo-home", home, "Paseo configuration home")
		f.BoolVar(&dryRun, "dry-run", false, "show missing workspaces without removing registries")
	case "list", "shell", "stop", "update":
		f.StringVar(&workspace, "workspace", "", "workspace path")
		if command == "update" {
			f.StringVar(&profile, "profile", "", "update one profile; omitted updates all active profiles")
		}
		if command == "list" {
			f.BoolVar(&all, "all", false, "list every registered workspace")
		}
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	// Accept flags before or after the positional profile.
	rest := args[1:]
	if (command == "shell" || command == "stop" || command == "update") && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		profile = rest[0]
		rest = rest[1:]
	}
	if err := f.Parse(rest); err != nil {
		return err
	}
	if command == "shell" || command == "stop" || command == "update" {
		if profile == "" && f.NArg() == 1 {
			profile = f.Arg(0)
		} else if f.NArg() != 0 {
			return errors.New("unexpected arguments")
		}
	} else if f.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	if _, ok := a.Catalog.Pair(profile); profile != "" && !ok && command != "shell" && command != "stop" {
		return fmt.Errorf("invalid profile %q", profile)
	}
	if command == "build" || command == "load" || command == "create" {
		setUtilities := false
		f.Visit(func(flag *flag.Flag) {
			if flag.Name == "utilities" {
				setUtilities = true
			}
		})
		isTerminal := false
		if input, ok := a.In.(*os.File); ok {
			if output, ok := a.Out.(*os.File); ok {
				isTerminal = term.IsTerminal(input.Fd()) && term.IsTerminal(output.Fd())
			}
		}
		openWizard := command == "create" && (interactive || (isTerminal && profile == "" && profiles == "" && selectionPath == "" && agents == "" && methods == "" && !setUtilities))
		if openWizard {
			if !isTerminal {
				return errors.New("--interactive requires terminal input and output")
			}
			if workspace == "" {
				var err error
				workspace, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			var initial config.Selection
			if runtime, err := a.selectedRuntime(workspace); err == nil {
				initial, _ = readSavedSelection(runtime)
			}
			if profile != "" || profiles != "" || selectionPath != "" || agents != "" || methods != "" || setUtilities {
				var err error
				initial, err = a.resolveSelection(workspace, profile, profiles, utilities, selectionPath, agents, methods, setUtilities)
				if err != nil {
					return err
				}
			}
			result, err := tui.Run(a.Catalog, initial, workspace, memory, a.In, a.Out, apply)
			if err != nil {
				return err
			}
			a.Selection = &result.Selection
			// Reuse CLI validation for locked versions before any Docker action.
			_, err = a.resolveSelection(workspace, "", strings.Join(result.Selection.Profiles(), ","), strings.Join(result.Selection.Utilities, ","), "", "", "", true)
			if err != nil {
				return err
			}
			apply = apply || result.Apply
			buildFirst = !skipBuild
		} else {
			s, err := a.resolveSelection(workspace, profile, profiles, utilities, selectionPath, agents, methods, setUtilities)
			if err != nil {
				return err
			}
			a.Selection = &s
		}
	}
	switch command {
	case "build":
		cfg := a.BakeConfig(namespace, platforms)
		if printBake {
			return json.NewEncoder(a.Out).Encode(cfg)
		}
		return a.build(namespace, platforms, push)
	case "load":
		return a.load(namespace, keep)
	case "create":
		if workspace == "" {
			return errors.New("--workspace is required")
		}
		if buildFirst {
			if err := a.build(namespace, "", false); err != nil {
				return err
			}
			if err := a.load(namespace, false); err != nil {
				return err
			}
		}
		if err := a.create(workspace, namespace, memory); err != nil {
			return err
		}
		if apply {
			runtime, err := a.selectedRuntime(workspace)
			if err != nil {
				return err
			}
			return a.register(runtime, home, true)
		}
		return nil
	case "unregister":
		if workspace == "" {
			return errors.New("--workspace is required")
		}
		return a.unregister(workspace, home)
	case "prune":
		return a.prune(home, dryRun)
	case "list":
		if all {
			return a.listAll()
		}
		fallthrough
	default:
		runtime, err := a.selectedRuntime(workspace)
		if err != nil {
			return err
		}
		if command == "register" {
			return a.register(runtime, home, apply)
		}
		var reg config.Registry
		if err = config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
			return err
		}
		if command == "list" {
			enc := json.NewEncoder(a.Out)
			enc.SetIndent("", "  ")
			return enc.Encode(reg)
		}
		if command == "update" {
			profiles := reg.ActiveProfiles()
			if profile != "" {
				profiles = []string{profile}
			}
			var failures []error
			for _, p := range profiles {
				e, ok := reg.Profiles[p]
				if !ok {
					failures = append(failures, fmt.Errorf("profile %s missing from registry", p))
					continue
				}
				if err := sandboxupdate.Update(runtime, e, true, a.Err); err != nil {
					failures = append(failures, fmt.Errorf("%s: %w", p, err))
				}
			}
			return errors.Join(failures...)
		}
		e, ok := reg.Profiles[profile]
		if !ok {
			return errors.New("profile missing from registry")
		}
		if command == "shell" {
			if err := sandboxupdate.Update(runtime, e, false, a.Err); err != nil {
				fmt.Fprintln(a.Err, "paseo-sbx: update failed; opening shell:", err)
			}
			return a.run("sbx", "exec", "-it", "--workdir", e.Workspace, e.Sandbox, "bash")
		}
		return a.run("sbx", "stop", e.Sandbox)
	}
}
