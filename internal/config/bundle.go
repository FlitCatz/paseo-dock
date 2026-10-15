package config

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Connection modes select how Paseo reaches one agent inside a container.
const (
	ConnectionNative = "native"
	ConnectionACP    = "acp"
	ConnectionNone   = "none"
)

type connection struct {
	modes []string
	// acp is the guest argv of the agent's ACP server; empty until pinned.
	acp []string
}

// Paseo accepts only built-in provider bases (or acp) for extends, so native
// exists only where Paseo has a built-in provider for the agent.
var connections = map[string]connection{
	"codex":           {modes: []string{ConnectionNative, ConnectionNone}},
	"claude":          {modes: []string{ConnectionNative, ConnectionNone}},
	"antigravity-cli": {modes: []string{ConnectionNone}},
}

// ConnectionModes lists the supported modes; the first one is the default.
func ConnectionModes(agent string) []string { return slices.Clone(connections[agent].modes) }

func DefaultConnection(agent string) string {
	if modes := connections[agent].modes; len(modes) > 0 {
		return modes[0]
	}
	return ConnectionNone
}

// ACPCommand returns the guest ACP server argv, or nil when the agent has none.
func ACPCommand(agent string) []string { return slices.Clone(connections[agent].acp) }

// Connection resolves an agent's effective mode; absent entries use the default.
func (e Entry) Connection(agent string) string {
	if mode, ok := e.Connections[agent]; ok {
		return mode
	}
	return DefaultConnection(agent)
}

// Bundle reports whether one container entry holds the whole selection.
func (e Entry) Bundle() bool { return e.Name != "" }

// AgentIDs lists every agent installed in the entry's runtime.
func (e Entry) AgentIDs() []string {
	if !e.Bundle() {
		return []string{e.Agent}
	}
	var out []string
	for _, p := range e.Pairs {
		if !slices.Contains(out, p.Agent) {
			out = append(out, p.Agent)
		}
	}
	slices.Sort(out)
	return out
}

// ForAgent returns the entry as seen by one of its agents. The result is for
// agent-specific commands only; identity checks must use the stored entry.
func (e Entry) ForAgent(agent string) Entry {
	if !e.Bundle() {
		return e
	}
	e.Agent, e.Tool = agent, ""
	return e
}

var bundleName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

func ValidBundleName(name string) bool { return bundleName.MatchString(name) }

// DefaultBundleName joins the sorted agents and methodologies, hashing the
// methodologies when the joined name would exceed the name limit.
func DefaultBundleName(pairs []Pair) string {
	var agents, methods []string
	for _, p := range pairs {
		if !slices.Contains(agents, p.Agent) {
			agents = append(agents, p.Agent)
		}
		if !slices.Contains(methods, p.Methodology) {
			methods = append(methods, p.Methodology)
		}
	}
	slices.Sort(agents)
	slices.Sort(methods)
	name := strings.Join(append(agents, methods...), "-")
	if ValidBundleName(name) {
		return name
	}
	var keys []string
	for _, p := range pairs {
		if key := p.Agent + "-" + p.Methodology; !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, ",")))
	return strings.Join(agents, "-") + fmt.Sprintf("-%x", sum[:4])
}

// normalizeConnections validates modes and drops defaults, so equal choices
// always have one stored form.
func normalizeConnections(pairs []Pair, requested map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for agent, mode := range requested {
		selected := slices.ContainsFunc(pairs, func(p Pair) bool { return p.Agent == agent })
		if !selected {
			return nil, fmt.Errorf("connection for unselected agent %q", agent)
		}
		if !slices.Contains(connections[agent].modes, mode) {
			return nil, fmt.Errorf("unsupported connection %q for %s (supported: %s)", mode, agent, strings.Join(connections[agent].modes, ", "))
		}
		if mode != DefaultConnection(agent) {
			out[agent] = mode
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// ValidBundle checks a stored bundle entry against the catalog it was created with.
func (c Catalog) ValidBundle(e Entry) bool {
	if !e.Bundle() || e.EffectiveCategory() != CategoryContainer || !ValidBundleName(e.Name) || len(e.Pairs) == 0 || e.Agent != "" || e.Tool != "" {
		return false
	}
	for _, p := range e.Pairs {
		pair, ok := c.Pair(p.Agent + "-" + p.Methodology)
		agent, known := c.Agent(p.Agent)
		if !ok || pair != p || !known || agent.Adapter != p.Agent || !SupportedAdapter(agent.Adapter) {
			return false
		}
	}
	for agent, mode := range e.Connections {
		if !slices.Contains(e.AgentIDs(), agent) || !slices.Contains(connections[agent].modes, mode) {
			return false
		}
	}
	return true
}

// UseBundle names a new container request with several pairs so it installs
// into one container. Saved selections without a name are not passed here:
// they keep their historical one-container-per-pair entries.
func (s *Selection) UseBundle() {
	if s.Category == CategoryContainer && s.Name == "" && len(s.Pairs) > 1 {
		s.Name = DefaultBundleName(s.Pairs)
	}
}
