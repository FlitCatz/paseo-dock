package operations

import (
	"context"
	"fmt"
	"paseo-sbx/internal/config"
	"path/filepath"
	"strings"
)

type SelectionOptions struct {
	Workspace, Profile, Profiles, Utilities, Path, Agents, Methods string
	SetUtilities                                                   bool
}

func (s *Service) ResolveSelection(o SelectionOptions) (config.Selection, error) {
	return s.execution(context.Background(), nil, nil).resolveSelection(o.Workspace, o.Profile, o.Profiles, o.Utilities, o.Path, o.Agents, o.Methods, o.SetUtilities)
}
func (s *Service) SavedSelection(workspace string) (config.Selection, error) {
	runtime, err := s.SelectedRuntime(workspace)
	if err != nil {
		return config.Selection{}, err
	}
	return readSavedSelection(runtime)
}
func ReadSavedSelection(runtime string) (config.Selection, error) { return readSavedSelection(runtime) }
func joinProfiles(s config.Selection) string                      { return strings.Join(s.Profiles(), ",") }
func joinUtilities(s config.Selection) string                     { return strings.Join(s.Utilities, ",") }
func (a *execution) resolveSelection(workspace, profile, profiles, utilities, path, agents, methods string, setUtilities bool) (config.Selection, error) {
	s := config.Selection{Schema: 1}
	sources := 0
	for _, v := range []string{profile, profiles, path, agents + methods} {
		if v != "" {
			sources++
		}
	}
	if sources > 1 {
		return s, fmt.Errorf("use only one of --profile, --profiles, --selection, or --agents/--methodologies")
	}
	var err error
	switch {
	case profile != "" || profiles != "":
		if profile != "" {
			profiles = profile
		}
		s, err = config.ParseProfiles(a.Catalog, profiles)
	case path != "":
		err = config.ReadJSON(path, &s)
	case agents != "" || methods != "":
		if agents == "" || methods == "" {
			return s, fmt.Errorf("--agents and --methodologies must be supplied together")
		}
		for _, agent := range strings.Split(agents, ",") {
			for _, method := range strings.Split(methods, ",") {
				s.Pairs = append(s.Pairs, config.Pair{Agent: strings.TrimSpace(agent), Methodology: strings.TrimSpace(method)})
			}
		}
	default:
		var runtime string
		runtime, err = a.selectedRuntime(workspace)
		if err == nil {
			s, err = readSavedSelection(runtime)
		}
		if err != nil {
			return s, fmt.Errorf("no saved selection; specify --profiles or --agents with --methodologies (interactive selection requires a terminal)")
		}
	}
	if err != nil {
		return s, err
	}
	if setUtilities {
		s.Utilities = []string{}
		if utilities != "" {
			for _, u := range strings.Split(utilities, ",") {
				s.Utilities = append(s.Utilities, strings.TrimSpace(u))
			}
		}
	}
	s, err = a.Catalog.Normalize(s)
	if err != nil {
		return s, err
	}
	for _, p := range s.Pairs {
		if a.Lock.BaseImages[p.Agent] == "" || a.Lock.Agents[p.Agent] == "" || a.Lock.Tools[p.Methodology] == nil {
			return s, fmt.Errorf("missing pinned versions for %s/%s", p.Agent, p.Methodology)
		}
		item, _ := a.Catalog.Agent(p.Agent)
		if item.Adapter == "antigravity-cli" && a.Lock.AgentArtifacts[p.Agent] == nil {
			return s, fmt.Errorf("missing pinned Antigravity CLI artifacts for %s", p.Agent)
		}
	}
	for _, u := range s.Utilities {
		if a.Lock.Utilities[u] == nil {
			return s, fmt.Errorf("missing pinned utility %s", u)
		}
	}
	return s, nil
}
func readSavedSelection(runtime string) (config.Selection, error) {
	var reg config.Registry
	if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
		return config.Selection{}, err
	}
	if reg.Selection == nil {
		return config.Selection{}, fmt.Errorf("no completed saved selection")
	}
	return *reg.Selection, nil
}
