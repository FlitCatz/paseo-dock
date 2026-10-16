package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"paseo-dock/internal/config"
)

func userNSConfirmation(t *testing.T, s config.Selection) *Model {
	t.Helper()
	m := newEmbeddedContainerModel(config.DefaultCatalog(), s, "/workspace", "2g")
	m.Color = false
	key(m, tea.KeyEnter)
	key(m, tea.KeyEnter)
	key(m, tea.KeyEnter)
	if m.Step != 3 {
		t.Fatalf("not at confirmation: %d / %s", m.Step, m.Error)
	}
	return m
}

func TestWizardCodexUserNSDefaultToggleAndResult(t *testing.T) {
	for _, saved := range []bool{false, true} {
		t.Run(map[bool]string{false: "default-off", true: "saved-on"}[saved], func(t *testing.T) {
			s := config.Selection{Schema: 2, Category: config.CategoryContainer, CodexUserNS: saved, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
			m := userNSConfirmation(t, s)
			m.Ports = []config.PortMapping{{HostAddress: "127.0.0.1", HostPort: 8080, ContainerPort: 3000}}
			for _, want := range []string{"u:", "workspace-write", "all guest processes", "kernel syscall access", "no added capabilities"} {
				if !strings.Contains(m.View().Content, want) {
					t.Fatalf("confirmation omits %q: %s", want, m.View().Content)
				}
			}
			if m.selection().CodexUserNS != saved {
				t.Fatal("saved choice lost")
			}
			key(m, 'u')
			if m.selection().CodexUserNS == saved {
				t.Fatal("u did not toggle")
			}
			_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			finished := cmd().(WizardFinishedMsg)
			if !m.Confirmed || finished.Result.Selection.CodexUserNS == saved || len(finished.Result.Ports) != 1 || finished.Result.Ports[0].HostPort != 8080 {
				t.Fatalf("choice/ports lost at completion: %+v", finished.Result)
			}
		})
	}
}

func TestWizardCodexUserNSOnlyEligibleConfirmation(t *testing.T) {
	for _, agent := range []string{"codex", "claude"} {
		m := newContainerModel(config.DefaultCatalog(), config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: agent, Methodology: "superpowers"}}}, "/workspace", "2g")
		key(m, 'u')
		if m.selection().CodexUserNS {
			t.Fatal("toggle acted before confirmation")
		}
		key(m, tea.KeyEnter)
		key(m, tea.KeyEnter)
		key(m, tea.KeyEnter)
		if strings.Contains(m.View().Content, "u:") != (agent == "codex") {
			t.Fatalf("wrong eligibility for %s", agent)
		}
		key(m, 'u')
		key(m, tea.KeyEnter)
		if m.Result.Selection.CodexUserNS != (agent == "codex") {
			t.Fatalf("wrong toggle result for %s", agent)
		}
	}
	m := NewModel(config.DefaultCatalog(), config.Selection{}, "/workspace", "2g")
	key(m, tea.KeyEnter)
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter)
	key(m, 'u')
	key(m, tea.KeyEnter)
	if !m.Confirmed || m.Result.Selection.CodexUserNS || strings.Contains(m.View().Content, "u:") {
		t.Fatal("sandbox offered namespace policy")
	}
}

func TestWizardCodexUserNSClearsOnCategoryOrAgentChange(t *testing.T) {
	s := config.Selection{Schema: 2, Category: config.CategoryContainer, CodexUserNS: true, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}, {Agent: "claude", Methodology: "superpowers"}}}
	t.Run("category", func(t *testing.T) {
		m := NewModel(config.DefaultCatalog(), s, "/workspace", "2g")
		key(m, tea.KeyEnter) // sandbox clears saved container choice
		key(m, tea.KeySpace)
		key(m, tea.KeyEnter)
		key(m, tea.KeyEnter)
		if !m.Confirmed || m.Result.Selection.CodexUserNS {
			t.Fatal("category change retained policy")
		}
		key(m, tea.KeyEscape)
		key(m, tea.KeyEscape)
		key(m, tea.KeyDown)
		key(m, tea.KeyEnter)
		if m.selection().CodexUserNS {
			t.Fatal("returning to container silently enabled policy")
		}
	})
	t.Run("agent", func(t *testing.T) {
		m := newContainerModel(config.DefaultCatalog(), s, "/workspace", "2g")
		key(m, tea.KeySpace) // remove Codex, keep Claude
		key(m, tea.KeyEnter)
		key(m, tea.KeyEnter)
		key(m, tea.KeyEnter)
		key(m, tea.KeyEnter)
		if !m.Confirmed || m.Result.Selection.CodexUserNS || m.Result.Selection.Pairs[0].Agent != "claude" {
			t.Fatalf("removing Codex retained policy: %+v / %s", m.Result, m.Error)
		}
		key(m, tea.KeyEscape)
		key(m, tea.KeyEscape)
		key(m, tea.KeyEscape)
		key(m, tea.KeySpace)
		if m.selection().CodexUserNS {
			t.Fatal("reselecting Codex silently enabled policy")
		}
	})
}

func TestDashboardCodexUserNSReachesFrozenCreateRequest(t *testing.T) {
	b := &jobBackend{}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	keyCmd(m, "n")
	m.creating.path.SetValue(t.TempDir())
	keyCmd(m, "enter")
	child := m.creating.wizard
	key(child, tea.KeyDown)
	key(child, tea.KeyEnter)
	key(child, tea.KeySpace)
	key(child, tea.KeyEnter)
	key(child, tea.KeySpace)
	key(child, tea.KeyEnter)
	key(child, tea.KeyEnter)
	keyCmd(m, "u")
	finished := keyCmd(m, "enter")()
	child.Result.Selection.CodexUserNS = false
	_, dispatch := m.Update(finished)
	runJobCommand(m, dispatch)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.requests) != 1 || !b.requests[0].Selection.CodexUserNS {
		t.Fatal("dashboard lost frozen opt-in", b.requests)
	}
}
