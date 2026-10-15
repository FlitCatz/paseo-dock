package operations

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/containerpolicy"
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
func (a *execution) create(workspace, namespace, memory string, verifyRelay ...bool) error {
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
	if a.Selection != nil && a.Selection.Schema == 2 {
		return a.createCategory(w, runtime, namespace, memory, saved, verifyRelay...)
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
		if names[name] && !saved.Profiles[p].EqualIdentity(entry) {
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
func (a *execution) prune(home string, dryRun bool, targets []Target, removeRuntimes bool) error {
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
					if t.Profile != "" || !t.Entry.EqualIdentity(config.Entry{}) {
						return errors.New("confirmed prune empty registry changed")
					}
				} else if entry, ok := reg.Profiles[t.Profile]; !ok || t.Entry.EqualIdentity(config.Entry{}) || !entry.EqualIdentity(t.Entry) {
					return errors.New("confirmed prune entry changed")
				}
			}
			installed = append(installed, config.Installed{Runtime: runtime, Registry: reg})
		}
	}
	var missing []config.Installed
	var sandboxes, containers []string
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
		names, ids := []string{}, []string{}
		for _, e := range i.Registry.Profiles {
			if e.EffectiveCategory() == config.CategoryContainer {
				ids = append(ids, e.ContainerID)
			} else if e.Sandbox != "" {
				names = append(names, e.Sandbox)
			}
		}
		slices.Sort(names)
		slices.Sort(ids)
		sandboxes = append(sandboxes, names...)
		containers = append(containers, ids...)
		fmt.Fprintf(a.Out, "Missing workspace: %s\n  registry: %s\n  sandboxes: %s\n", i.Registry.Workspace, i.Runtime, strings.Join(names, ", "))
		if len(ids) > 0 {
			fmt.Fprintln(a.Out, "  containers:", strings.Join(ids, ", "))
		}
	}
	if len(missing) == 0 {
		fmt.Fprintln(a.Out, "No missing workspaces.")
		return nil
	}
	if dryRun && !removeRuntimes {
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
			if entry.Sandbox != "" || entry.ContainerID != "" {
				keys = append(keys, operationlock.EntryKey(entry))
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
	if removeRuntimes {
		return a.pruneRuntimes(home, dryRun, missing)
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
	if len(containers) > 0 {
		fmt.Fprintln(a.Out, "Containers were kept. To delete them (irreversible):")
		for _, id := range containers {
			fmt.Fprintln(a.Out, "  docker rm", id)
		}
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
	if current.Workspace != i.Registry.Workspace || !maps.EqualFunc(current.Profiles, i.Registry.Profiles, config.Entry.EqualIdentity) {
		return false, errors.New("confirmed prune registry identity changed")
	}
	return (config.Installed{Runtime: runtime, Registry: current}).Missing(), nil
}

// createCategory preserves other categories and only activates a completed selection.
// Execute holds the workspace lock; exact existing resources also take their target lock.
func (a *execution) createCategory(w, runtime, namespace, memory string, saved config.Registry, verifyRelay ...bool) error {
	if saved.Workspace != "" && saved.Workspace != w {
		return errors.New("registry workspace mismatch")
	}
	backend, err := a.backend(a.Selection.Category)
	if err != nil {
		return err
	}
	if sbx, ok := backend.(*SBXBackend); ok {
		copy := *sbx
		copy.Memory = memory
		backend = &copy
	}
	registry := saved
	registry.Schema = 2
	registry.Workspace = w
	if registry.Profiles == nil {
		registry.Profiles = map[string]config.Entry{}
	}
	registry.Active = slices.Clone(saved.ActiveProfiles())
	if registry.Active == nil {
		registry.Active = []string{}
	}
	planned := map[string]config.Entry{}
	for _, profile := range a.profiles() {
		entry := config.Entry{Category: a.Selection.Category, Workspace: w, Utilities: slices.Clone(a.Selection.Utilities), Image: a.image(profile, namespace)}
		if entry.Image == "" {
			return fmt.Errorf("no resolved image for %s; build first", profile)
		}
		if entry.Category == config.CategorySandbox {
			entry.Agent = a.Selection.Agent
			entry.Sandbox = "paseo-" + strings.ReplaceAll(profile, "/", "-") + "-" + filepath.Base(runtime)
		} else {
			if a.Selection.Name != "" {
				bundle := a.Selection.BundleEntry()
				entry.Name, entry.Pairs, entry.Connections = bundle.Name, bundle.Pairs, bundle.Connections
			}
			for _, pair := range a.Selection.Pairs {
				if entry.Bundle() {
					break
				}
				candidate := config.Entry{Category: entry.Category, Agent: pair.Agent, Tool: pair.Methodology}
				if candidate.Key() == profile {
					entry.Agent = pair.Agent
					entry.Tool = pair.Methodology
					break
				}
			}
			entry.Network = "bridge"
			if a.Selection.CodexUserNS && slices.Contains(entry.AgentIDs(), "codex") {
				entry.SecurityPolicy = containerpolicy.ID
				entry.SecurityPolicyDigest = containerpolicy.SHA256
			}
			entry.Ports = slices.Clone(a.ports)
			entry, err = validateDockerEntry(entry, true)
			if err != nil {
				return err
			}
		}
		if prior, ok := saved.Profiles[profile]; ok {
			entry.ContainerID = prior.ContainerID
			entry.ImageID = prior.ImageID
			if !entry.EqualIdentity(prior) {
				return fmt.Errorf("existing target %s does not match selected image/settings; existing resource preserved", profile)
			}
		}
		planned[profile] = entry
	}
	// Only SBX needs the name inventory; a Docker outage cannot block sandbox creation.
	names := map[string]bool{}
	if a.Selection.Category == config.CategorySandbox {
		rows, err := backend.List(a.ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			names[row.ID] = true
		}
		for profile, entry := range planned {
			if names[entry.Sandbox] && !saved.Profiles[profile].EqualIdentity(entry) {
				return fmt.Errorf("sandbox name %s is not owned by this registry", entry.Sandbox)
			}
		}
	}
	for _, profile := range a.profiles() {
		entry := planned[profile]
		release, err := operationlock.Acquire(a.ctx, runtime, []string{operationlock.EntryKey(entry)})
		if err != nil {
			return err
		}
		// Re-read trusted registry after acquiring the resource lock.
		var current config.Registry
		readErr := config.ReadJSON(filepath.Join(runtime, "registry.json"), &current)
		if prior, exists := saved.Profiles[profile]; exists {
			if readErr != nil {
				release()
				return readErr
			}
			if err := current.ValidateIdentity(profile, prior); err != nil {
				release()
				return err
			}
		} else if readErr == nil {
			if _, exists := current.Profiles[profile]; exists {
				release()
				return errors.New("target appeared during creation")
			}
		} else if !os.IsNotExist(readErr) {
			release()
			return readErr
		}
		if !names[entry.Sandbox] {
			created, createErr := backend.Create(a.ctx, entry)
			a.changed = true
			if created.Category == config.CategoryContainer && created.ContainerID != "" {
				a.remaining = append(a.remaining, Target{Category: created.Category, Workspace: w, Runtime: runtime, Profile: profile, Entry: created})
			}
			if createErr != nil {
				// A recovered CID is retained for diagnosis/retry without activating it.
				if created.Category == config.CategoryContainer && created.ContainerID != "" {
					registry.Profiles[profile] = created
					a.uncertain = true
					createErr = errors.Join(createErr, a.writeJSON(filepath.Join(runtime, "registry.json"), registry))
				}
				release()
				return createErr
			}
			entry = created
		}
		registry.Profiles[profile] = entry
		err = a.writeJSON(filepath.Join(runtime, "registry.json"), registry)
		if err != nil && entry.Category == config.CategoryContainer {
			a.uncertain = true
		}
		// A bundle initializes OpenSpec once for every agent paired with it.
		var openspecTools []string
		for _, pair := range append([]config.Pair{{Agent: entry.Agent, Methodology: entry.Tool}}, entry.Pairs...) {
			if pair.Methodology != "openspec" {
				continue
			}
			tool := pair.Agent
			if tool == "antigravity-cli" {
				tool = "antigravity"
			}
			openspecTools = append(openspecTools, tool)
		}
		if err == nil && len(openspecTools) > 0 {
			slices.Sort(openspecTools)
			err = backend.Exec(a.ctx, entry, []string{"env", "OPENSPEC_TELEMETRY=0", "openspec", "init", w, "--tools", strings.Join(openspecTools, ","), "--profile", "core", "--no-animation"}, nil, a.Out, a.Err)
			if err != nil {
				err = fmt.Errorf("OpenSpec initialization failed; resource retained: %w", err)
			}
		}
		release()
		if err != nil {
			return err
		}
	}
	if a.Selection.Category == config.CategoryContainer {
		for _, key := range a.profiles() {
			if err := a.checkACP(backend, key, registry.Profiles[key]); err != nil {
				return err
			}
		}
	}
	if len(verifyRelay) > 0 && verifyRelay[0] && a.Selection.Category == config.CategoryContainer {
		proposed := registry
		proposed.Active = slices.Clone(registry.Active)
		for _, key := range a.profiles() {
			if !slices.Contains(proposed.Active, key) {
				proposed.Active = append(proposed.Active, key)
			}
		}
		if err := a.checkProspectiveDockerRelays(runtime, proposed); err != nil {
			a.uncertain = true
			return err
		}
	}
	if err := a.installLauncher(filepath.Dir(runtime), []string{w}); err != nil {
		return err
	}
	if err := a.writeJSON(filepath.Join(runtime, "selection.json"), a.Selection); err != nil {
		return err
	}
	// Keep active targets belonging to the other runtime category.
	active := []string{}
	for _, profile := range registry.Active {
		if registry.Profiles[profile].EffectiveCategory() != a.Selection.Category {
			active = append(active, profile)
		}
	}
	registry.Active = append(active, a.profiles()...)
	registry.Selection = a.Selection
	registry.Catalog = &a.Catalog
	if err := a.writeJSON(filepath.Join(runtime, "registry.json"), registry); err != nil {
		return err
	}
	return a.writeJSON(filepath.Join(a.Root, ".generated", "runtime.json"), map[string]string{"path": runtime})
}
