package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"os/exec"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
	"paseo-sbx/internal/sandboxupdate"
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
func (b *dashboardBackend) ShellCommand(context.Context, operations.Target) (*exec.Cmd, error) {
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
		if !strings.Contains(view, "a-two") {
			t.Fatalf("focused row hidden at %v: %q", size, view)
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
