package operations

import (
	"os"
	"path/filepath"
	"testing"

	"paseo-sbx/internal/config"
)

func TestResolveSelectionCodexUserNSOverrides(t *testing.T) {
	for _, source := range []string{"saved", "file", "profile"} {
		for _, choice := range []string{"preserve", "off", "on"} {
			t.Run(source+"/"+choice, func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				s, err := New("../..")
				if err != nil {
					t.Fatal(err)
				}
				workspace, _ := config.Canonical(t.TempDir())
				selection := config.Selection{Schema: 2, Category: config.CategoryContainer, CodexUserNS: true, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
				o := SelectionOptions{Workspace: workspace}
				switch source {
				case "saved":
					runtime, _ := config.RuntimePath(workspace)
					if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), config.Registry{Workspace: workspace, Selection: &selection}); err != nil {
						t.Fatal(err)
					}
				case "file":
					o.Path = filepath.Join(t.TempDir(), "selection.json")
					if err := config.WriteJSON(o.Path, selection); err != nil {
						t.Fatal(err)
					}
				case "profile":
					o.Profile = "codex-superpowers"
				}
				if choice != "preserve" {
					value := choice == "on"
					o.CodexUserNS = &value
				}
				got, err := s.ResolveSelection(o)
				if err != nil {
					t.Fatal(err)
				}
				want := choice == "on" || (source != "profile" && choice == "preserve")
				if got.CodexUserNS != want {
					t.Fatalf("choice lost: %+v", got)
				}
				if source == "profile" && choice == "preserve" {
					if got.Schema != 1 || got.Category != "" {
						t.Fatalf("nil override changed legacy selection format: %+v", got)
					}
				} else if got.Schema != 2 || got.Category != config.CategoryContainer {
					t.Fatalf("explicit override lost creation format: %+v", got)
				}
			})
		}
	}
}

func TestResolveSelectionCodexUserNSOverrideRetainsDecodeValidation(t *testing.T) {
	s, err := New("../..")
	if err != nil {
		t.Fatal(err)
	}
	off := false
	for _, invalid := range []string{`{"schema":3}`, `{"schema":2,"category":"sandbox","agent":"codex","codex_userns":true}`, `{`} {
		path := filepath.Join(t.TempDir(), "selection.json")
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ResolveSelection(SelectionOptions{Path: path, CodexUserNS: &off}); err == nil {
			t.Fatalf("override bypassed decode validation for %s", invalid)
		}
	}
}
