package operations

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operationlock"
	"path/filepath"
	"slices"
	"strings"
)

func (a *execution) selectedRuntime(workspace string) (string, error) {
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
func (a *execution) installLauncher(base string, workspaces []string) error {
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
func (a *execution) create(workspace, namespace, memory string) error {
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
		if err = a.writeJSON(filepath.Join(runtime, "registry.json"), registry); err != nil {
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
		if err = a.writeJSON(filepath.Join(runtime, "selection.json"), a.Selection); err != nil {
			return err
		}
	}
	// Active profiles and their saved selection have a single atomic commit.
	// Progressive registry writes above deliberately retain the previous state.
	registry.Active = a.profiles()
	registry.Catalog = &a.Catalog
	registry.Selection = a.Selection
	if err = a.writeJSON(filepath.Join(runtime, "registry.json"), registry); err != nil {
		return err
	}
	if err = a.writeJSON(filepath.Join(a.Root, ".generated", "runtime.json"), map[string]string{"path": runtime}); err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "Host runtime:", runtime)
	return nil
}
func (a *execution) listAll() error {
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
func (a *execution) prune(home string, dryRun bool, targets []Target) error {
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
	if targets != nil {
		// An explicitly empty confirmed set must never resolve new missing rows.
		confirmed := map[string][]Target{}
		for _, t := range targets {
			confirmed[t.Runtime] = append(confirmed[t.Runtime], t)
		}
		installed = nil
		runtimes := make([]string, 0, len(confirmed))
		for runtime := range confirmed {
			runtimes = append(runtimes, runtime)
		}
		slices.Sort(runtimes)
		for _, runtime := range runtimes {
			canonical, err := config.Canonical(runtime)
			if err != nil {
				return err
			}
			if canonical != runtime || filepath.Dir(runtime) != base {
				return errors.New("confirmed prune runtime identity changed")
			}
			var reg config.Registry
			if err = config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
				return err
			}
			if err = validateObservedRegistry(reg, runtime); err != nil {
				return err
			}
			scope := confirmed[runtime]
			count := len(reg.Profiles)
			if count == 0 {
				count = 1
			}
			if len(scope) != count {
				return errors.New("confirmed prune registry scope changed")
			}
			seen := map[string]bool{}
			for _, t := range scope {
				if t.Workspace != reg.Workspace || seen[t.Profile] {
					return errors.New("confirmed prune workspace changed")
				}
				seen[t.Profile] = true
				if len(reg.Profiles) == 0 {
					if t.Profile != "" || t.Entry != (config.Entry{}) {
						return errors.New("confirmed prune empty registry changed")
					}
				} else if entry, ok := reg.Profiles[t.Profile]; !ok || t.Entry == (config.Entry{}) || entry != t.Entry {
					return errors.New("confirmed prune entry changed")
				}
			}
			installed = append(installed, config.Installed{Runtime: runtime, Registry: reg})
		}
	}
	var missing []config.Installed
	var sandboxes []string
	for _, i := range installed {
		if !i.Missing() {
			continue
		}
		if err = validateObservedRegistry(i.Registry, i.Runtime); err != nil {
			return err
		}
		missing = append(missing, i)
		profiles := sortedProfiles(i.Registry.Profiles)
		if len(profiles) == 0 {
			a.missing = append(a.missing, Target{Workspace: i.Registry.Workspace, Runtime: i.Runtime})
		}
		for _, p := range profiles {
			a.missing = append(a.missing, Target{Workspace: i.Registry.Workspace, Runtime: i.Runtime, Profile: p, Entry: i.Registry.Profiles[p]})
		}
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
	// Acquire every affected sandbox before changing provider or registry metadata.
	// Keep these lock inodes even after pruning: automatic callers can retain an
	// entry after its registry is forgotten and must still acquire the same flock.
	var releases []func()
	defer func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}()
	for _, i := range missing {
		if filepath.Dir(i.Runtime) != base {
			return fmt.Errorf("refusing to remove %s outside %s", i.Runtime, base)
		}
		var keys []string
		for _, entry := range i.Registry.Profiles {
			if entry.Sandbox != "" {
				keys = append(keys, operationlock.SandboxKey(entry.Sandbox))
			}
		}
		release, err := operationlock.Acquire(a.ctx, i.Runtime, keys)
		if err != nil {
			return err
		}
		releases = append(releases, release)
	}
	// Sandbox acquisition may take place after a workspace or registry was edited.
	checked := missing[:0]
	for _, i := range missing {
		valid, err := pruneStillMissing(i)
		if err != nil {
			return err
		}
		if valid {
			checked = append(checked, i)
		}
	}
	missing = checked
	if len(missing) == 0 {
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
		// Forget launch routing and saved selection, retaining update history and
		// lock files because the underlying sandboxes are deliberately preserved.
		for _, name := range []string{"selection.json", "agent-wrapper", "registry.json"} {
			if err = a.ctx.Err(); err != nil {
				return err
			}
			valid, err := pruneStillMissing(i)
			if err != nil {
				return err
			}
			if !valid {
				break
			}
			if err = os.Remove(filepath.Join(i.Runtime, name)); err != nil && !os.IsNotExist(err) {
				return err
			}
			a.changed = true
			if name == "registry.json" {
				fmt.Fprintln(a.Out, "Removed registry:", i.Runtime)
			}
		}
	}
	if len(sandboxes) > 0 {
		fmt.Fprintln(a.Out, "Sandboxes were kept. To delete them (irreversible):")
		fmt.Fprintln(a.Out, "  sbx rm", strings.Join(sandboxes, " "))
	}
	return nil
}

func pruneStillMissing(i config.Installed) (bool, error) {
	runtime, err := config.Canonical(i.Runtime)
	if err != nil {
		return false, err
	}
	if runtime != i.Runtime {
		return false, errors.New("confirmed prune runtime path changed")
	}
	var current config.Registry
	if err = config.ReadJSON(filepath.Join(runtime, "registry.json"), &current); err != nil {
		return false, err
	}
	if err = validateObservedRegistry(current, runtime); err != nil {
		return false, err
	}
	if current.Workspace != i.Registry.Workspace || !maps.Equal(current.Profiles, i.Registry.Profiles) {
		return false, errors.New("confirmed prune registry identity changed")
	}
	return (config.Installed{Runtime: runtime, Registry: current}).Missing(), nil
}
