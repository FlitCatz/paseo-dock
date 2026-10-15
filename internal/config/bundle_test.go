package config

import (
	"slices"
	"strings"
	"testing"
)

func TestSinglePairKeepsHistoricalEntry(t *testing.T) {
	s, err := DefaultCatalog().Normalize(Selection{Schema: 2, Category: CategoryContainer, Pairs: []Pair{{"codex", "superpowers"}}, Connections: map[string]string{"codex": ConnectionNative}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "" || s.Connections != nil || strings.Join(s.Profiles(), ",") != "container/codex-superpowers" {
		t.Fatalf("single pair became a bundle: %+v", s)
	}
}

func TestPairsBundleIntoOneContainer(t *testing.T) {
	c := DefaultCatalog()
	request := Selection{Schema: 2, Category: CategoryContainer, Pairs: []Pair{{"codex", "superpowers"}, {"claude", "ponytail"}, {"codex", "ponytail"}}}
	// A saved selection without a name keeps one container per pair.
	saved, err := c.Normalize(request)
	if err != nil || saved.Name != "" || len(saved.Profiles()) != 3 {
		t.Fatalf("saved selection migrated: %+v %v", saved, err)
	}
	request.UseBundle()
	s, err := c.Normalize(request)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "claude-codex-ponytail-superpowers" || strings.Join(s.Profiles(), ",") != "container/claude-codex-ponytail-superpowers" {
		t.Fatalf("unexpected bundle: %+v", s)
	}
	e := s.BundleEntry()
	if !e.Bundle() || !c.ValidBundle(e) || strings.Join(e.AgentIDs(), ",") != "claude,codex" {
		t.Fatalf("invalid bundle entry: %+v", e)
	}
	view := e.ForAgent("codex")
	if view.Agent != "codex" || view.Tool != "" || view.Key() != e.Key() || view.EqualIdentity(e) {
		t.Fatalf("agent view: %+v", view)
	}
	other := e
	other.Pairs = other.Pairs[1:]
	if other.EqualIdentity(e) {
		t.Fatal("pairs are not identity")
	}
	other = e
	other.Connections = map[string]string{"codex": ConnectionNone}
	if other.EqualIdentity(e) {
		t.Fatal("connections are not identity")
	}
}

func TestBundleNameAndConnectionValidation(t *testing.T) {
	c := DefaultCatalog()
	named, err := c.Normalize(Selection{Schema: 2, Category: CategoryContainer, Name: "dev", Pairs: []Pair{{"codex", "superpowers"}}})
	if err != nil || named.Name != "dev" || named.Profiles()[0] != "container/dev" {
		t.Fatalf("explicit name: %+v %v", named, err)
	}
	connected, err := c.Normalize(Selection{Schema: 2, Category: CategoryContainer, Pairs: []Pair{{"claude", "superpowers"}}, Connections: map[string]string{"claude": ConnectionNone}})
	if err != nil || connected.Name != "claude-superpowers" || connected.Connections["claude"] != ConnectionNone {
		t.Fatalf("non-default connection: %+v %v", connected, err)
	}
	for _, s := range []Selection{
		{Schema: 2, Category: CategoryContainer, Name: "Bad_Name", Pairs: []Pair{{"codex", "superpowers"}}},
		{Schema: 2, Category: CategoryContainer, Pairs: []Pair{{"codex", "superpowers"}}, Connections: map[string]string{"codex": "bogus"}},
		{Schema: 2, Category: CategoryContainer, Pairs: []Pair{{"gjc", "native"}}, Connections: map[string]string{"gjc": ConnectionNative}},
		{Schema: 2, Category: CategoryContainer, Pairs: []Pair{{"antigravity-cli", "native"}}, Connections: map[string]string{"antigravity-cli": ConnectionACP}},
		{Schema: 2, Category: CategoryContainer, Pairs: []Pair{{"codex", "superpowers"}}, Connections: map[string]string{"claude": ConnectionNone}},
		{Schema: 2, Category: CategorySandbox, Agent: "codex", Name: "dev"},
	} {
		if _, err := c.Normalize(s); err == nil {
			t.Fatalf("accepted %+v", s)
		}
	}
}

func TestDefaultBundleNameStaysWithinLimit(t *testing.T) {
	var pairs []Pair
	for _, agent := range []string{"antigravity-cli", "claude", "codex"} {
		for _, method := range []string{"gsd-core", "openspec", "ponytail", "superpowers"} {
			pairs = append(pairs, Pair{agent, method})
		}
	}
	name := DefaultBundleName(pairs)
	reversed := append([]Pair{}, pairs...)
	slices.Reverse(reversed)
	if DefaultBundleName(append(reversed, pairs[0])) != name {
		t.Fatal("name depends on pair order")
	}
	if !ValidBundleName(name) || !strings.HasPrefix(name, "antigravity-cli-claude-codex-") || name == DefaultBundleName(pairs[:4]) {
		t.Fatal(name)
	}
}

func TestDefaultConnections(t *testing.T) {
	legacy := Entry{Category: CategoryContainer, Agent: "antigravity-cli", Tool: "native"}
	if legacy.Connection("antigravity-cli") != ConnectionNone || (Entry{}).Connection("codex") != ConnectionNative || (Entry{}).Connection("omp") != ConnectionNative || (Entry{}).Connection("gjc") != ConnectionACP || ACPCommand("antigravity-cli") != nil || strings.Join(ACPCommand("omp"), " ") != "omp acp" {
		t.Fatal("unexpected default connection")
	}
}

func TestOmpAndGjcAreContainerAgents(t *testing.T) {
	c := DefaultCatalog()
	for _, agent := range []string{"omp", "gjc"} {
		s, err := c.Normalize(Selection{Schema: 2, Category: CategoryContainer, Pairs: []Pair{{agent, "native"}}})
		if err != nil || s.Name != "" || s.Profiles()[0] != "container/"+agent+"-native" {
			t.Fatal(agent, s, err)
		}
		if _, err := c.Normalize(Selection{Schema: 2, Category: CategoryContainer, Pairs: []Pair{{agent, "superpowers"}}}); err == nil {
			t.Fatal("unverified methodology accepted for", agent)
		}
		if _, err := c.Normalize(Selection{Schema: 2, Category: CategorySandbox, Agent: agent}); err == nil {
			t.Fatal("agent-only sandbox accepted for", agent)
		}
	}
	for _, p := range c.AllProfiles() {
		if strings.HasPrefix(p, "omp-") || strings.HasPrefix(p, "gjc-") {
			t.Fatal("legacy SBX profile", p)
		}
	}
}
