package operations

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"paseo-sbx/internal/config"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var disabledTools = []string{"create_workspace", "list_workspaces", "archive_workspace", "rename_workspace", "list_workspace_scripts", "start_workspace_script", "stop_workspace_script", "list_terminals", "create_terminal", "kill_terminal", "capture_terminal", "send_terminal_keys", "create_schedule", "create_heartbeat", "delete_heartbeat", "list_schedules", "inspect_schedule", "pause_schedule", "resume_schedule", "delete_schedule", "update_schedule", "schedule_logs", "run_schedule_once", "speak", "archive_agent", "kill_agent", "update_agent", "set_agent_mode", "list_pending_permissions", "respond_to_permission", "list_profiles", "inspect_provider"}

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
func ownedCommand(command string, base string) bool {
	if command == config.LauncherPath(base) {
		return true
	}
	return filepath.Base(command) == "agent-wrapper" && filepath.Dir(filepath.Dir(command)) == base
}
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

// validateProviderScope runs under Execute's existing runtime/home locks, before
// any registry, launcher, backup or provider write. Explicit targets describe
// the complete confirmed active set; a nil set keeps legacy CLI resolution.
func (s *Service) validateProviderScope(target Target, targets []Target) error {
	runtime := target.Runtime
	if runtime == "" {
		var err error
		runtime, err = s.SelectedRuntime(target.Workspace)
		if err != nil {
			return err
		}
	}
	runtime, err := config.Canonical(runtime)
	if err != nil {
		return err
	}
	var reg config.Registry
	if err = config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
		return err
	}
	if err = validateObservedRegistry(reg, runtime); err != nil {
		return err
	}
	if target.Workspace != "" {
		workspace, err := config.Canonical(target.Workspace)
		if err != nil {
			return err
		}
		if workspace != reg.Workspace {
			return errors.New("confirmed provider workspace changed")
		}
	}
	confirmed := make([]string, 0, len(targets))
	seen := make(map[string]bool, len(targets))
	for _, t := range targets {
		if t.Entry == (config.Entry{}) || seen[t.Profile] {
			return errors.New("invalid confirmed provider scope")
		}
		seen[t.Profile] = true
		validated, err := s.resolveTarget(t)
		if err != nil {
			return err
		}
		if validated.Runtime != runtime || validated.Workspace != reg.Workspace {
			return errors.New("confirmed provider workspace changed")
		}
		confirmed = append(confirmed, validated.Profile)
	}
	active := slices.Clone(reg.ActiveProfiles())
	slices.Sort(active)
	slices.Sort(confirmed)
	if !slices.Equal(active, confirmed) {
		return errors.New("confirmed workspace activation changed")
	}
	return nil
}

func (a *execution) register(runtime, home string, apply bool) error {
	var reg config.Registry
	if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
		return err
	}
	if len(reg.ActiveProfiles()) == 0 {
		return errors.New("no complete active selection; finish create before registering")
	}
	return a.syncProviders(home, apply)
}

// Restore is opt-in and requires both the prior active scope and the complete
// desired saved selection to remain identical to what the dashboard confirmed.
func (a *execution) restoreRegistration(runtime string, r Request) error {
	if r.Targets == nil || r.RestoreTargets == nil || a.Selection == nil {
		return errors.New("restore requires confirmed prior scope and saved selection entries")
	}
	var reg config.Registry
	path := filepath.Join(runtime, "registry.json")
	if err := config.ReadJSON(path, &reg); err != nil {
		return err
	}
	if err := validateObservedRegistry(reg, runtime); err != nil {
		return err
	}
	if reg.Selection == nil {
		return errors.New("no completed saved selection")
	}
	saved, err := a.Catalog.Normalize(*reg.Selection)
	if err != nil {
		return err
	}
	if saved.Schema != a.Selection.Schema || !slices.Equal(saved.Pairs, a.Selection.Pairs) || !slices.Equal(saved.Utilities, a.Selection.Utilities) {
		return errors.New("confirmed saved selection changed")
	}
	desired := saved.Profiles()
	if len(r.RestoreTargets) != len(desired) {
		return errors.New("incomplete confirmed saved selection entries")
	}
	seen := map[string]bool{}
	for _, target := range r.RestoreTargets {
		if target.Entry == (config.Entry{}) || seen[target.Profile] || !slices.Contains(desired, target.Profile) {
			return errors.New("invalid confirmed restore scope")
		}
		seen[target.Profile] = true
		validated, err := a.Service.resolveTarget(target)
		if err != nil {
			return err
		}
		if validated.Runtime != runtime || validated.Workspace != reg.Workspace {
			return errors.New("confirmed restore workspace changed")
		}
		pair, ok := a.Catalog.Pair(target.Profile)
		if !ok {
			return errors.New("invalid saved profile")
		}
		adapter, ok := a.Catalog.Agent(pair.Agent)
		if !ok || target.Entry.Agent != adapter.Adapter || target.Entry.Tool != pair.Methodology || target.Entry.Image == "" {
			return errors.New("incomplete or incompatible saved profile entry")
		}
	}
	reg.Active = desired
	return a.writeJSON(path, reg)
}
func (a *execution) unregister(workspace, home string) error {
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
	if err = a.writeJSON(path, reg); err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "Unregistered workspace:", w)
	return a.syncProviders(home, true)
}
func (a *execution) syncProviders(home string, apply bool) error {
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
	if err := a.writeJSON(path, generated); err != nil {
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
	if err = a.writeJSON(path, current); err != nil {
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
