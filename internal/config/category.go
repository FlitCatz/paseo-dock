package config

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

// Category distinguishes runtime backends without changing historical profile IDs.
type Category string

const (
	CategorySandbox       Category = "sandbox"
	CategoryContainer     Category = "container"
	CategoryLegacySandbox Category = "legacy-sandbox"
)

// SandboxAgentPath selects verified home updates before the minimal image agent.
// It is a guest-only path; never derive it from the host's PATH.
const SandboxAgentPath = "/home/agent/.local/bin:/opt/paseo-dock/runtime/bin:/opt/paseo-dock/node/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

type PortMapping struct {
	HostAddress   string `json:"host_address"`
	HostPort      int    `json:"host_port"`
	ContainerPort int    `json:"container_port"`
}

func (c Category) valid() bool {
	return c == "" || c == CategorySandbox || c == CategoryContainer || c == CategoryLegacySandbox
}
func (e Entry) EffectiveCategory() Category {
	if e.Category == "" {
		return CategoryLegacySandbox
	}
	return e.Category
}
func (e Entry) Key() string {
	switch e.EffectiveCategory() {
	case CategorySandbox:
		return "sandbox/" + e.Agent
	case CategoryContainer:
		if e.Bundle() {
			return "container/" + e.Name
		}
		return "container/" + e.Agent + "-" + e.Tool
	case CategoryLegacySandbox:
		return e.Agent + "-" + e.Tool
	default:
		return ""
	}
}

// EqualIdentity compares every saved resource and installation option. Port and
// utility order is not identity, but duplicate mappings/options remain significant.
func (e Entry) EqualIdentity(other Entry) bool {
	if e.SecurityPolicy != other.SecurityPolicy || e.SecurityPolicyDigest != other.SecurityPolicyDigest {
		return false
	}
	ep, op := e.Ports, other.Ports
	eu, ou := e.Utilities, other.Utilities
	if e.Category != other.Category || e.ContainerID != other.ContainerID || e.ImageID != other.ImageID || e.Network != other.Network || e.Agent != other.Agent || e.Tool != other.Tool || e.Workspace != other.Workspace || e.Sandbox != other.Sandbox || e.Image != other.Image {
		return false
	}
	if e.Name != other.Name || !slices.Equal(e.Pairs, other.Pairs) || !maps.Equal(e.Connections, other.Connections) {
		return false
	}
	ep = slices.Clone(ep)
	op = slices.Clone(op)
	compare := func(a, b PortMapping) int {
		if n := cmp.Compare(a.HostAddress, b.HostAddress); n != 0 {
			return n
		}
		if n := cmp.Compare(a.HostPort, b.HostPort); n != 0 {
			return n
		}
		return cmp.Compare(a.ContainerPort, b.ContainerPort)
	}
	slices.SortFunc(ep, compare)
	slices.SortFunc(op, compare)
	eu = slices.Clone(eu)
	ou = slices.Clone(ou)
	slices.Sort(eu)
	slices.Sort(ou)
	return slices.Equal(ep, op) && slices.Equal(eu, ou)
}
func (r Registry) ValidateIdentity(key string, expected Entry) error {
	actual, ok := r.Profiles[key]
	if !ok {
		return fmt.Errorf("target %s missing from registry", key)
	}
	if !actual.Category.valid() || !expected.Category.valid() {
		return fmt.Errorf("unknown target category")
	}
	if actual.Workspace != r.Workspace || !actual.EqualIdentity(expected) {
		return fmt.Errorf("confirmed target changed: %s", key)
	}
	return nil
}
func (e *Entry) UnmarshalJSON(data []byte) error {
	type plain Entry
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if !decoded.Category.valid() {
		return fmt.Errorf("unknown category %q", decoded.Category)
	}
	*e = Entry(decoded)
	return nil
}
func (r *Registry) UnmarshalJSON(data []byte) error {
	type plain Registry
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.Schema != 0 && decoded.Schema != 1 && decoded.Schema != 2 {
		return fmt.Errorf("unsupported registry schema %d", decoded.Schema)
	}
	*r = Registry(decoded)
	return nil
}
func (r Registry) MarshalJSON() ([]byte, error) {
	type plain Registry
	if r.Schema != 0 && r.Schema != 1 && r.Schema != 2 {
		return nil, fmt.Errorf("unsupported registry schema %d", r.Schema)
	}
	r.Schema = 2
	return json.Marshal(plain(r))
}
func (s Selection) validateFormat() error {
	if s.CodexUserNS {
		eligible := false
		for _, p := range s.Pairs {
			eligible = eligible || p.Agent == "codex"
		}
		if s.Schema != 2 || s.Category != CategoryContainer || !eligible {
			return fmt.Errorf("codex_userns requires a schema-2 container selection containing codex")
		}
	}
	if (s.Name != "" || len(s.Connections) > 0) && (s.Schema != 2 || s.Category != CategoryContainer) {
		return fmt.Errorf("container name and connections require a schema-2 container selection")
	}
	if s.Schema == 1 {
		if s.Category != "" || s.Agent != "" {
			return fmt.Errorf("legacy selection cannot specify category or agent")
		}
		return nil
	}
	if s.Schema != 2 {
		return fmt.Errorf("unsupported selection schema %d", s.Schema)
	}
	switch s.Category {
	case CategorySandbox:
		if s.Agent == "" || len(s.Pairs) > 0 || len(s.Utilities) > 0 {
			return fmt.Errorf("sandbox selection requires only an agent")
		}
	case CategoryContainer:
		if s.Agent != "" {
			return fmt.Errorf("container selection uses agent/methodology pairs")
		}
	default:
		return fmt.Errorf("invalid selection category %q", s.Category)
	}
	return nil
}
func (s *Selection) UnmarshalJSON(data []byte) error {
	type plain Selection
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	result := Selection(decoded)
	if err := result.validateFormat(); err != nil {
		return err
	}
	*s = result
	return nil
}
