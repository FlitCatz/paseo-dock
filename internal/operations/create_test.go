package operations

import (
	"bytes"
	"context"
	"os"
	"paseo-sbx/internal/config"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageMismatchBlocksReuse(t *testing.T) {
	s, r, path := restoreFixture(t)
	var reg config.Registry
	if err := config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	r.Selection.Pairs = []config.Pair{{Agent: "codex", Methodology: "superpowers"}}
	r.Selection.Utilities = []string{"rtk"}
	name := "paseo-codex-superpowers-" + filepath.Base(r.Target.Runtime)
	e := reg.Profiles["codex-superpowers"]
	e.Sandbox = name
	e.Image = "old-incompatible-image"
	reg.Profiles["codex-superpowers"] = e
	reg.Active = []string{"codex-superpowers"}
	if err := config.WriteJSON(path, reg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SBX_TEST_NAMES", `[{"name":"`+name+`"}]`)
	before, _ := os.ReadFile(path)
	result, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: r.Target.Workspace}, Selection: r.Selection}, nil)
	if err == nil || !strings.Contains(err.Error(), "does not match") || result.Changed {
		t.Fatalf("incompatible image/settings silently reused: %+v %v", result, err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("image mismatch changed registry")
	}
	log, _ := os.ReadFile(os.Getenv("SBX_TEST_LOG"))
	if strings.Contains(string(log), "create ") {
		t.Fatal("image mismatch ran create", string(log))
	}
}

func TestSelectionChangingCreationPreservesPreviousActivation(t *testing.T) {
	s, r, path := restoreFixture(t)
	var reg config.Registry
	if err := config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	prior := config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
	reg.Selection = &prior
	reg.Active = []string{"codex-superpowers"}
	if err := config.WriteJSON(path, reg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SBX_TEST_CREATE_FAIL", "codex-superpowers")
	result, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: r.Target.Workspace}, Selection: r.Selection}, nil)
	if err == nil || !result.Changed {
		t.Fatalf("fixture did not fail partially: %+v %v", result, err)
	}
	if err = config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	if len(reg.ActiveProfiles()) != 1 || reg.ActiveProfiles()[0] != "codex-superpowers" || reg.Selection == nil || len(reg.Selection.Pairs) != 1 || reg.Selection.Pairs[0].Agent != "codex" {
		t.Fatal("partial creation changed previous activation", reg)
	}
}

func TestCreationIncompatibleUtilitiesFailBeforeDocker(t *testing.T) {
	s, r, _ := restoreFixture(t)
	exe, _ := os.Executable()
	if err := os.Symlink(exe, filepath.Join(filepath.Dir(os.Getenv("SBX_TEST_LOG")), "docker")); err != nil {
		t.Fatal(err)
	}
	selection := config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "antigravity-cli", Methodology: "native"}}, Utilities: []string{"headroom"}}
	var events strings.Builder
	result, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: r.Target.Workspace}, Selection: selection, BuildFirst: true}, func(e Event) { events.WriteString(e.Text) })
	if err == nil || result.Changed || events.Len() != 0 {
		t.Fatalf("incompatible utility reached Docker/execution: %+v %v %s", result, err, events.String())
	}
}
