package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
)

func layoutFixture() *Dashboard {
	m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
	s := fixtureSnapshot()
	s.Workspaces[0].Sandboxes[0].Target.Entry.Category = config.CategoryContainer
	s.Workspaces[0].Sandboxes[0].Target.Entry.Agent = "codex"
	s.Workspaces[0].Sandboxes[0].Target.Entry.Tool = "gsd"
	s.Workspaces[0].Sandboxes[0].Active = true
	s.Workspaces[0].Sandboxes[1].Target.Entry.Category = config.CategorySandbox
	s.Workspaces[0].Sandboxes[1].Target.Entry.Agent = "claude"
	s.Workspaces[0].Sandboxes = append(s.Workspaces[0].Sandboxes, operations.Sandbox{Target: operations.Target{Profile: "legacy-core", Entry: config.Entry{Agent: "legacy", Tool: "core"}}, RuntimeState: "missing"})
	s.Workspaces[1].Missing = true
	s.TemplateRows = []operations.Template{{Repository: "paseo-dock/codex", Tag: "stable", ID: "sha256:template"}}
	s.Templates = operations.Connection{Status: "connected", CheckedAt: time.Now()}
	deliver(m, s)
	return m
}

func TestLazydockerLayoutAndBounds(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 20}} {
		m := layoutFixture()
		defer m.cancel()
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View().Content
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("%v overflow: %q", size, line)
			}
		}
		if size[0] >= 80 {
			for _, want := range []string{"1 Workspaces", "2 Sandbox / Container", "3 Templates", "[상세]", "로그", "버전", "설정", "●", "○", "✕", " C", " S", " L", "1/3", "(missing)"} {
				if !strings.Contains(view, want) {
					t.Errorf("%v missing %q:\n%s", size, want, view)
				}
			}
			for _, pair := range [][2]string{{"●", " C "}, {"○", " S "}, {"✕", " L "}} {
				found := false
				for _, line := range strings.Split(view, "\n") {
					if strings.Contains(line, pair[0]) && strings.Contains(line, pair[1]) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%v lost category suffix for %s", size, pair[0])
				}
			}
		} else if !strings.Contains(view, "1/4 Workspaces") || strings.Contains(view, "2 Sandbox / Container") {
			t.Fatalf("compact view:\n%s", view)
		}
		if strings.Contains(view, "\x1b") {
			t.Fatal("NO_COLOR emitted ANSI")
		}
	}
}

func TestLayoutFocusTabsAndTemplateFetch(t *testing.T) {
	b := &dashboardBackend{snapshot: fixtureSnapshot()}
	m := NewDashboard(b, config.Catalog{}, "")
	defer m.cancel()
	m.Update(m.Init()())
	for _, step := range []struct {
		key   string
		focus int
	}{{"2", 1}, {"3", 2}, {"1", 0}, {"\t", 1}, {"\t", 2}, {"\t", 3}} {
		var cmd tea.Cmd
		if step.key == "\t" {
			_, c := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			cmd = c
		} else {
			_, c := m.Update(tea.KeyPressMsg{Code: []rune(step.key)[0], Text: step.key})
			cmd = c
		}
		if m.Focus != step.focus {
			t.Fatalf("%q focus=%d", step.key, m.Focus)
		}
		if step.key == "3" && cmd == nil {
			t.Fatal("template entry did not fetch")
		}
		if cmd != nil {
			msg := cmd().(snapshotMsg)
			if !msg.query.IncludeTemplates {
				t.Fatal("template entry did not fetch")
			}
			m.Update(msg)
		}
	}
	dashboardKey(m, "]")
	if m.rightTab != 1 {
		t.Fatal("next tab")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.rightTab != 0 {
		t.Fatal("left alias")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.rightTab != 1 {
		t.Fatal("right alias")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	dashboardKey(m, "[")
	if m.rightTab != 3 {
		t.Fatal("previous tab wraps")
	}
	_, entryCmd := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	if m.Focus != 2 {
		t.Fatal("t alias")
	}
	if entryCmd == nil || !entryCmd().(snapshotMsg).query.IncludeTemplates {
		t.Fatal("t entry did not fetch")
	}
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"}); cmd != nil {
		t.Fatal("repeat t fetched templates")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.Focus != 1 {
		t.Fatal("shift tab did not move backward")
	}
}

func TestLayoutJobKeepsTabAndPopupKeys(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := layoutFixture()
	defer m.cancel()
	m.Focus = 3
	m.rightTab = 0
	m.enqueue(operations.Request{Kind: operations.KindStop})
	if m.rightTab != 0 || !strings.Contains(m.View().Content, "로그•") {
		t.Fatal("running job changed tab or lacks marker")
	}
	dashboardKey(m, "/")
	for _, key := range []string{"1", "[", "]"} {
		dashboardKey(m, key)
	}
	if m.Focus != 3 || m.rightTab != 0 || m.Search != "1[]" {
		t.Fatal("search key isolation")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.actionMenu = true
	before := m.View().Content
	for _, key := range []string{"1", "[", "]"} {
		dashboardKey(m, key)
	}
	if m.Focus != 3 || m.rightTab != 0 || !strings.Contains(before, "1 Workspaces") || !strings.Contains(before, "작업 메뉴") {
		t.Fatal("popup isolation or overlay")
	}
}

func TestLayoutPopupCenteredAndTypedSymbols(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := layoutFixture()
	defer m.cancel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.confirm = &confirmation{request: operations.Request{Kind: operations.KindRemove, Target: operations.Target{Profile: "a1[]"}}, label: "대상 삭제"}
	view := strings.Split(m.View().Content, "\n")
	popupRow, popupColumn := -1, -1
	for y, line := range view {
		if x := strings.Index(line, "╔═ › 실행 확인"); x >= 0 {
			popupRow, popupColumn = y, x
			break
		}
	}
	if popupRow < 8 || popupRow > 25 || popupColumn < 20 || popupColumn > 30 {
		t.Fatalf("popup off center: %d,%d", popupRow, popupColumn)
	}
	if !strings.Contains(strings.Join(view, "\n"), "1 Workspaces") {
		t.Fatal("popup replaced layout")
	}
	for _, line := range view {
		if ansi.StringWidth(line) > 120 {
			t.Fatalf("popup overflow: %q", line)
		}
	}
	for _, key := range []string{"a", "1", "[", "]"} {
		dashboardKey(m, key)
	}
	if m.confirm == nil || m.confirm.typed != "a1[]" || m.Focus != 0 || m.rightTab != 0 {
		t.Fatalf("typed confirmation lost symbols or moved focus: %+v", m.confirm)
	}
}

func TestLayoutPlainTextPreview(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, size := range [][2]int{{120, 40}, {80, 24}} {
		m := layoutFixture()
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		t.Logf("NO_COLOR %dx%d:\n%s", size[0], size[1], m.View().Content)
		m.cancel()
	}
}

func TestTemplateSelectionPersistsAcrossRefresh(t *testing.T) {
	m := layoutFixture()
	defer m.cancel()
	m.Focus = 2
	s := m.Snapshot
	s.TemplateRows = []operations.Template{{Repository: "first", Tag: "one", ID: "id-1"}, {Repository: "second", Tag: "two", ID: "id-2"}}
	s.Templates.CheckedAt = time.Now()
	deliver(m, s)
	dashboardKey(m, "j")
	if m.TemplateID != "id-2" {
		t.Fatalf("template selection %q", m.TemplateID)
	}
	s.TemplateRows[0], s.TemplateRows[1] = s.TemplateRows[1], s.TemplateRows[0]
	deliver(m, s)
	if m.TemplateID != "id-2" || m.templates.SelectedItem().(panelItem).id != "id-2" {
		t.Fatal("template refresh changed selection")
	}
}

func TestAllManagementPopupsOverlayLayout(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, tc := range []struct {
		name, title string
		open        func(*Dashboard)
	}{
		{"confirmation", "실행 확인", func(m *Dashboard) { m.confirm = &confirmation{label: "확인"} }},
		{"action", "작업 메뉴", func(m *Dashboard) { m.actionMenu = true }},
		{"advanced", "고급 작업", func(m *Dashboard) { m.advancedMenu = true }},
		{"exit", "종료 확인", func(m *Dashboard) { m.exitPrompt = true }},
		{"blocked", "실행 차단", func(m *Dashboard) { m.pruneBlocked = []operations.Target{{Profile: "blocked"}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := layoutFixture()
			defer m.cancel()
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			tc.open(m)
			view := m.View().Content
			if !strings.Contains(view, "1 Workspaces") || !strings.Contains(view, "3 Templates") {
				t.Fatal("popup replaced layout")
			}
			found := false
			for y, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > 120 {
					t.Fatal("popup line overflow")
				}
				if x := strings.Index(line, "╔═ › "+tc.title); x >= 0 {
					if x < 20 || x > 30 || y < 5 || y > 30 {
						t.Fatalf("popup off center %d,%d", x, y)
					}
					found = true
				}
			}
			if !found {
				t.Fatalf("popup border/title missing:\n%s", view)
			}
		})
	}
}

func TestLayoutUnknownGlyph(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := layoutFixture()
	defer m.cancel()
	m.Snapshot.Workspaces[0].Sandboxes[0].RuntimeState = "unknown"
	if view := m.View().Content; !strings.Contains(view, "? container/codex") || !strings.Contains(view, "  C ") {
		t.Fatalf("unknown state glyph missing:\n%s", view)
	}
}

func TestLayoutHelpListsNewKeys(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := layoutFixture()
	defer m.cancel()
	m.Help = true
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := m.View().Content
	for _, want := range []string{"1/2/3", "Tab/Shift+Tab", "[ ]/←→/hl"} {
		if !strings.Contains(view, want) {
			t.Fatalf("full help missing %q:\n%s", want, view)
		}
	}
}
