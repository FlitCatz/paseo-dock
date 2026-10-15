package cli

import (
	"fmt"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
	"path/filepath"
	"slices"
	"strings"
)

func (a *App) resolveSelection(workspace, profile, profiles, utilities, path, agents, methods string, setUtilities bool) (config.Selection, error) {
	return a.Service.ResolveSelection(operations.SelectionOptions{Workspace: workspace, Profile: profile, Profiles: profiles, Utilities: utilities, Path: path, Agents: agents, Methods: methods, SetUtilities: setUtilities})
}
func readSavedSelection(runtime string) (config.Selection, error) {
	return operations.ReadSavedSelection(runtime)
}

// Creation converts historical pair syntax at the CLI boundary. Existing-target
// management reads registry entries directly and never normalizes their stack.
func (a *App) creationSelection(category config.Category, agent, workspace, profile, profiles, utilities, path, agents, methods string, setUtilities bool, override *bool, name, connections string) (config.Selection, error) {
	if category == config.CategorySandbox && path == "" {
		if agent == "" {
			return config.Selection{}, fmt.Errorf("--category sandbox requires --agent codex|claude")
		}
		if profile != "" || profiles != "" || agents != "" || methods != "" || utilities != "" {
			return config.Selection{}, fmt.Errorf("sandbox accepts only --agent, without methodology or utilities")
		}
		s := config.Selection{Schema: 2, Category: category, Agent: agent}
		if override != nil {
			s.CodexUserNS = *override
		}
		return a.Catalog.Normalize(s)
	}
	if agent != "" {
		return config.Selection{}, fmt.Errorf("--agent requires --category sandbox for creation")
	}
	s, err := a.Service.ResolveSelection(operations.SelectionOptions{Workspace: workspace, Profile: profile, Profiles: profiles, Utilities: utilities, Path: path, Agents: agents, Methods: methods, SetUtilities: setUtilities, CodexUserNS: override})
	if err != nil {
		return s, err
	}
	if category != "" && s.Schema == 2 && category != s.Category {
		return s, fmt.Errorf("selection category does not match --category")
	}
	if s.Schema == 1 {
		s.Schema = 2
		s.Category = config.CategoryContainer
	}
	if category != "" && s.Category != category {
		return s, fmt.Errorf("selection category does not match --category")
	}
	if err := applyBundleOptions(&s, name, connections); err != nil {
		return s, err
	}
	// Only an explicit new choice bundles; a reused saved selection keeps its entries.
	if profile != "" || profiles != "" || path != "" || agents != "" || methods != "" || name != "" || connections != "" {
		s.UseBundle()
	}
	return a.Catalog.Normalize(s)
}

func categoryMatches(want config.Category, e config.Entry) bool {
	return want == "" || want == e.EffectiveCategory() || (want == config.CategorySandbox && e.EffectiveCategory() == config.CategoryLegacySandbox)
}
func (a *App) registryTargets(workspace, profile string, category config.Category, single bool) (config.Registry, []operations.Target, error) {
	runtime, err := a.selectedRuntime(workspace)
	if err != nil {
		return config.Registry{}, nil, err
	}
	var reg config.Registry
	if err = config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
		return reg, nil, err
	}
	keys := make([]string, 0)
	for key, e := range reg.Profiles {
		if !categoryMatches(category, e) {
			continue
		}
		if profile != "" && profile != key && profile != e.Agent && profile != e.Agent+"-"+e.Tool && (e.Name == "" || profile != e.Name) {
			continue
		}
		if profile == "" && !single && !reg.IsActive(key) {
			continue
		}
		keys = append(keys, key)
	}
	slices.Sort(keys)
	categories := map[config.Category]bool{}
	var targets []operations.Target
	for _, key := range keys {
		e := reg.Profiles[key]
		categories[e.EffectiveCategory()] = true
		targets = append(targets, operations.Target{Category: e.EffectiveCategory(), Workspace: reg.Workspace, Runtime: runtime, Profile: key, Entry: e})
	}
	if (len(categories) > 1 && category == "") || (single && len(targets) > 1) {
		return reg, nil, fmt.Errorf("ambiguous runtime categories or targets; specify --category and an exact target")
	}
	if (profile != "" || single) && len(targets) == 0 {
		return reg, nil, fmt.Errorf("target missing from registry for requested category")
	}
	return reg, targets, nil
}

// applyBundleOptions sets the container name and per-agent connections; both
// apply only to container selections and are validated by Normalize.
func applyBundleOptions(s *config.Selection, name, connections string) error {
	if name != "" {
		s.Name = name
	}
	if connections == "" {
		return nil
	}
	s.Connections = map[string]string{}
	for _, item := range strings.Split(connections, ",") {
		agent, mode, ok := strings.Cut(strings.TrimSpace(item), "=")
		if !ok || agent == "" || mode == "" {
			return fmt.Errorf("--connections requires agent=mode items, got %q", item)
		}
		if _, dup := s.Connections[agent]; dup {
			return fmt.Errorf("duplicate connection for %s", agent)
		}
		s.Connections[agent] = mode
	}
	return nil
}
