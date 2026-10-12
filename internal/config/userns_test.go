package config

import (
	"encoding/json"
	"testing"
)

func TestUserNSSelectionFormat(t *testing.T) {
	for _, input := range []string{
		`{"schema":1,"pairs":[{"agent":"codex","methodology":"native"}],"codex_userns":true}`,
		`{"schema":2,"category":"sandbox","agent":"codex","codex_userns":true}`,
		`{"schema":2,"category":"container","pairs":[{"agent":"claude","methodology":"native"}],"codex_userns":true}`,
	} {
		var s Selection
		if err := json.Unmarshal([]byte(input), &s); err == nil {
			t.Errorf("accepted invalid opt-in: %s", input)
		}
	}
}
func TestUserNSPolicyIdentity(t *testing.T) {
	var a, b Entry
	_ = json.Unmarshal([]byte(`{"agent":"codex","security_policy":"codex-userns-v1","security_policy_digest":"digest"}`), &a)
	_ = json.Unmarshal([]byte(`{"agent":"codex"}`), &b)
	if a.EqualIdentity(b) {
		t.Fatal("policyless target adopted policy identity")
	}
}
func TestUserNSRuntimeDoesNotChangeImage(t *testing.T) {
	s := Selection{Schema: 2, Category: CategoryContainer, Pairs: []Pair{{Agent: "codex", Methodology: "superpowers"}}}
	before := ImageTag(Lock{Tag: "test"}, s)
	s.CodexUserNS = true
	if got := ImageTag(Lock{Tag: "test"}, s); got != before {
		t.Fatalf("runtime opt-in changed image: %s %s", before, got)
	}
	n, err := DefaultCatalog().Normalize(s)
	if err != nil || !n.CodexUserNS {
		t.Fatalf("lost opt-in: %+v %v", n, err)
	}
	s.Pairs = append(s.Pairs, Pair{Agent: "claude", Methodology: "superpowers"})
	if _, err := DefaultCatalog().Normalize(s); err != nil {
		t.Fatal(err)
	}
}
