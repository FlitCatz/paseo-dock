package operations

import (
	"context"
	"os"
	"paseo-sbx/internal/config"
	"path/filepath"
	"strings"
	"testing"
)

// Recomputing the missing set after confirmation could delete unconfirmed metadata.
func TestPrunePreviewIsFrozen(t *testing.T) {
	for _, change := range []string{"newly missing", "now existing", "entry replaced", "profile added", "empty confirmation"} {
		t.Run(change, func(t *testing.T) {
			s, r, path := restoreFixture(t)
			log := os.Getenv("SBX_TEST_LOG")
			target := r.Target
			if err := os.RemoveAll(target.Workspace); err != nil {
				t.Fatal(err)
			}
			preview, err := s.Execute(context.Background(), Request{Kind: KindPrune, DryRun: true, PaseoHome: r.PaseoHome}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(preview.Missing) != 2 {
				t.Fatal("preview did not list exact missing entries", preview.Missing)
			}
			otherWorkspace, err := config.Canonical(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			otherRuntime, _ := config.RuntimePath(otherWorkspace)
			otherEntry := target.Entry
			otherEntry.Workspace = otherWorkspace
			otherEntry.Sandbox = "unconfirmed"
			otherPath := filepath.Join(otherRuntime, "registry.json")
			if err = config.WriteJSON(otherPath, config.Registry{Workspace: otherWorkspace, Profiles: map[string]config.Entry{"other": otherEntry}, Active: []string{"other"}}); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "newly missing":
				os.RemoveAll(otherWorkspace)
			case "now existing":
				os.MkdirAll(target.Workspace, 0700)
			case "entry replaced", "profile added":
				var reg config.Registry
				config.ReadJSON(path, &reg)
				e := reg.Profiles[target.Profile]
				e.Sandbox = "replacement"
				profile := target.Profile
				if change == "profile added" {
					profile = "new"
				}
				reg.Profiles[profile] = e
				config.WriteJSON(path, reg)
			case "empty confirmation":
				preview.Missing = []Target{}
			}
			result, err := s.Execute(context.Background(), Request{Kind: KindPrune, PruneTargets: preview.Missing, PaseoHome: r.PaseoHome}, nil)
			if change == "newly missing" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err = os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("confirmed missing registry not removed", err)
				}
			} else {
				if _, err = os.Stat(path); err != nil {
					t.Fatal("changed/unconfirmed registry removed", err)
				}
				if result.Changed {
					t.Fatal("invalid/empty prune reported writes", result)
				}
			}
			if _, err = os.Stat(otherPath); err != nil {
				t.Fatal("unconfirmed registry removed", err)
			}
			b, _ := os.ReadFile(log)
			if strings.Contains(string(b), "rm ") {
				t.Fatal("prune removed sandbox", string(b))
			}
		})
	}
}

func TestPruneRechecksAfterProviderSync(t *testing.T) {
	s, r, path := restoreFixture(t)
	os.RemoveAll(r.Target.Workspace)
	preview, err := s.Execute(context.Background(), Request{Kind: KindPrune, DryRun: true, PaseoHome: r.PaseoHome}, nil)
	if err != nil {
		t.Fatal(err)
	}
	restored := false
	_, err = s.Execute(context.Background(), Request{Kind: KindPrune, PruneTargets: preview.Missing, PaseoHome: r.PaseoHome}, func(e Event) {
		if !restored && e.Stage == "paseo" {
			restored = true
			if err := os.MkdirAll(r.Target.Workspace, 0700); err != nil {
				t.Fatal(err)
			}
		}
	})
	if !restored {
		t.Fatal("fixture did not recreate workspace at provider boundary")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatal("prune removed workspace metadata recreated during provider sync", statErr, err)
	}
}
