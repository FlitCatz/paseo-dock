package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/gateway"
	"paseo-sbx/internal/operationlock"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var disabledTools = []string{"create_workspace", "list_workspaces", "archive_workspace", "rename_workspace", "list_workspace_scripts", "start_workspace_script", "stop_workspace_script", "list_terminals", "create_terminal", "kill_terminal", "capture_terminal", "send_terminal_keys", "create_schedule", "create_heartbeat", "delete_heartbeat", "list_schedules", "inspect_schedule", "pause_schedule", "resume_schedule", "delete_schedule", "update_schedule", "schedule_logs", "run_schedule_once", "speak", "archive_agent", "kill_agent", "update_agent", "set_agent_mode", "list_pending_permissions", "respond_to_permission", "list_profiles", "inspect_provider"}

func providerID(profile string, e config.Entry) string {
	switch e.EffectiveCategory() {
	case config.CategorySandbox:
		return "sbx-" + e.Agent
	case config.CategoryContainer:
		return "container-" + e.Agent + "-" + e.Tool
	default:
		return profile
	}
}

// ProviderID derives routing solely from the trusted stored entry category.
func ProviderID(profile string, e config.Entry) string { return providerID(profile, e) }

func (a *execution) checkDockerRelays(installed []config.Installed) error {
	for _, i := range installed {
		for _, key := range i.Registry.ActiveProfiles() {
			e := i.Registry.Profiles[key]
			if e.EffectiveCategory() != config.CategoryContainer {
				continue
			}
			if err := i.Registry.ValidateIdentity(key, e); err != nil {
				return err
			}
			if err := a.checkDockerRelay(i.Runtime, key, e); err != nil {
				return err
			}
		}
	}
	return nil
}

// Validate the provider union before a proposed activation becomes persistent.
func (a *execution) checkProspectiveDockerRelays(runtime string, reg config.Registry) error {
	base, err := config.BaseDir()
	if err != nil {
		return err
	}
	installed, err := config.Registries(base)
	if err != nil {
		return err
	}
	live := []config.Installed{{Runtime: runtime, Registry: reg}}
	for _, i := range installed {
		if i.Runtime != runtime && !i.Missing() {
			live = append(live, i)
		}
	}
	return a.checkDockerRelays(live)
}

func (a *execution) checkDockerRelay(runtime, key string, e config.Entry) error {
	release, err := operationlock.Acquire(a.ctx, runtime, []string{operationlock.EntryKey(e)})
	if err != nil {
		return err
	}
	defer release()
	var current config.Registry
	if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &current); err != nil {
		return err
	}
	if err := current.ValidateIdentity(key, e); err != nil {
		return err
	}
	backend, err := a.backend(e.EffectiveCategory())
	if err != nil {
		return err
	}
	relay, err := gateway.NewDockerRelay(nil, func(endpoint string) error {
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		var out bytes.Buffer
		if err := backend.Exec(ctx, e, gateway.DockerProbeCommand(endpoint), nil, &out, a.Err); err != nil {
			return err
		}
		if out.String() != gateway.RelayReady {
			return errors.New("unexpected Docker relay challenge response")
		}
		return nil
	})
	if err != nil {
		return err
	}
	defer relay.Close()
	return relay.CheckReachability()
}

// ProviderTarget is one Paseo provider of a registry entry.
type ProviderTarget struct {
	ID, Agent, Mode, Label string
}

// ProviderTargets lists one provider ID per agent of an entry, including agents
// connected as none (the launcher still serves them for paseo-sbx run).
// Single-agent entries keep their historical provider ID.
func ProviderTargets(profile string, e config.Entry) []ProviderTarget {
	if !e.Bundle() {
		mode := config.ConnectionNative
		if e.EffectiveCategory() == config.CategoryContainer {
			mode = e.Connection(e.Agent)
		}
		id := providerID(profile, e)
		return []ProviderTarget{{ID: id, Agent: e.Agent, Mode: mode, Label: "SBX " + id}}
	}
	var out []ProviderTarget
	for _, agent := range e.AgentIDs() {
		mode := e.Connection(agent)
		out = append(out, ProviderTarget{ID: "container-" + e.Name + "-" + agent, Agent: agent, Mode: mode, Label: "SBX " + e.Name + " · " + agent})
	}
	return out
}

func ActiveProviders(installed []config.Installed) map[string]ProviderTarget {
	targets := map[string]ProviderTarget{}
	for _, i := range installed {
		for _, p := range i.Registry.ActiveProfiles() {
			if e, ok := i.Registry.Profiles[p]; ok {
				for _, t := range ProviderTargets(p, e) {
					if t.Mode == config.ConnectionNone || slices.Contains(i.Registry.DisabledProviders, t.ID) {
						continue
					}
					targets[t.ID] = t
				}
			}
		}
	}
	return targets
}

func ActiveAdapters(installed []config.Installed) map[string]string {
	adapters := map[string]string{}
	for id, t := range ActiveProviders(installed) {
		adapters[id] = t.Agent
	}
	return adapters
}

// ProviderConfig generates native-mode providers keyed by provider ID.
func ProviderConfig(launcher string, adapters map[string]string) map[string]any {
	targets := map[string]ProviderTarget{}
	for id, agent := range adapters {
		targets[id] = ProviderTarget{ID: id, Agent: agent, Mode: config.ConnectionNative, Label: "SBX " + id}
	}
	return ProviderConfigTargets(launcher, targets)
}

func ProviderConfigTargets(launcher string, targets map[string]ProviderTarget) map[string]any {
	providers := map[string]any{}
	for p, t := range targets {
		agent := t.Agent
		switch t.Mode {
		case config.ConnectionNone:
			continue
		case config.ConnectionACP:
			providers[p] = map[string]any{"extends": "acp", "label": t.Label, "enabled": true, "command": []string{launcher, p, "--acp"}, "paseoTools": map[string]any{"enabled": true, "disabledTools": disabledTools}}
			continue
		}
		if agent == "antigravity-cli" && !strings.HasPrefix(p, "container-") && !strings.HasPrefix(p, "sbx-") {
			// The existing plugin owns this provider. Per-agent options select SBX.
			continue
		}
		command := []string{launcher, p}
		if agent == "antigravity-cli" {
			command = append(command, "--acp")
		}
		provider := map[string]any{"extends": config.ProviderAdapter(agent), "label": t.Label, "enabled": true, "command": command, "paseoTools": map[string]any{"enabled": true, "disabledTools": disabledTools}}
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
		// ACP providers of any agent carry a trailing --acp; ownership still
		// requires the trusted launcher and a matching provider ID.
		managed := len(command) == 2 || (len(command) == 3 && command[2] == "--acp")
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
		if t.Entry.EqualIdentity(config.Entry{}) || seen[t.Profile] {
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
	// A failed ACP check disables only that agent's provider: the rest of the
	// workspace still registers, and the failure is reported afterwards.
	var acpErrs []error
	disabled := slices.Clone(reg.DisabledProviders)
	for _, key := range reg.ActiveProfiles() {
		entry := reg.Profiles[key]
		if entry.EffectiveCategory() != config.CategoryContainer {
			continue
		}
		backend, err := a.backend(entry.EffectiveCategory())
		if err != nil {
			return err
		}
		acpErrs = append(acpErrs, a.recordACP(backend, &reg, key, entry))
	}
	if !slices.Equal(disabled, reg.DisabledProviders) {
		if err := a.writeJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
			return err
		}
	}
	if err := a.syncProviders(home, apply, true); err != nil {
		return err
	}
	return errors.Join(acpErrs...)
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
	historical := reg.EffectiveCatalog()
	saved, err := historical.Normalize(*reg.Selection)
	if err != nil {
		return err
	}
	confirmed, err := historical.Normalize(*a.Selection)
	if err != nil {
		return err
	}
	if saved.Schema != confirmed.Schema || saved.Category != confirmed.Category || saved.Agent != confirmed.Agent || !slices.Equal(saved.Pairs, confirmed.Pairs) || !slices.Equal(saved.Utilities, confirmed.Utilities) || saved.Name != confirmed.Name || !maps.Equal(saved.Connections, confirmed.Connections) {
		return errors.New("confirmed saved selection changed")
	}
	desired := saved.Profiles()
	if len(r.RestoreTargets) != len(desired) {
		return errors.New("incomplete confirmed saved selection entries")
	}
	seen := map[string]bool{}
	for _, target := range r.RestoreTargets {
		if target.Entry.EqualIdentity(config.Entry{}) || seen[target.Profile] || !slices.Contains(desired, target.Profile) {
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
		if saved.Category == config.CategorySandbox {
			if target.Entry.Category != saved.Category || target.Entry.Agent != saved.Agent || target.Entry.Tool != "" || len(target.Entry.Utilities) != 0 || target.Entry.Image == "" {
				return errors.New("incomplete or incompatible sandbox entry")
			}
			continue
		}
		if saved.Name != "" {
			bundle := saved.BundleEntry()
			if !target.Entry.Bundle() || target.Profile != bundle.Key() || !historical.ValidBundle(target.Entry) || target.Entry.Name != bundle.Name || !slices.Equal(target.Entry.Pairs, bundle.Pairs) || !maps.Equal(target.Entry.Connections, bundle.Connections) || target.Entry.Image == "" {
				return errors.New("incomplete or incompatible saved bundle entry")
			}
			continue
		}
		profile := strings.TrimPrefix(target.Profile, "container/")
		pair, ok := historical.Pair(profile)
		if !ok {
			return errors.New("invalid saved profile")
		}
		adapter, ok := historical.Agent(pair.Agent)
		if !ok || target.Entry.Agent != adapter.Adapter || target.Entry.Tool != pair.Methodology || target.Entry.Image == "" {
			return errors.New("incomplete or incompatible saved profile entry")
		}
	}
	reg.Active = desired
	if err := a.checkProspectiveDockerRelays(runtime, reg); err != nil {
		return err
	}
	return a.writeJSON(path, reg)
}
func (a *execution) unregister(workspace, home string, categories ...config.Category) error {
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
	category := config.Category("")
	if len(categories) > 0 {
		category = categories[0]
	}
	active := reg.ActiveProfiles()
	reg.Active = []string{}
	for _, key := range active {
		entry := reg.Profiles[key]
		matches := category == "" || category == entry.EffectiveCategory() || (category == config.CategorySandbox && entry.EffectiveCategory() == config.CategoryLegacySandbox)
		if !matches {
			reg.Active = append(reg.Active, key)
		}
	}
	if err = a.writeJSON(path, reg); err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "Unregistered workspace:", w)
	return a.syncProviders(home, true)
}
func (a *execution) syncProviders(home string, apply bool, verifyRelays ...bool) error {
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
	if len(verifyRelays) > 0 && verifyRelays[0] {
		if err = a.checkDockerRelays(live); err != nil {
			return err
		}
	}
	if err = a.installLauncher(base, workspaces); err != nil {
		return err
	}
	generated := ProviderConfigTargets(config.LauncherPath(base), ActiveProviders(live))
	if err = a.filterProviderBases(generated); err != nil {
		return err
	}
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
	backup := ""
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
		backup = path + ".backup-" + time.Now().UTC().Format("20060102T150405.000000000Z")
		if err = os.WriteFile(backup, original, 0600); err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := os.Chmod(backup, info.Mode().Perm()); err != nil {
			return err
		}
		fmt.Fprintln(a.Out, "Existing configuration backed up:", backup)
	}
	MergeManagedConfig(current, generated, base)
	if err = a.writeJSON(path, current); err != nil {
		return err
	}
	if err = a.validatePaseoConfig(home); err != nil {
		var restoreErr error
		if backup != "" {
			restoreErr = os.Rename(backup, path)
		} else {
			restoreErr = os.Remove(path)
		}
		if restoreErr != nil {
			return errors.Join(fmt.Errorf("Paseo config validation failed: %w", err), fmt.Errorf("restore original config (backup %q): %w", backup, restoreErr))
		}
		return fmt.Errorf("Paseo config validation failed; original config restored: %w", err)
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
