package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
	"strings"
	"testing"
)

func TestWizardHidesStackForSandbox(t *testing.T) {
	m := NewModel(config.DefaultCatalog(), config.Selection{}, "/workspace", "2g")
	m.Color = false
	if !strings.Contains(m.View().Content, "Category") {
		t.Fatal("category choice missing")
	}
	key(m, tea.KeyEnter) // sandbox first
	if strings.Contains(m.View().Content, "Methodology") || strings.Contains(m.View().Content, "Utility") {
		t.Fatal("sandbox shows stack choices")
	}
	if len(m.items()) != 2 {
		t.Fatal("sandbox must offer only supported Codex and Claude")
	}
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter)
	key(m, tea.KeyEnter)
	if !m.Confirmed || m.Result.Selection.Category != config.CategorySandbox || m.Result.Selection.Agent != "codex" || len(m.Result.Selection.Pairs) != 0 || len(m.Result.Selection.Utilities) != 0 {
		t.Fatalf("invalid sandbox result: %+v", m.Result)
	}
}
func TestDashboardKeepsCategoryRowsDistinct(t *testing.T) {
	a := operations.Sandbox{Target: operations.Target{Category: config.CategorySandbox, Profile: "codex", Entry: config.Entry{Category: config.CategorySandbox, Agent: "codex"}}}
	b := operations.Sandbox{Target: operations.Target{Category: config.CategoryContainer, Profile: "codex", Entry: config.Entry{Category: config.CategoryContainer, Agent: "codex", Tool: "superpowers"}}}
	if sandboxID(a) == sandboxID(b) || sandboxID(a) != "sandbox/codex" || sandboxID(b) != "container/codex-superpowers" {
		t.Fatal("row keys lost category", sandboxID(a), sandboxID(b))
	}
}
func TestConfirmationShowsMountNetworkPorts(t *testing.T) {
	m := NewDashboard(&dashboardBackend{}, config.DefaultCatalog(), "")
	defer m.cancel()
	e := config.Entry{Category: config.CategoryContainer, Workspace: "/workspace", Network: "bridge", Ports: []config.PortMapping{{HostAddress: "127.0.0.1", HostPort: 8080, ContainerPort: 3000}}, ContainerID: "cid"}
	m.confirm = &confirmation{request: operations.Request{Kind: operations.KindStop, Target: operations.Target{Category: e.Category, Workspace: e.Workspace, Entry: e}}}
	for _, want := range []string{"container", "Mount: /workspace", "bridge", "127.0.0.1:8080", "3000", "cid"} {
		if !strings.Contains(m.managementContent(), want) {
			t.Fatalf("confirmation missing %s: %s", want, m.managementContent())
		}
	}
}

func TestFrozenRequestKeepsPortsAndStack(t *testing.T) {
	r := operations.Request{Ports: []config.PortMapping{{HostPort: 8080}}, Target: operations.Target{Entry: config.Entry{Ports: []config.PortMapping{{HostPort: 9000}}, Utilities: []string{"removed"}}}, Targets: []operations.Target{{Entry: config.Entry{Ports: []config.PortMapping{{HostPort: 3000}}}}}}
	r.RestoreTargets = []operations.Target{{Entry: config.Entry{Ports: []config.PortMapping{{HostPort: 4000}}}}}
	r.PruneTargets = []operations.Target{{Entry: config.Entry{Utilities: []string{"kept"}}}}
	frozen := freezeRequest(r)
	r.RestoreTargets[0].Entry.Ports[0].HostPort = 1
	r.PruneTargets[0].Entry.Utilities[0] = "changed"
	r.Ports[0].HostPort = 1
	r.Target.Entry.Ports[0].HostPort = 1
	r.Target.Entry.Utilities[0] = "changed"
	r.Targets[0].Entry.Ports[0].HostPort = 1
	if frozen.RestoreTargets[0].Entry.Ports[0].HostPort != 4000 || frozen.PruneTargets[0].Entry.Utilities[0] != "kept" || frozen.Ports[0].HostPort != 8080 || frozen.Target.Entry.Ports[0].HostPort != 9000 || frozen.Target.Entry.Utilities[0] != "removed" || frozen.Targets[0].Entry.Ports[0].HostPort != 3000 {
		t.Fatal("queued settings changed")
	}
}
func TestContainerCreationAcceptsExplicitLoopbackPorts(t *testing.T) {
	m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
	defer m.cancel()
	keyCmd(m, "n")
	// The third input is explicit port publication; sandbox leaves it unused.
	m.creating.path.SetValue(t.TempDir())
	keyCmd(m, "tab")
	keyCmd(m, "tab")
	for _, r := range "8080:3000" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m.creating.focus = 0
	keyCmd(m, "enter")
	if m.creating.wizard == nil {
		t.Fatal("port input blocked wizard")
	}
	child := m.creating.wizard
	key(child, tea.KeyDown)
	key(child, tea.KeyEnter)
	key(child, tea.KeySpace)
	key(child, tea.KeyEnter)
	key(child, tea.KeySpace)
	key(child, tea.KeyEnter)
	key(child, tea.KeyEnter)
	if !strings.Contains(child.View().Content, "127.0.0.1:8080") || !strings.Contains(child.View().Content, "3000") {
		t.Fatal("container confirmation lost requested ports", child.View().Content)
	}
}
func TestContainerLoadExplainsDockerStore(t *testing.T) {
	m := NewDashboard(&dashboardBackend{}, config.DefaultCatalog(), "")
	defer m.cancel()
	m.Update(savedSelectionMsg{request: operations.Request{Kind: operations.KindLoad}, selection: config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}, flow: m.flowGeneration})
	if m.confirm != nil || !strings.Contains(m.actionError, "Docker store") {
		t.Fatal("container load offered as SBX load")
	}
}

func TestWizardSavedSelectionStillStartsWithCategory(t *testing.T) {
	m := NewModel(config.DefaultCatalog(), config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}, Utilities: []string{"removed"}}, "/workspace", "2g")
	if !strings.Contains(m.View().Content, "Category ·") {
		t.Fatal("saved selection bypassed category")
	}
	key(m, tea.KeyEnter) // sandbox discards historical stack
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter)
	key(m, tea.KeyEnter)
	if !m.Confirmed || len(m.Result.Selection.Utilities) != 0 || len(m.Result.Selection.Pairs) != 0 {
		t.Fatal("sandbox inherited historical stack")
	}
}
func TestSandboxWizardFitsSmallTerminal(t *testing.T) {
	m := NewModel(config.DefaultCatalog(), config.Selection{}, "/long/workspace/path", "2g")
	key(m, tea.KeyEnter)
	key(m, tea.KeySpace)
	key(m, tea.KeyEnter)
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 4})
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) > 4 {
		t.Fatal("sandbox wizard overflow")
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > 20 {
			t.Fatal("sandbox wizard line overflow", line)
		}
	}
}
func TestLegacyRestoreUsesStoredStackAfterCatalogRemoval(t *testing.T) {
	m := NewDashboard(&dashboardBackend{}, config.DefaultCatalog(), "")
	defer m.cancel()
	w := operations.Workspace{Selection: &config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "codex", Methodology: "removed"}}, Utilities: []string{"removed"}}, Sandboxes: []operations.Sandbox{{Target: operations.Target{Profile: "codex-removed", Entry: config.Entry{Agent: "codex", Tool: "removed", Sandbox: "kept", Image: "kept-image"}}}}}
	targets, err := m.restoreTargets(&w)
	if err != nil || len(targets) != 1 {
		t.Fatal("catalog removal stranded legacy registration", err)
	}
}

func TestTinySandboxWizardKeepsFocusedAgentVisible(t *testing.T) {
	m := NewModel(config.DefaultCatalog(), config.Selection{}, "/workspace", "2g")
	key(m, tea.KeyEnter)
	key(m, tea.KeyDown)
	m.Update(tea.WindowSizeMsg{Width: 24, Height: 3})
	if !strings.Contains(m.View().Content, "› ○ Claude") {
		t.Fatal("focused Claude row clipped", m.View().Content)
	}
}
