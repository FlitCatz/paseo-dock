package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
	"paseo-dock/internal/sandboxupdate"
	"strings"
	"testing"
	"time"
)

type dashboardBackend struct {
	queries         []operations.Query
	snapshot        operations.Snapshot
	err             error
	observedContext context.Context
}

func (b *dashboardBackend) Snapshot(ctx context.Context, q operations.Query) (operations.Snapshot, error) {
	b.queries = append(b.queries, q)
	b.observedContext = ctx
	return b.snapshot, b.err
}
func (b *dashboardBackend) Execute(context.Context, operations.Request, func(operations.Event)) (operations.Result, error) {
	panic("dashboard must not mutate")
}
func (b *dashboardBackend) ShellCommand(context.Context, operations.Target) (operations.ShellProcess, error) {
	panic("dashboard must not shell")
}
func (b *dashboardBackend) SavedSelection(string) (config.Selection, error) {
	return config.Selection{}, nil
}
func fixtureSnapshot() operations.Snapshot {
	return operations.Snapshot{SBX: operations.Connection{Status: "connected", CheckedAt: time.Unix(10, 0)}, Workspaces: []operations.Workspace{
		{ID: "runtime-a", Path: "/한국어/a", Sandboxes: []operations.Sandbox{{Target: operations.Target{Profile: "a-one", Entry: config.Entry{Sandbox: "sandbox-one"}}, RuntimeState: "running"}, {Target: operations.Target{Profile: "a-two", Entry: config.Entry{Sandbox: "sandbox-two"}}, RuntimeState: "stopped"}}},
		{ID: "runtime-b", Path: "/workspace/b", Sandboxes: []operations.Sandbox{{Target: operations.Target{Profile: "b-one"}, RuntimeState: "running"}}},
	}}
}
func deliver(m *Dashboard, s operations.Snapshot) {
	m.requestID++
	m.pending = true
	m.Update(snapshotMsg{id: m.requestID, snapshot: s})
}
func dashboardKey(m *Dashboard, s string) { m.Update(tea.KeyPressMsg{Code: []rune(s)[0], Text: s}) }
func TestDashboardStaleResponseAndSelection(t *testing.T) {
	m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "/한국어/a")
	s := fixtureSnapshot()
	deliver(m, s)
	if m.WorkspaceID != "runtime-a" || m.ProfileID != "a-one" {
		t.Fatalf("initial identity %q %q", m.WorkspaceID, m.ProfileID)
	}
	m.ProfileID = "a-two"
	s.Workspaces[0].Sandboxes[0], s.Workspaces[0].Sandboxes[1] = s.Workspaces[0].Sandboxes[1], s.Workspaces[0].Sandboxes[0]
	s.Workspaces[0], s.Workspaces[1] = s.Workspaces[1], s.Workspaces[0]
	deliver(m, s)
	if m.WorkspaceID != "runtime-a" || m.ProfileID != "a-two" {
		t.Fatal("reorder changed identity")
	}
	m.Update(snapshotMsg{id: m.requestID - 1, snapshot: operations.Snapshot{}})
	if len(m.Snapshot.Workspaces) != 2 || m.ProfileID != "a-two" {
		t.Fatal("stale response changed data")
	}
	s.Workspaces = s.Workspaces[:1]
	deliver(m, s)
	if m.WorkspaceID != "runtime-b" || m.ProfileID != "b-one" {
		t.Fatal("disappeared workspace leaves unsafe selection")
	}
}
func TestDashboardRefreshCoalesces(t *testing.T) {
	b := &dashboardBackend{snapshot: fixtureSnapshot()}
	m := NewDashboard(b, config.Catalog{}, "")
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("initial query missing")
	}
	response := cmd()
	for i := 0; i < 5; i++ {
		_, cmd = m.Update(pollMsg(time.Unix(20+int64(i), 0)))
		if cmd != nil { // Tick commands are allowed; they must not query.
			msg := cmd()
			if _, ok := msg.(snapshotMsg); ok {
				t.Fatal("overlapping snapshot query")
			}
		}
	}
	if len(b.queries) != 1 {
		t.Fatalf("queries while pending: %d", len(b.queries))
	}
	m.Update(response)
	_, cmd = m.Update(pollMsg(time.Now()))
	if cmd == nil {
		t.Fatal("completion prevented next query")
	}
	m.Update(cmd())
	if len(b.queries) != 2 {
		t.Fatalf("queries after completion %d", len(b.queries))
	}
	if !b.queries[0].IncludePaseo || !b.queries[0].IncludeDocker || b.queries[0].IncludeTemplates {
		t.Fatalf("initial query %+v", b.queries[0])
	}
}
func TestDashboardFailureKeepsLastSuccess(t *testing.T) {
	m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
	s := fixtureSnapshot()
	s.Paseo = operations.Connection{Status: "running", CheckedAt: time.Unix(9, 0)}
	deliver(m, s)
	last := m.LastSuccess
	m.requestID++
	m.pending = true
	m.Update(snapshotMsg{id: m.requestID, err: errors.New("offline")})
	if len(m.Snapshot.Workspaces) != 2 || m.Snapshot.Workspaces[0].Sandboxes[0].RuntimeState != "unknown" || m.Error != "offline" || !m.LastSuccess.Equal(last) || last.IsZero() {
		t.Fatalf("failed refresh lost state: %+v", m)
	}
	deliver(m, fixtureSnapshot())
	if m.Snapshot.Paseo.Status != "running" {
		t.Fatal("fast query discarded slow metadata")
	}
}
func TestDashboardNavigationSearch(t *testing.T) {
	m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
	deliver(m, fixtureSnapshot())
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	dashboardKey(m, "j")
	if m.Focus != 1 || m.ProfileID != "a-two" {
		t.Fatal("tab and j failed")
	}
	dashboardKey(m, "/")
	dashboardKey(m, "t")
	dashboardKey(m, "w")
	dashboardKey(m, "o")
	if !strings.Contains(m.View().Content, "a-two") {
		t.Fatal("search hides matching selected row")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Search != "" || m.Searching {
		t.Fatal("escape failed")
	}
	dashboardKey(m, "?")
	if !m.Help {
		t.Fatal("help failed")
	}
	dashboardKey(m, "?")
}
func TestDashboardNarrowKoreanNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
	deliver(m, fixtureSnapshot())
	m.Focus = 1
	m.ProfileID = "a-two"
	for _, size := range [][2]int{{160, 40}, {80, 24}, {20, 5}, {8, 3}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View().Content
		if strings.Contains(view, "\x1b[") {
			t.Fatal("NO_COLOR emitted ANSI")
		}
		lines := strings.Split(view, "\n")
		if len(lines) > size[1] {
			t.Fatalf("height %d exceeds %d", len(lines), size[1])
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("line too wide %q", line)
			}
		}
		want := "a-t"
		if size[0] <= 8 {
			want = "› ○"
		}
		if !strings.Contains(view, want) || m.sandboxes.SelectedItem().(panelItem).id != "a-two" {
			t.Fatalf("focused row hidden at %v: %q", size, view)
		}
	}
}

func TestDashboardSectionsHaveVisibleFrames(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, size := range [][2]int{{120, 40}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
			defer m.cancel()
			deliver(m, fixtureSnapshot())
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			view := m.View().Content
			frames := 4
			if size[0] >= 80 && size[1] >= 28 {
				frames += 4
			} // overview cards
			if strings.Count(view, "╭")+strings.Count(view, "╔") != frames || strings.Count(view, "╰")+strings.Count(view, "╚") != frames {
				t.Fatalf("four independently closed sections are not visible:\n%s", view)
			}
			for _, title := range []string{"1 Workspaces", "2 Sandbox / Container", "3 Templates", "[상세] · 로그 · 버전 · 설정"} {
				if !strings.Contains(view, title) {
					t.Errorf("section title %q is missing", title)
				}
			}
			if strings.Contains(view, "\x1b[") {
				t.Fatal("section boundaries require color despite NO_COLOR")
			}
			for focus := 0; focus < 4; focus++ {
				m.Focus = focus
				focused := m.View().Content
				if strings.Count(focused, "╔") != 1 || strings.Count(focused, "╚") != 1 {
					t.Fatalf("focus %d is not uniquely marked by its frame", focus)
				}
				if len(strings.Split(focused, "\n")) > size[1] {
					t.Fatal("frames overflow terminal height")
				}
				for _, line := range strings.Split(focused, "\n") {
					if ansi.StringWidth(line) > size[0] {
						t.Fatal("frames overflow terminal width")
					}
				}
			}
			if size == [2]int{80, 24} {
				t.Logf("80x24 section preview:\n%s", view)
			}
		})
	}
}

func TestDashboardActionMenuHasItsOwnFrame(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
	defer m.cancel()
	deliver(m, fixtureSnapshot())
	m.actionMenu = true
	view := m.View().Content
	if strings.Count(view, "╔") < 1 || strings.Count(view, "╚") < 1 || !strings.Contains(view, "작업 메뉴") || !strings.Contains(view, "1 Workspaces") {
		t.Fatalf("action menu has no distinct closed section:\n%s", view)
	}
}

// Removing display-width clipping or list scrolling must hide/overflow the selection.
func TestDashboardTerminalBounds(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 10}, {12, 4}} {
		for _, focus := range []int{0, 1} {
			t.Run(fmt.Sprintf("%dx%d/focus%d", size[0], size[1], focus), func(t *testing.T) {
				m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
				defer m.cancel()
				s := fixtureSnapshot()
				s.Workspaces[0].Path = "/한국어/선택된-아주-긴-workspace-경로"
				s.SBX.Error = strings.Repeat("연결 실패: 긴 오류 경로 /한국어/파일 ", 40)
				for i := 0; i < 40; i++ {
					s.Workspaces[0].Sandboxes = append(s.Workspaces[0].Sandboxes, operations.Sandbox{Target: operations.Target{Profile: fmt.Sprintf("한글-%02d", i)}, RuntimeState: "unknown"})
				}
				deliver(m, s)
				m.Focus, m.ProfileID = focus, "한글-39"
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				view := m.View().Content
				lines := strings.Split(view, "\n")
				if len(lines) > size[1] {
					t.Fatalf("height %d > %d", len(lines), size[1])
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > size[0] {
						t.Fatalf("width exceeds %d: %q", size[0], line)
					}
				}
				want := "› 선택된"
				if focus == 0 && m.workspaces.SelectedItem().(panelItem).id != "runtime-a" {
					t.Fatal("compact label changed selected identity")
				}
				if focus == 1 {
					want = "› ? 한글"
				}
				if size[0] <= 12 {
					want = "›"
				}
				if !strings.Contains(view, want) {
					t.Fatalf("focused selection invisible: %q", view)
				}
			})
		}
	}
}

// Output must remain bounded and responsive when a subprocess emits megabytes.
func TestDashboardLogFloodBounds(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
	defer m.cancel()
	deliver(m, fixtureSnapshot())
	log := newJobLog()
	log.emit(operations.Event{Stage: "stdout", Text: strings.Repeat("한국어 출력\n", 200000) + "마지막 출력"})
	if len(log.text()) > 256<<10 || !strings.HasSuffix(log.text(), "마지막 출력") {
		t.Fatal("log did not retain bounded tail")
	}
	m.jobs = []*job{{id: 1, state: "running", log: log, request: operations.Request{Kind: operations.KindUpdate}}}
	m.Focus = 3
	for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 10}, {12, 4}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View().Content
		if len(strings.Split(view, "\n")) > size[1] {
			t.Fatal("flood overflows terminal height")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("flood overflows terminal width")
			}
		}
	}
}

func TestDashboardPendingManualRefreshIncludesSlowMetadata(t *testing.T) {
	b := &dashboardBackend{snapshot: fixtureSnapshot()}
	m := NewDashboard(b, config.Catalog{}, "")
	response := m.Init()()
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m.Update(response)
	_, cmd := m.Update(pollMsg(time.Now()))
	msg := cmd().(snapshotMsg)
	if !msg.query.IncludePaseo || !msg.query.IncludeDocker || !msg.query.IncludeTemplates {
		t.Fatalf("pending manual refresh lost metadata: %+v", msg.query)
	}
}
func TestDashboardCadenceAndTemplateTab(t *testing.T) {
	b := &dashboardBackend{snapshot: fixtureSnapshot()}
	m := NewDashboard(b, config.Catalog{}, "")
	m.Update(m.Init()())
	start := time.Now()
	for _, step := range []struct {
		after time.Duration
		slow  bool
	}{{3 * time.Second, false}, {6 * time.Second, false}, {15 * time.Second, true}} {
		_, cmd := m.Update(pollMsg(start.Add(step.after)))
		if cmd == nil {
			t.Fatal("poll never resumed")
		}
		msg := cmd().(snapshotMsg)
		if msg.query.IncludePaseo != step.slow || msg.query.IncludeDocker != step.slow || msg.query.IncludeTemplates {
			t.Fatalf("at %s wrong query %+v", step.after, msg.query)
		}
		m.Update(msg)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	msg := cmd().(snapshotMsg)
	if !msg.query.IncludeTemplates {
		t.Fatal("template tab did not query")
	}
	m.Update(msg)
}
func TestDashboardDetailLogTab(t *testing.T) {
	m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
	deliver(m, fixtureSnapshot())
	m.Focus = 2
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Focus = 3
	view := m.View().Content
	if !strings.Contains(view, "조회 dashboard") {
		t.Fatalf("detail/log tab did not switch: %q", view)
	}
}
func TestDashboardLongListFocusedRowVisible(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
	s := fixtureSnapshot()
	for i := 0; i < 40; i++ {
		s.Workspaces[0].Sandboxes = append(s.Workspaces[0].Sandboxes, operations.Sandbox{Target: operations.Target{Profile: fmt.Sprintf("한국어-%02d", i)}})
	}
	deliver(m, s)
	m.Focus = 1
	m.ProfileID = "한국어-39"
	for _, size := range [][2]int{{160, 40}, {80, 24}, {20, 5}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if view := m.View().Content; !strings.Contains(view, "한국어-39") {
			t.Fatalf("last selection invisible at %v: %q", size, view)
		}
	}
}
func TestDashboardFailureDoesNotMutateBackendSnapshot(t *testing.T) {
	s := fixtureSnapshot()
	m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
	deliver(m, s)
	m.requestID++
	m.pending = true
	m.Update(snapshotMsg{id: m.requestID, err: errors.New("offline")})
	if s.Workspaces[0].Sandboxes[0].RuntimeState != "running" {
		t.Fatal("dashboard mutated backend-owned observation")
	}
}
func TestDashboardQuitCancelsPendingQuery(t *testing.T) {
	b := &dashboardBackend{snapshot: fixtureSnapshot()}
	m := NewDashboard(b, config.Catalog{}, "")
	cmd := m.Init()
	m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	cmd()
	if b.observedContext.Err() != context.Canceled {
		t.Fatal("quit did not cancel query context")
	}
}

func TestDashboardUnmanagedSelectsReadOnlyRow(t *testing.T) {
	m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
	s := fixtureSnapshot()
	s.Unmanaged = []operations.Sandbox{{Target: operations.Target{Entry: config.Entry{Sandbox: "external-sandbox"}}, RuntimeState: "running", Registration: "unmanaged"}}
	deliver(m, s)
	dashboardKey(m, "v")
	m.Focus = 2
	if m.ProfileID != "external-sandbox" || !strings.Contains(m.View().Content, "읽기 전용") {
		t.Fatal("unmanaged view does not retain an actual read-only selection")
	}
}

func TestDashboardUnmanagedRowHasNoCategorySuffix(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
	defer m.cancel()
	s := fixtureSnapshot()
	s.Unmanaged = []operations.Sandbox{{Target: operations.Target{Entry: config.Entry{Sandbox: "external-sandbox"}}, RuntimeState: "running"}}
	deliver(m, s)
	dashboardKey(m, "v")
	m.Focus = 1
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	for _, line := range strings.Split(m.View().Content, "\n") {
		if strings.Contains(line, "║ ›") && strings.Contains(line, "external-sandbox") {
			if !strings.Contains(line, "읽기 전용") || strings.Contains(line, "  L") {
				t.Fatalf("unmanaged row mislabeled: %q", line)
			}
			return
		}
	}
	t.Fatal("unmanaged row not rendered")
}

func TestDashboardDetailResourceAndRuntimeLabels(t *testing.T) {
	for _, tc := range []struct {
		name, want, absent string
		entry              config.Entry
	}{
		{"container", "Container ID: cid-one", "Sandbox: stale-sandbox", config.Entry{Category: config.CategoryContainer, Agent: "codex", Tool: "gsd", ContainerID: "cid-one", Sandbox: "stale-sandbox"}},
		{"sandbox", "Sandbox: sandbox-one", "Container ID:", config.Entry{Category: config.CategorySandbox, Agent: "codex", Sandbox: "sandbox-one"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
			defer m.cancel()
			s := fixtureSnapshot()
			s.Workspaces[0].Sandboxes[0].Target.Entry = tc.entry
			deliver(m, s)
			content := m.detailContent()
			for _, want := range []string{tc.want, "Runtime ID: runtime-a", "Runtime: running"} {
				if !strings.Contains(content, want) {
					t.Errorf("missing %q in %q", want, content)
				}
			}
			if strings.Contains(content, tc.absent) {
				t.Errorf("unexpected %q in %q", tc.absent, content)
			}
		})
	}
}
func TestDashboardTemplateTabVisibleOnNarrowScreen(t *testing.T) {
	b := &dashboardBackend{snapshot: fixtureSnapshot()}
	m := NewDashboard(b, config.Catalog{}, "")
	deliver(m, fixtureSnapshot())
	m.Update(tea.WindowSizeMsg{Width: 30, Height: 10})
	_, cmd := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	m.Update(cmd())
	if !strings.Contains(m.View().Content, "Templates") {
		t.Fatalf("template tab hidden: %q", m.View().Content)
	}
}
func TestDashboardPartialSBXFailureRetainsRowsAndTime(t *testing.T) {
	m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
	s := fixtureSnapshot()
	s.Unmanaged = []operations.Sandbox{{Target: operations.Target{Entry: config.Entry{Sandbox: "external"}}, RuntimeState: "running"}}
	deliver(m, s)
	last := m.LastSuccess
	next := fixtureSnapshot()
	next.SBX = operations.Connection{Error: "login unavailable", Status: "error", CheckedAt: time.Unix(20, 0)}
	deliver(m, next)
	if len(m.Snapshot.Unmanaged) != 1 || m.Snapshot.Unmanaged[0].RuntimeState != "unknown" || m.Snapshot.Workspaces[0].Sandboxes[0].RuntimeState != "unknown" || !m.LastSuccess.Equal(last) || m.Error != "login unavailable" {
		t.Fatal("connection failure became an empty/missing snapshot")
	}
}

func TestDashboardUnconfirmedUpdateIsNotSuccess(t *testing.T) {
	m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
	s := fixtureSnapshot()
	s.Workspaces[0].Sandboxes[0].Update = &sandboxupdate.State{Versions: map[string]string{"agent": "1.0"}}
	deliver(m, s)
	if content := m.detailContent(); strings.Contains(content, "업데이트: 성공") || !strings.Contains(content, "미확인") {
		t.Fatalf("unconfirmed update shown as success: %q", content)
	}
}

func TestDashboardManualRefreshKeepsSinglePollingTimer(t *testing.T) {
	b := &dashboardBackend{snapshot: fixtureSnapshot()}
	m := NewDashboard(b, config.Catalog{}, "")
	_, scheduled := m.Update(m.Init()())
	if scheduled == nil {
		t.Fatal("initial observation did not schedule polling")
	}
	for i := 0; i < 3; i++ {
		_, query := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
		if query == nil {
			t.Fatal("manual query missing")
		}
		_, extraTimer := m.Update(query())
		if extraTimer != nil {
			t.Fatal("manual response added another polling timer before existing timer fired")
		}
	}
	_, query := m.Update(scheduled())
	if query == nil {
		t.Fatal("original timer stopped polling")
	}
	_, nextTimer := m.Update(query())
	if nextTimer == nil {
		t.Fatal("consumed timer was not rearmed after observation")
	}
	if len(b.queries) != 5 {
		t.Fatalf("query count %d, want initial + 3 manual + 1 scheduled", len(b.queries))
	}
}

func TestDashboardSearchKeepsMarkerIdentityAndDetailsAligned(t *testing.T) {
	for _, tc := range []struct {
		name, search, workspace, profile string
		focus                            int
	}{
		{"workspace", "/workspace/b", "runtime-b", "b-one", 0},
		{"profile", "two", "runtime-a", "a-two", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
			deliver(m, fixtureSnapshot())
			m.Focus = tc.focus
			dashboardKey(m, "/")
			for _, r := range tc.search {
				dashboardKey(m, string(r))
			}
			view := m.View().Content
			if m.WorkspaceID != tc.workspace || m.ProfileID != tc.profile {
				t.Fatalf("filter left wrong identity %s/%s: %q", m.WorkspaceID, m.ProfileID, view)
			}
			if details := m.detailContent(); !strings.Contains(details, "Profile: "+tc.profile) {
				t.Fatalf("details diverged: %q", details)
			}
			selected := m.workspaces.SelectedItem()
			want := tc.workspace
			if tc.focus == 1 {
				selected = m.sandboxes.SelectedItem()
				want = tc.profile
			}
			if selected == nil || selected.(panelItem).id != want {
				t.Fatalf("highlight differs from authoritative identity: %+v", selected)
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			m.View()
			if m.WorkspaceID != tc.workspace || m.ProfileID != tc.profile {
				t.Fatal("clearing search changed visible selected identity")
			}
		})
	}
}

func TestDashboardDetailsScrollAfterReturningFromLogs(t *testing.T) {
	m := NewDashboard(&dashboardBackend{}, config.Catalog{}, "")
	s := fixtureSnapshot()
	s.Workspaces[0].Sandboxes[0].InitialVersions = map[string]string{}
	for i := 0; i < 30; i++ {
		s.Workspaces[0].Sandboxes[0].InitialVersions[fmt.Sprintf("tool-%02d", i)] = "1.0"
	}
	deliver(m, s)
	m.Focus = 3
	m.View()
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.View()
	if m.Focus != 3 || m.rightTab != 2 {
		t.Fatalf("left returned to focus/tab %d/%d", m.Focus, m.rightTab)
	}
	logOffset := m.logs.YOffset()
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.details.YOffset() != 1 || m.logs.YOffset() != logOffset {
		t.Fatalf("details/log offsets %d/%d, want 1/%d", m.details.YOffset(), m.logs.YOffset(), logOffset)
	}
	dashboardKey(m, "j")
	if m.details.YOffset() != 2 {
		t.Fatalf("j scrolled wrong viewport, details offset %d", m.details.YOffset())
	}
}

func TestDashboardTemplatesOnlyOnEntryOrManualRefresh(t *testing.T) {
	b := &dashboardBackend{snapshot: fixtureSnapshot()}
	m := NewDashboard(b, config.Catalog{}, "")
	m.Update(m.Init()())
	_, cmd := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	msg := cmd().(snapshotMsg)
	if !msg.query.IncludeTemplates {
		t.Fatal("entry did not request templates")
	}
	m.Update(msg)
	for i := 0; i < 3; i++ {
		_, cmd = m.Update(pollMsg(time.Now().Add(time.Duration(i+1) * 3 * time.Second)))
		msg = cmd().(snapshotMsg)
		if msg.query.IncludeTemplates {
			t.Fatal("displayed template tab caused repeated automatic template query")
		}
		m.Update(msg)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	msg = cmd().(snapshotMsg)
	if !msg.query.IncludeTemplates {
		t.Fatal("manual refresh omitted templates")
	}
	m.Update(msg)
	dashboardKey(m, "1")
	_, cmd = m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	if !cmd().(snapshotMsg).query.IncludeTemplates {
		t.Fatal("reentry omitted templates")
	}
}
