package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
)

func TestControlCenterRegisteredComposition(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := layoutFixture()
	defer m.cancel()
	e := &m.Snapshot.Workspaces[0].Sandboxes[0].Target.Entry
	e.Name = "dev-suite"
	e.Pairs = []config.Pair{{Agent: "codex", Methodology: "openspec"}, {Agent: "claude", Methodology: "native"}}
	e.Utilities = []string{"rtk", "codegraph"}
	m.reconcileSelection()
	view := m.detailContent()
	for _, want := range []string{"COMPOSITION", "codex → openspec", "claude → native", "rtk", "codegraph"} {
		if !strings.Contains(view, want) {
			t.Errorf("registered composition missing %q: %s", want, view)
		}
	}
}

func TestControlCenterPausedLogSurvivesReflow(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := layoutFixture()
	defer m.cancel()
	l := newJobLog()
	for i := 0; i < 100; i++ {
		l.emit(operations.Event{Text: fmt.Sprintf("record-%03d %s\n", i, strings.Repeat("payload ", 14))})
	}
	m.jobs = []*job{{id: 1, state: "completed", log: l}}
	m.Focus, m.rightTab = 3, 1
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.View()
	m.logPaused = true
	// Locate a known source record in the wrapped viewport.
	m.logs.GotoTop()
	for i := 0; i < 500; i++ {
		if strings.HasPrefix(ansi.Strip(m.logs.View()), "record-030") {
			break
		}
		m.logs.ScrollDown(1)
	}
	if !strings.HasPrefix(ansi.Strip(m.logs.View()), "record-030") {
		t.Fatal("fixture anchor not found")
	}
	dashboardKey(m, "L")
	m.View()
	if !m.logPaused || !strings.HasPrefix(ansi.Strip(m.logs.View()), "record-030") {
		t.Fatalf("expanded logs lost record: %s", m.logs.View())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.View()
	if !strings.HasPrefix(ansi.Strip(m.logs.View()), "record-030") {
		t.Fatal("shrinking logs lost record")
	}
}

func TestControlCenterReviewCanInspectLongSettings(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, size := range [][2]int{{120, 40}, {110, 28}, {60, 18}} {
		m := userNSConfirmation(t, config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}})
		m.Workspace = "/workspace/" + strings.Repeat("long-project/", 14) + "mount-tail"
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		var seen strings.Builder
		m.View()
		key(m, tea.KeyHome)
		for i := 0; i < 80; i++ {
			view := m.View().Content
			assertScreenBounds(t, view, size[0], size[1])
			for _, line := range strings.Split(view, "\n") {
				seen.WriteString(strings.Join(strings.Fields(strings.Trim(line, " ║│")), ""))
			}
			key(m, tea.KeyDown)
		}
		for _, want := range []string{"mount-tail", "each Codex container", "no-new-privileges enabled"} {
			if !strings.Contains(seen.String(), strings.ReplaceAll(want, " ", "")) {
				t.Errorf("%v review hides %q", size, want)
			}
		}
		if m.Confirmed {
			t.Fatal("scroll confirmed creation")
		}
	}
}

func TestControlCenterExpandedLogsAndModalIsolation(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := layoutFixture()
	defer m.cancel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	dashboardKey(m, "L")
	if m.Focus != 3 || m.rightTab != 1 || !strings.Contains(m.View().Content, "FOLLOW") || strings.Contains(m.View().Content, "1 Workspaces") {
		t.Fatalf("L must open expanded logs: %s", m.View().Content)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !strings.Contains(m.View().Content, "1 Workspaces") {
		t.Fatal("Esc did not restore dashboard")
	}
	m.Searching = true
	dashboardKey(m, "L")
	if m.Search != "L" {
		t.Fatal("L stolen from search")
	}
	m.Searching, m.Search, m.actionMenu = false, "", true
	dashboardKey(m, "f")
	if !strings.Contains(m.View().Content, "1 Workspaces") {
		t.Fatal("modal key expanded panel")
	}
}

func TestControlCenterViewportBounds(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, size := range [][2]int{{160, 48}, {120, 40}, {110, 28}, {80, 24}, {60, 18}, {24, 8}, {8, 3}, {1, 1}} {
		m := layoutFixture()
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for focus := 0; focus < 4; focus++ {
			m.Focus = focus
			for tab := 0; tab < 4; tab++ {
				m.rightTab = tab
				for _, toggle := range []string{"", "f"} {
					if toggle != "" {
						dashboardKey(m, toggle)
					}
					assertScreenBounds(t, m.View().Content, size[0], size[1])
					if toggle != "" {
						dashboardKey(m, toggle)
					}
				}
			}
		}
		m.cancel()
	}
}

func assertScreenBounds(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		t.Fatalf("%dx%d: %d rows", width, height, len(lines))
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > width {
			t.Fatalf("%dx%d overflow: %q", width, height, line)
		}
	}
}

func TestControlCenterWideWizardSummary(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := newContainerModel(config.DefaultCatalog(), config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "openspec"}}, Utilities: []string{"rtk"}}, "/workspace/example", "2g")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := m.View().Content
	for _, want := range []string{"COMPOSITION", "codex → openspec", "rtk", "STEP 1 / 4"} {
		if !strings.Contains(view, want) {
			t.Errorf("wizard missing %q", want)
		}
	}
	assertScreenBounds(t, view, 120, 40)
}

func TestControlCenterCountsObservedStateNotRegistration(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := layoutFixture()
	defer m.cancel()
	m.Snapshot.Workspaces = m.Snapshot.Workspaces[:1]
	rows := m.Snapshot.Workspaces[0].Sandboxes
	rows[0].RuntimeState = "unknown"
	rows[0].Active = true
	view := m.overview(120)
	for _, want := range []string{"0 running · 1 stopped", "2 unknown / missing"} {
		if !strings.Contains(view, want) {
			t.Fatalf("misleading observed counts: %s", view)
		}
	}
}

func TestControlCenterColorAndPlainBoundsMatch(t *testing.T) {
	m := layoutFixture()
	defer m.cancel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	t.Setenv("NO_COLOR", "")
	colored := m.View().Content
	if !strings.Contains(colored, "\x1b[") {
		t.Fatal("missing dark theme")
	}
	assertScreenBounds(t, colored, 120, 40)
	t.Setenv("NO_COLOR", "1")
	plain := m.View().Content
	if ansi.Strip(colored) != plain {
		t.Fatal("color changed layout")
	}
}

func TestControlCenterSandboxWizardSharesFrame(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := NewModel(config.DefaultCatalog(), config.Selection{}, "/workspace/example", "2g")
	key(m, tea.KeyEnter)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := m.View().Content
	if !strings.Contains(view, "PASEO DOCK") || !strings.Contains(view, "╔") {
		t.Fatal("sandbox wizard lacks shared chrome")
	}
	assertScreenBounds(t, view, 120, 40)
}
