package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLegacyRegistryRoundTrip(t *testing.T) {
	raw := `{"workspace":"/work","profiles":{"codex-superpowers":{"agent":"codex","tool":"superpowers","workspace":"/work","sandbox":"old","image":"old-image"}},"selection":{"schema":1,"pairs":[{"agent":"codex","methodology":"superpowers"}],"utilities":[]}}`
	var r Registry
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	e := r.Profiles["codex-superpowers"]
	if e.EffectiveCategory() != CategoryLegacySandbox || e.Key() != "codex-superpowers" {
		t.Fatalf("legacy identity: %+v", e)
	}
	r.Active = []string{}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"category"`) {
		t.Fatalf("injected legacy category: %s", b)
	}
	if !strings.Contains(string(b), `"schema":2`) {
		t.Fatalf("new registry write lacks schema 2: %s", b)
	}
	var reread Registry
	if err := json.Unmarshal(b, &reread); err != nil {
		t.Fatal(err)
	}
	if !e.EqualIdentity(reread.Profiles["codex-superpowers"]) {
		t.Fatal("legacy entry changed")
	}
	for _, invalid := range []string{`{"schema":99}`, `{"profiles":{"x":{"category":"typo"}}}`} {
		if json.Unmarshal([]byte(invalid), &r) == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
}

func TestCategoryKeysDoNotCollide(t *testing.T) {
	for _, tc := range []struct {
		e   Entry
		key string
	}{
		{Entry{Agent: "codex", Tool: "superpowers"}, "codex-superpowers"},
		{Entry{Category: CategorySandbox, Agent: "codex"}, "sandbox/codex"},
		{Entry{Category: CategoryContainer, Agent: "codex", Tool: "superpowers"}, "container/codex-superpowers"},
	} {
		if got := tc.e.Key(); got != tc.key {
			t.Fatalf("key = %q, want %q", got, tc.key)
		}
	}
	if (Entry{Category: "typo", Agent: "codex", Tool: "superpowers"}).Key() != "" {
		t.Fatal("unknown category produced a legacy key")
	}
}

func TestEntryIdentityIgnoresOptionOrdering(t *testing.T) {
	e := Entry{Ports: []PortMapping{{HostAddress: "127.0.0.1", HostPort: 3000, ContainerPort: 3000}, {HostAddress: "127.0.0.1", HostPort: 4000, ContainerPort: 4000}}, Utilities: []string{"rtk", "headroom"}}
	other := Entry{Ports: []PortMapping{e.Ports[1], e.Ports[0]}, Utilities: []string{"headroom", "rtk"}}
	if !e.EqualIdentity(other) {
		t.Fatal("ordering changed resource identity")
	}
	if e.Ports[0].HostPort != 3000 || e.Utilities[0] != "rtk" {
		t.Fatal("comparison mutated entry")
	}
	other.Ports[0].HostAddress = "0.0.0.0"
	if e.EqualIdentity(other) {
		t.Fatal("changed port address accepted")
	}
}

func TestRegistryIdentityRejectsChangedCategoryAndID(t *testing.T) {
	original := Entry{Category: CategoryContainer, Agent: "codex", Tool: "superpowers", Workspace: "/work", ContainerID: "id", Image: "image", ImageID: "sha256:image", Network: "bridge", Ports: []PortMapping{{HostAddress: "127.0.0.1", HostPort: 3000, ContainerPort: 3000}}, Utilities: []string{"rtk"}}
	r := Registry{Workspace: "/work", Profiles: map[string]Entry{"container/codex-superpowers": original}}
	if err := r.ValidateIdentity("container/codex-superpowers", original); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Entry){
		"category": func(e *Entry) { e.Category = CategorySandbox }, "id": func(e *Entry) { e.ContainerID = "other" }, "image": func(e *Entry) { e.Image = "other" }, "image-id": func(e *Entry) { e.ImageID = "other" }, "mount": func(e *Entry) { e.Workspace = "/other" }, "port": func(e *Entry) {
			e.Ports = []PortMapping{{HostAddress: "127.0.0.1", HostPort: 3001, ContainerPort: 3000}}
		}, "network": func(e *Entry) { e.Network = "host" }, "utility": func(e *Entry) { e.Utilities = []string{"headroom"} },
	} {
		t.Run(name, func(t *testing.T) {
			changed := original
			mutate(&changed)
			r.Profiles["container/codex-superpowers"] = changed
			if r.ValidateIdentity("container/codex-superpowers", original) == nil {
				t.Fatal("accepted changed target")
			}
		})
	}
	if r.ValidateIdentity("missing", original) == nil {
		t.Fatal("accepted missing target")
	}
}

func TestCategorySelectionCompatibility(t *testing.T) {
	c := DefaultCatalog()
	s, err := c.Normalize(Selection{Schema: 2, Category: CategorySandbox, Agent: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Schema != 2 || len(s.Pairs) != 0 || len(s.Utilities) != 0 || len(s.Profiles()) != 1 || s.Profiles()[0] != "sandbox/codex" {
		t.Fatalf("sandbox selection: %+v", s)
	}
	container, err := c.Normalize(Selection{Schema: 2, Category: CategoryContainer, Pairs: []Pair{{Agent: "codex", Methodology: "superpowers"}}})
	if err != nil || container.Profiles()[0] != "container/codex-superpowers" {
		t.Fatalf("container: %+v %v", container, err)
	}
	for _, invalid := range []Selection{{Schema: 99}, {Schema: 2, Category: "typo", Agent: "codex"}, {Schema: 2, Category: CategorySandbox, Agent: "codex", Utilities: []string{"rtk"}}, {Schema: 2, Category: CategorySandbox, Agent: "codex", Pairs: []Pair{{Agent: "codex", Methodology: "superpowers"}}}} {
		if _, err := c.Normalize(invalid); err == nil {
			t.Fatalf("accepted %+v", invalid)
		}
	}
	var legacy Selection
	if err := json.Unmarshal([]byte(`{"schema":1,"pairs":[{"agent":"codex","methodology":"superpowers"}]}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Normalize(legacy); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"schema":99}`, `{"schema":2,"category":"typo","agent":"codex"}`} {
		if json.Unmarshal([]byte(raw), &legacy) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
