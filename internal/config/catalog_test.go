package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSelectionOnlyChosenPairs(t *testing.T) {
	c, err := LoadCatalog("../..")
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.Normalize(Selection{Schema: 1, Pairs: []Pair{{"codex", "ponytail"}, {"codex", "superpowers"}, {"codex", "ponytail"}}, Utilities: []string{"rtk", "rtk"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Profiles(), []string{"codex-ponytail", "codex-superpowers"}) || !reflect.DeepEqual(s.Utilities, []string{"rtk"}) {
		t.Fatal(s)
	}
}
func TestSelectionRejectsUnsupportedInput(t *testing.T) {
	c, err := LoadCatalog("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []Selection{{Schema: 1}, {Schema: 2, Pairs: []Pair{{"codex", "superpowers"}}}, {Schema: 1, Pairs: []Pair{{"other", "superpowers"}}}, {Schema: 1, Pairs: []Pair{{"codex", "other"}}}, {Schema: 1, Pairs: []Pair{{"codex", "superpowers"}}, Utilities: []string{"other"}}} {
		if _, err := c.Normalize(s); err == nil {
			t.Fatalf("accepted %+v", s)
		}
	}
	c.Methodologies[0].Agents = []string{"claude"}
	if _, err := c.Normalize(Selection{Schema: 1, Pairs: []Pair{{"codex", c.Methodologies[0].ID}}}); err == nil {
		t.Fatal("accepted incompatible methodology")
	}
}
func TestImageFingerprintIncludesUtilityVersions(t *testing.T) {
	l, err := Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	s := Selection{Schema: 1, Pairs: []Pair{{"codex", "superpowers"}}, Utilities: []string{"rtk"}}
	before := ImageTag(l, s)
	l.Utilities["rtk"] = json.RawMessage(`{"version":"999"}`)
	if before == ImageTag(l, s) {
		t.Fatal("utility change reused image")
	}
	if ImageTag(l, s) == ImageTag(l, Selection{Schema: 1, Pairs: s.Pairs}) {
		t.Fatal("selection ignored")
	}
}
