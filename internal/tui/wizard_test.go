package tui

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"paseo-sbx/internal/config"
	"strings"
	"testing"
)

func key(m *Model, code rune) { m.Update(tea.KeyPressMsg(tea.Key{Code: code})) }
func TestWizardOnlyChosenCombinations(t *testing.T) {
	m := NewModel(config.DefaultCatalog(), config.Selection{}, "/workspace", "2g")
	key(m, tea.KeyEnter)
	if m.Step != 0 || m.Error == "" {
		t.Fatal("allowed empty agents")
	}
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter) // Codex only
	key(m, tea.KeySpace)
	key(m, tea.KeyDown)
	key(m, tea.KeyDown)
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter) // Superpowers + Ponytail
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter) // RTK only
	if !strings.Contains(m.View().Content, "codex-superpowers") || strings.Contains(m.View().Content, "claude-superpowers") {
		t.Fatal(m.View().Content)
	}
	key(m, tea.KeyEnter)
	if !m.Confirmed || len(m.Result.Selection.Pairs) != 2 || len(m.Result.Selection.Utilities) != 1 || m.Result.Selection.Utilities[0] != "rtk" {
		t.Fatal(m.Result)
	}
}
func TestWizardCancellationAndBack(t *testing.T) {
	m := NewModel(config.DefaultCatalog(), config.Selection{}, "/workspace", "2g")
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter)
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter)
	key(m, tea.KeyEscape)
	if m.Step != 1 {
		t.Fatal(m.Step)
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if !errors.Is(m.Err, ErrCancelled) || m.Confirmed {
		t.Fatal(m.Err)
	}
}
func TestWizardPerAgentMethodsAndHeadroomDisclosure(t *testing.T) {
	m := NewModel(config.DefaultCatalog(), config.Selection{}, "/workspace", "2g")
	key(m, tea.KeySpace)
	key(m, tea.KeyDown)
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter)
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter) // Codex Superpowers
	key(m, tea.KeyDown)
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter) // Claude GSD Core
	key(m, tea.KeyDown)
	key(m, tea.KeySpace)
	if !strings.Contains(m.View().Content, "Python") {
		t.Fatal(m.View().Content)
	}
	key(m, tea.KeyEnter)
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter)
	if !m.Result.Apply || len(m.Result.Selection.Pairs) != 2 {
		t.Fatal(m.Result)
	}
	if m.Result.Selection.Profiles()[0] != "claude-gsd-core" || m.Result.Selection.Profiles()[1] != "codex-superpowers" {
		t.Fatal(m.Result.Selection)
	}
}
func TestIncompatibleUtilityCanBeDeselected(t *testing.T) {
	c := config.DefaultCatalog()
	c.Utilities[0].Agents = []string{"codex"}
	m := NewModel(c, config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}, {Agent: "claude", Methodology: "superpowers"}}, Utilities: []string{"rtk"}}, "/workspace", "2g")
	key(m, tea.KeyEnter)
	key(m, tea.KeyEnter)
	key(m, tea.KeyEnter)
	if m.Step != 2 {
		t.Fatal(m.Step)
	}
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter)
	if m.Step != 3 || len(m.Result.Selection.Utilities) != 0 {
		t.Fatalf("could not clear incompatible selection: %s", m.View().Content)
	}
}

func TestWizardAntigravityNative(t *testing.T) {
	m := NewModel(config.DefaultCatalog(), config.Selection{}, "/workspace", "2g")
	key(m, tea.KeyDown)
	key(m, tea.KeyDown)
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter)
	if len(m.items()) != 2 || m.items()[1].ID != "native" {
		t.Fatal(m.items())
	}
	key(m, tea.KeyDown)
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter)
	key(m, tea.KeySpace) // RTK is unavailable for Antigravity.
	if m.Error == "" || m.utilities["rtk"] {
		t.Fatal("unsupported utility selectable")
	}
	key(m, tea.KeyEnter)
	key(m, tea.KeyEnter)
	if !m.Confirmed || strings.Join(m.Result.Selection.Profiles(), ",") != "antigravity-cli-native" {
		t.Fatal(m.Result)
	}
}
