package config

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

//go:embed catalog.json
var catalogJSON []byte

type Item struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Adapter   string   `json:"adapter,omitempty"`
	Installer string   `json:"installer,omitempty"`
	Agents    []string `json:"agents,omitempty"`
	Note      string   `json:"note,omitempty"`
}
type Catalog struct {
	Schema        int    `json:"schema"`
	Agents        []Item `json:"agents"`
	Methodologies []Item `json:"methodologies"`
	Utilities     []Item `json:"utilities"`
}
type Pair struct {
	Agent       string `json:"agent"`
	Methodology string `json:"methodology"`
}
type Selection struct {
	CodexUserNS bool     `json:"codex_userns,omitempty"`
	Category    Category `json:"category,omitempty"`
	Agent       string   `json:"agent,omitempty"`
	Schema      int      `json:"schema"`
	Pairs       []Pair   `json:"pairs"`
	Utilities   []string `json:"utilities"`
	// A container with Name holds every pair in one bundle entry.
	Name        string            `json:"name,omitempty"`
	Connections map[string]string `json:"connections,omitempty"`
}

func DefaultCatalog() Catalog { var c Catalog; _ = json.Unmarshal(catalogJSON, &c); return c }
func LoadCatalog(root string) (Catalog, error) {
	c := DefaultCatalog()
	if err := ReadJSON(filepath.Join(root, "catalog.json"), &c); err != nil && !os.IsNotExist(err) {
		return c, err
	}
	return c, c.Validate()
}
func (c Catalog) Validate() error {
	if c.Schema != 1 || len(c.Agents) == 0 || len(c.Methodologies) == 0 {
		return fmt.Errorf("invalid catalog")
	}
	id := regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	for _, items := range [][]Item{c.Agents, c.Methodologies, c.Utilities} {
		seen := map[string]bool{}
		for _, i := range items {
			if !id.MatchString(i.ID) || seen[i.ID] || i.Label == "" {
				return fmt.Errorf("invalid or duplicate catalog ID %q", i.ID)
			}
			seen[i.ID] = true
		}
	}
	profiles := map[string]bool{}
	for _, m := range c.Methodologies {
		for _, agent := range m.Agents {
			if _, ok := c.Agent(agent); !ok {
				return fmt.Errorf("methodology %s has unknown agent %s", m.ID, agent)
			}
			p := agent + "-" + m.ID
			if profiles[p] {
				return fmt.Errorf("ambiguous profile %s", p)
			}
			profiles[p] = true
		}
	}
	return nil
}
func find(items []Item, id string) (Item, bool) {
	for _, i := range items {
		if i.ID == id {
			return i, true
		}
	}
	return Item{}, false
}
func (c Catalog) Agent(id string) (Item, bool) { return find(c.Agents, id) }
func (c Catalog) Pair(profile string) (Pair, bool) {
	for _, a := range c.Agents {
		for _, m := range c.Methodologies {
			if a.ID+"-"+m.ID == profile && slices.Contains(m.Agents, a.ID) {
				return Pair{a.ID, m.ID}, true
			}
		}
	}
	return Pair{}, false
}
func (c Catalog) Normalize(s Selection) (Selection, error) {
	if err := s.validateFormat(); err != nil {
		return s, err
	}
	if s.Category == CategorySandbox {
		a, ok := c.Agent(s.Agent)
		if !ok || (a.Adapter != "codex" && a.Adapter != "claude") {
			return s, fmt.Errorf("unsupported agent-only adapter for %q", s.Agent)
		}
		return s, nil
	}
	if len(s.Pairs) == 0 {
		return s, fmt.Errorf("select at least one agent/methodology pair")
	}
	pairs := map[string]Pair{}
	for _, p := range s.Pairs {
		a, ok := c.Agent(p.Agent)
		if !ok || !SupportedAdapter(a.Adapter) {
			return s, fmt.Errorf("unsupported agent adapter for %q", p.Agent)
		}
		m, ok := find(c.Methodologies, p.Methodology)
		if !ok || !slices.Contains(m.Agents, p.Agent) {
			return s, fmt.Errorf("unsupported pair %s/%s", p.Agent, p.Methodology)
		}
		if m.Installer != "git-plugin" && m.Installer != "gsd-core" && m.Installer != "openspec" && m.Installer != "none" {
			return s, fmt.Errorf("unsupported methodology installer %q", m.Installer)
		}
		pairs[p.Agent+"-"+p.Methodology] = p
	}
	utilities := map[string]bool{}
	for _, id := range s.Utilities {
		u, ok := find(c.Utilities, id)
		if !ok || (u.Installer != "rtk" && u.Installer != "headroom") {
			return s, fmt.Errorf("unsupported utility %q", id)
		}
		for _, p := range s.Pairs {
			if !slices.Contains(u.Agents, p.Agent) {
				return s, fmt.Errorf("utility %s does not support %s", id, p.Agent)
			}
		}
		utilities[id] = true
	}
	s.Pairs = nil
	keys := make([]string, 0, len(pairs))
	for p := range pairs {
		keys = append(keys, p)
	}
	slices.Sort(keys)
	for _, p := range keys {
		s.Pairs = append(s.Pairs, pairs[p])
	}
	s.Utilities = []string{}
	for id := range utilities {
		s.Utilities = append(s.Utilities, id)
	}
	slices.Sort(s.Utilities)
	if s.Category != CategoryContainer {
		return s, nil
	}
	connections, err := normalizeConnections(s.Pairs, s.Connections)
	if err != nil {
		return s, err
	}
	s.Connections = connections
	// Connections exist only on new requests, which always use a bundle.
	if s.Name == "" && len(s.Connections) > 0 {
		s.Name = DefaultBundleName(s.Pairs)
	}
	if s.Name != "" && !ValidBundleName(s.Name) {
		return s, fmt.Errorf("invalid container name %q: use 1-48 lowercase letters, digits or hyphens, starting with a letter", s.Name)
	}
	return s, nil
}

// BundleEntry returns the identity fields of the selection's bundle entry.
func (s Selection) BundleEntry() Entry {
	return Entry{Category: s.Category, Name: s.Name, Pairs: slices.Clone(s.Pairs), Connections: s.Connections, Utilities: slices.Clone(s.Utilities)}
}
func (s Selection) Profiles() []string {
	if s.Category == CategoryContainer && s.Name != "" {
		return []string{s.BundleEntry().Key()}
	}
	out := make([]string, 0, len(s.Pairs))
	for _, p := range s.Pairs {
		out = append(out, Entry{Category: s.Category, Agent: p.Agent, Tool: p.Methodology}.Key())
	}
	if s.Category == CategorySandbox {
		out = append(out, Entry{Category: s.Category, Agent: s.Agent}.Key())
	}
	return out
}
func (c Catalog) AllProfiles() []string {
	var out []string
	for _, a := range c.Agents {
		for _, m := range c.Methodologies {
			if slices.Contains(m.Agents, a.ID) {
				out = append(out, a.ID+"-"+m.ID)
			}
		}
	}
	return out
}
func ImageTag(l Lock, s Selection, catalogs ...Catalog) string {
	s.CodexUserNS = false // Runtime policy does not change image contents.
	c := DefaultCatalog()
	if len(catalogs) > 0 {
		c = catalogs[0]
	}
	b, _ := json.Marshal(struct {
		Lock      Lock
		Selection Selection
		Catalog   Catalog
		Recipe    string
	}{l, s, c, "selection-utilities-v1"})
	sum := sha256.Sum256(b)
	return l.Tag + "-" + fmt.Sprintf("%x", sum[:6])
}
func (r Registry) ActiveProfiles() []string {
	if r.Active != nil {
		return r.Active
	}
	out := []string{}
	for p := range r.Profiles {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}
func (r Registry) IsActive(profile string) bool { return slices.Contains(r.ActiveProfiles(), profile) }
func (r Registry) EffectiveCatalog() Catalog {
	if r.Catalog != nil {
		return *r.Catalog
	}
	return DefaultCatalog()
}
func ParseProfiles(c Catalog, value string) (Selection, error) {
	s := Selection{Schema: 1}
	for _, id := range strings.Split(value, ",") {
		p, ok := c.Pair(strings.TrimSpace(id))
		if !ok {
			return s, fmt.Errorf("invalid profile %q", id)
		}
		s.Pairs = append(s.Pairs, p)
	}
	return s, nil
}
