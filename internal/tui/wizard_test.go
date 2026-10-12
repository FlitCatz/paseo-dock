package tui

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"github.com/charmbracelet/x/ansi"
	"paseo-sbx/internal/config"
	"strings"
	"testing"
)

func key(m *Model, code rune) { m.Update(tea.KeyPressMsg(tea.Key{Code: code})) }
func TestEmbeddedWizardReturnsToDashboard(t *testing.T) {
	for _, embedded := range []bool{true, false} {
		for _, cancel := range []bool{true, false} {
			var m *Model
			if embedded {
				m = newEmbeddedContainerModel(config.DefaultCatalog(), config.Selection{}, "/workspace", "2g")
			} else {
				m = newContainerModel(config.DefaultCatalog(), config.Selection{}, "/workspace", "2g")
			}
			code := rune(tea.KeyEscape)
			if !cancel {
				key(m, tea.KeySpace)
				key(m, tea.KeyEnter)
				key(m, tea.KeySpace)
				key(m, tea.KeyEnter)
				key(m, tea.KeyEnter)
				code = tea.KeyEnter
			}
			if cancel {
				key(m, tea.KeyEscape)
			}
			_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: code}))
			if cmd == nil {
				t.Fatal("completion missing")
			}
			msg := cmd()
			if embedded {
				result, ok := msg.(WizardFinishedMsg)
				if !ok {
					t.Fatalf("child terminated parent instead of returning: %T", msg)
				}
				if cancel && !errors.Is(result.Err, ErrCancelled) {
					t.Fatal("cancel missing", result.Err)
				}
				if !cancel && (result.Err != nil || len(result.Result.Selection.Pairs) != 1) {
					t.Fatal("result missing", result)
				}
			} else if _, ok := msg.(tea.QuitMsg); !ok {
				t.Fatalf("standalone completion changed: %T", msg)
			}
		}
	}
}
func TestWizardOnlyChosenCombinations(t *testing.T) {
	m := newContainerModel(config.DefaultCatalog(), config.Selection{}, "/workspace", "2g")
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
	m := newContainerModel(config.DefaultCatalog(), config.Selection{}, "/workspace", "2g")
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
	m := newContainerModel(config.DefaultCatalog(), config.Selection{}, "/workspace", "2g")
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
	if m.Result.Selection.Profiles()[0] != "container/claude-gsd-core" || m.Result.Selection.Profiles()[1] != "container/codex-superpowers" {
		t.Fatal(m.Result.Selection)
	}
}
func TestIncompatibleUtilityCanBeDeselected(t *testing.T) {
	c := config.DefaultCatalog()
	c.Utilities[0].Agents = []string{"codex"}
	m := newContainerModel(c, config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}, {Agent: "claude", Methodology: "superpowers"}}, Utilities: []string{"rtk"}}, "/workspace", "2g")
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
	m := newContainerModel(config.DefaultCatalog(), config.Selection{}, "/workspace", "2g")
	key(m, tea.KeyDown)
	key(m, tea.KeyDown)
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter)
	if len(m.items()) != 5 || m.items()[1].ID != "gsd-core" || m.items()[2].ID != "ponytail" || m.items()[3].ID != "openspec" || m.items()[4].ID != "native" {
		t.Fatal(m.items())
	}
	key(m, tea.KeyDown)
	key(m, tea.KeyDown)
	key(m, tea.KeyDown)
	key(m, tea.KeyDown)
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter)
	key(m, tea.KeySpace) // RTK instructions are available for Antigravity.
	if m.Error != "" || !m.utilities["rtk"] {
		t.Fatal("RTK unavailable")
	}
	key(m, tea.KeyDown)
	key(m, tea.KeySpace) // Headroom integration is not configured for Antigravity.
	if m.Error == "" || m.utilities["headroom"] {
		t.Fatal("unconfigured utility selectable")
	}
	key(m, tea.KeyEnter)
	key(m, tea.KeyEnter)
	if !m.Confirmed || strings.Join(m.Result.Selection.Profiles(), ",") != "container/antigravity-cli-native" || strings.Join(m.Result.Selection.Utilities, ",") != "rtk" {
		t.Fatal(m.Result)
	}
}

func TestContinuousFormNavigation(t *testing.T) {
	m := newContainerModel(config.DefaultCatalog(), config.Selection{}, "/한글 project", "2g")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !strings.Contains(m.View().Content, "Headroom") {
		t.Fatal("24-line terminal does not show all initial choices")
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	for _, label := range []string{"1  Agent", "2  Methodology", "3  Utility", "Superpowers", "GSD Core", "Ponytail", "OpenSpec"} {
		if !strings.Contains(m.View().Content, label) {
			t.Fatalf("missing visible choice %q", label)
		}
	}
	key(m, 'x')
	key(m, tea.KeyDown)
	key(m, 'x')
	key(m, tea.KeyTab)
	for _, label := range []string{"Methodology · Codex", "Methodology · Claude", "3  Utility"} {
		if !strings.Contains(m.View().Content, label) {
			t.Fatalf("form lost group %q: %s", label, m.View().Content)
		}
	}
	for range 3 {
		key(m, tea.KeyDown)
	}
	key(m, 'x') // Codex OpenSpec
	key(m, tea.KeyTab)
	key(m, 'x') // Claude Superpowers
	key(m, tea.KeyTab)
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift}))
	if m.Step != 1 || m.agentIndex != 1 || !m.pairs["codex-openspec"] || !m.pairs["claude-superpowers"] {
		t.Fatal("going back lost choices")
	}
	key(m, tea.KeyTab)
	key(m, tea.KeyTab)
	key(m, tea.KeyTab)
	if m.Step != 3 || m.Confirmed {
		t.Fatal("Tab confirmed resource creation")
	}
	key(m, tea.KeyEnter)
	if !m.Confirmed || strings.Join(m.Result.Selection.Profiles(), ",") != "container/claude-superpowers,container/codex-openspec" {
		t.Fatal(m.Result)
	}
}

func TestFormColorAndSmallTerminal(t *testing.T) {
	m := newContainerModel(config.DefaultCatalog(), config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "antigravity-cli", Methodology: "openspec"}}}, "/한글 project", "2g")
	m.Color = true
	if !strings.Contains(m.View().Content, "\x1b[") {
		t.Fatal("missing color")
	}
	key(m, tea.KeyTab)
	for range 3 {
		key(m, tea.KeyDown)
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 44, Height: 14}, {Width: 24, Height: 6}, {Width: 24, Height: 1}} {
		m.Update(size)
		view := m.View().Content
		lines := strings.Split(view, "\n")
		if len(lines) > size.Height {
			t.Fatalf("overflow: %s", view)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size.Width {
				t.Fatalf("wide line: %q", line)
			}
		}
		if !strings.Contains(ansi.Strip(view), "OpenSpec") {
			t.Fatalf("active choice hidden: %s", view)
		}
	}
	t.Setenv("NO_COLOR", "1")
	plain := newContainerModel(config.DefaultCatalog(), config.Selection{}, "/workspace", "2g").View().Content
	if strings.Contains(plain, "\x1b[") {
		t.Fatal("NO_COLOR ignored")
	}
}

func newContainerModel(c config.Catalog, s config.Selection, w, memory string) *Model {
	m := NewModel(c, s, w, memory)
	if m.Step == -1 {
		key(m, tea.KeyDown)
		key(m, tea.KeyEnter)
	}
	return m
}
func newEmbeddedContainerModel(c config.Catalog, s config.Selection, w, memory string) *Model {
	m := newContainerModel(c, s, w, memory)
	m.embedded = true
	return m
}
