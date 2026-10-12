package operations

import (
	"bytes"
	"context"
	"os"
	"paseo-sbx/internal/config"
	"path/filepath"
	"slices"
	"testing"
)

// Ignoring explicit restoration strands a completed workspace after unregister.
func TestRestoreRegistration(t *testing.T) {
	s, targets, home := providerScopeFixture(t)
	loaded, err := New("../..")
	if err != nil {
		t.Fatal(err)
	}
	s.Catalog, s.Lock = loaded.Catalog, loaded.Lock
	selection := config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}, {Agent: "claude", Methodology: "superpowers"}}}
	var reg config.Registry
	path := filepath.Join(targets[0].Runtime, "registry.json")
	if err = config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	for i := range targets {
		delete(reg.Profiles, targets[i].Profile)
		targets[i].Profile = selection.Pairs[i].Agent + "-superpowers"
		targets[i].Entry.Tool = "superpowers"
		reg.Profiles[targets[i].Profile] = targets[i].Entry
	}
	reg.Active = []string{"codex-superpowers", "claude-superpowers"}
	reg.Selection = &selection
	if err = config.WriteJSON(path, reg); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Execute(context.Background(), Request{Kind: KindUnregister, Target: targets[0], Targets: targets, PaseoHome: home}, nil); err != nil {
		t.Fatal(err)
	}
	result, err := s.Execute(context.Background(), Request{Kind: KindRegister, Target: targets[0], Targets: []Target{}, RestoreTargets: targets, Selection: selection, RestoreSelection: true, Apply: true, PaseoHome: home}, nil)
	if err != nil {
		t.Fatalf("complete saved selection was not restored: %v", err)
	}
	if !result.Changed {
		t.Fatal("restoration reported no changes")
	}
	if err = config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	if len(reg.ActiveProfiles()) != 2 {
		t.Fatal("restored scope incomplete", reg.ActiveProfiles())
	}
	providers, err := readProviders(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := providers["codex-superpowers"]; !ok {
		t.Fatal("restored provider absent")
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func restoreFixture(t *testing.T) (*Service, Request, string) {
	t.Helper()
	s, targets, home := providerScopeFixture(t)
	loaded, err := New("../..")
	if err != nil {
		t.Fatal(err)
	}
	s.Catalog, s.Lock = loaded.Catalog, loaded.Lock
	selection := config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}, {Agent: "claude", Methodology: "superpowers"}}}
	var reg config.Registry
	path := filepath.Join(targets[0].Runtime, "registry.json")
	if err = config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	for i := range targets {
		delete(reg.Profiles, targets[i].Profile)
		targets[i].Profile = selection.Pairs[i].Agent + "-superpowers"
		targets[i].Entry.Tool = "superpowers"
		reg.Profiles[targets[i].Profile] = targets[i].Entry
	}
	reg.Active = []string{}
	reg.Selection = &selection
	if err = config.WriteJSON(path, reg); err != nil {
		t.Fatal(err)
	}
	return s, Request{Kind: KindRegister, Target: targets[0], Targets: []Target{}, RestoreTargets: targets, Selection: selection, RestoreSelection: true, Apply: true, PaseoHome: home}, path
}

func TestRestoreRejectsChangedConfirmedScope(t *testing.T) {
	for _, change := range []string{"desired entry", "saved selection", "prior active", "missing selection", "missing entry", "missing identity", "partial desired", "adapter", "image", "legacy"} {
		t.Run(change, func(t *testing.T) {
			s, r, path := restoreFixture(t)
			var reg config.Registry
			if err := config.ReadJSON(path, &reg); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "desired entry":
				e := reg.Profiles[r.RestoreTargets[1].Profile]
				e.Sandbox = "replacement"
				reg.Profiles[r.RestoreTargets[1].Profile] = e
			case "saved selection":
				reg.Selection.Utilities = []string{"rtk"}
			case "prior active":
				reg.Active = []string{r.Target.Profile}
			case "missing selection":
				reg.Selection = nil
			case "missing entry":
				delete(reg.Profiles, r.RestoreTargets[1].Profile)
			case "missing identity":
				r.RestoreTargets[1].Entry = config.Entry{}
			case "partial desired":
				r.RestoreTargets = r.RestoreTargets[:1]
			case "adapter":
				e := reg.Profiles[r.RestoreTargets[1].Profile]
				e.Agent = "codex"
				reg.Profiles[r.RestoreTargets[1].Profile] = e
				r.RestoreTargets[1].Entry = e
			case "image":
				e := reg.Profiles[r.RestoreTargets[1].Profile]
				e.Image = ""
				reg.Profiles[r.RestoreTargets[1].Profile] = e
				r.RestoreTargets[1].Entry = e
			case "legacy":
				r.RestoreSelection = false
			}
			if err := config.WriteJSON(path, reg); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			providerBefore, _ := os.ReadFile(filepath.Join(r.PaseoHome, "config.json"))
			result, err := s.Execute(context.Background(), r, nil)
			if err == nil || result.Changed {
				t.Fatalf("invalid restoration wrote state: %+v %v", result, err)
			}
			after, _ := os.ReadFile(path)
			providerAfter, _ := os.ReadFile(filepath.Join(r.PaseoHome, "config.json"))
			if !bytes.Equal(before, after) || !bytes.Equal(providerBefore, providerAfter) {
				t.Fatal("rejected restore changed activation/provider")
			}
		})
	}
}

func TestRestorePreservesSharedWorkspaceProvider(t *testing.T) {
	s, r, path := restoreFixture(t)
	otherWorkspace, err := config.Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := config.RuntimePath(otherWorkspace)
	e := r.RestoreTargets[1].Entry
	e.Workspace = otherWorkspace
	e.Sandbox = "other"
	if err = config.WriteJSON(filepath.Join(runtime, "registry.json"), config.Registry{Workspace: otherWorkspace, Active: []string{"claude-superpowers"}, Profiles: map[string]config.Entry{"claude-superpowers": e}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Execute(context.Background(), r, nil); err != nil {
		t.Fatal(err)
	}
	var reg config.Registry
	if err = config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(reg.ActiveProfiles(), []string{"claude-superpowers", "codex-superpowers"}) {
		t.Fatal("restoration scope wrong", reg.ActiveProfiles())
	}
	r.Targets = slices.Clone(r.RestoreTargets)
	r.Kind = KindUnregister
	r.RestoreSelection = false
	if _, err = s.Execute(context.Background(), r, nil); err != nil {
		t.Fatal(err)
	}
	providers, err := readProviders(r.PaseoHome)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"claude-superpowers", "user-provider"} {
		if _, ok := providers[profile]; !ok {
			t.Fatal("removed shared/custom provider", profile)
		}
	}
}

func TestLegacyRestoreUsesPersistedCatalogAfterHostUtilityRemoval(t *testing.T) {
	for _, change := range []string{"none", "saved selection", "entry"} {
		t.Run(change, func(t *testing.T) {
			s, r, path := restoreFixture(t)
			var reg config.Registry
			if err := config.ReadJSON(path, &reg); err != nil {
				t.Fatal(err)
			}
			historical := config.DefaultCatalog()
			reg.Catalog = &historical
			reg.Selection.Utilities = []string{"rtk"}
			r.Selection.Utilities = []string{"rtk"}
			for i := range r.RestoreTargets {
				r.RestoreTargets[i].Entry.Utilities = []string{"rtk"}
				reg.Profiles[r.RestoreTargets[i].Profile] = r.RestoreTargets[i].Entry
			}
			r.Target = r.RestoreTargets[0]
			if change == "saved selection" {
				reg.Selection.Utilities = []string{}
			}
			if change == "entry" {
				e := reg.Profiles[r.RestoreTargets[1].Profile]
				e.Image = "replacement"
				reg.Profiles[r.RestoreTargets[1].Profile] = e
			}
			if err := config.WriteJSON(path, reg); err != nil {
				t.Fatal(err)
			}
			s.Catalog.Utilities = nil
			s.Lock.Utilities = nil
			_, err := s.Execute(context.Background(), r, nil)
			if change == "none" && err != nil {
				t.Fatal("host catalog removal stranded saved legacy restore", err)
			}
			if change != "none" && err == nil {
				t.Fatal("changed stored restoration accepted")
			}
		})
	}
}
