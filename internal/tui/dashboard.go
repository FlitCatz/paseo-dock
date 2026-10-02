package tui

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"context"
	"io"
	"os/exec"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
	"slices"
	"strings"
	"time"
)

type Backend interface {
	Snapshot(context.Context, operations.Query) (operations.Snapshot, error)
	Execute(context.Context, operations.Request, func(operations.Event)) (operations.Result, error)
	ShellCommand(context.Context, operations.Target) (*exec.Cmd, error)
	SavedSelection(string) (config.Selection, error)
}
type pollMsg time.Time
type snapshotMsg struct {
	id       uint64
	query    operations.Query
	snapshot operations.Snapshot
	err      error
}
type Dashboard struct {
	Backend                        Backend
	Catalog                        config.Catalog
	Snapshot                       operations.Snapshot
	WorkspaceID, ProfileID, Search string
	Focus                          int
	Width, Height                  int
	LastSuccess                    time.Time
	Error                          string
	Help, Searching, Unmanaged     bool
	requestID                      uint64
	pending                        bool
	pollScheduled                  bool
	ctx                            context.Context
	cancel                         context.CancelFunc
	lastSlow                       time.Time
	templatesWanted                bool
	slowWanted                     bool
	templateTab                    bool
	detailTab                      int
	workspaces, sandboxes          list.Model
	details, logs                  viewport.Model
	help                           help.Model
}

func NewDashboard(b Backend, c config.Catalog, w string) *Dashboard {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Dashboard{Backend: b, Catalog: c, WorkspaceID: w, ctx: ctx, cancel: cancel, Width: 80, Height: 24, details: viewport.New(), logs: viewport.New(), help: help.New()}
	m.workspaces = newPanelList()
	m.sandboxes = newPanelList()
	return m
}
func poll() tea.Cmd { return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return pollMsg(t) }) }
func (m *Dashboard) refresh(now time.Time, manual bool) tea.Cmd {
	if manual {
		m.templatesWanted = true
		m.slowWanted = true
	}
	if m.pending {
		return nil
	}
	m.pending = true
	m.requestID++
	id := m.requestID
	slow := m.slowWanted || m.lastSlow.IsZero() || now.Sub(m.lastSlow) >= 15*time.Second
	q := operations.Query{IncludePaseo: slow, IncludeDocker: slow, IncludeTemplates: m.templatesWanted}
	m.templatesWanted = false
	m.slowWanted = false
	if slow {
		m.lastSlow = now
	}
	b, ctx := m.Backend, m.ctx
	return func() tea.Msg {
		s, err := b.Snapshot(ctx, q)
		return snapshotMsg{id: id, query: q, snapshot: s, err: err}
	}
}
func (m *Dashboard) Init() tea.Cmd { return m.refresh(time.Now(), false) }
func (m *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = max(1, v.Width)
		m.Height = max(1, v.Height)
	case pollMsg:
		m.pollScheduled = false
		return m, m.refresh(time.Time(v), false)
	case snapshotMsg:
		if v.id != m.requestID || !m.pending {
			return m, nil
		}
		m.pending = false
		if v.err != nil {
			m.Error = v.err.Error()
			m.Snapshot.SBX.Error = m.Error
			m.Snapshot.SBX.Status = "error"
			m.unknownRuntime()
		} else {
			old := m.Snapshot
			s := v.snapshot
			s.Workspaces = slices.Clone(s.Workspaces)
			s.Unmanaged = slices.Clone(s.Unmanaged)
			for i := range s.Workspaces {
				s.Workspaces[i].Sandboxes = slices.Clone(s.Workspaces[i].Sandboxes)
			}
			if s.Paseo.CheckedAt.IsZero() {
				s.Paseo = old.Paseo
			}
			if s.Docker.CheckedAt.IsZero() {
				s.Docker = old.Docker
			}
			if s.Templates.CheckedAt.IsZero() {
				s.Templates = old.Templates
				s.TemplateRows = old.TemplateRows
			} else if s.Templates.Error != "" {
				s.TemplateRows = old.TemplateRows
			}
			if s.SBX.Error != "" {
				s.Unmanaged = old.Unmanaged
			}
			// Corrupt registries retain previously observed rows alongside the new registry error.
			for i := range s.Workspaces {
				if s.Workspaces[i].Error != "" && len(s.Workspaces[i].Sandboxes) == 0 {
					for _, w := range old.Workspaces {
						if w.ID == s.Workspaces[i].ID {
							s.Workspaces[i].Sandboxes = slices.Clone(w.Sandboxes)
							s.Workspaces[i].Path = w.Path
							s.Workspaces[i].Selection = w.Selection
						}
					}
				}
			}
			m.Snapshot = s
			m.Error = s.SBX.Error
			if s.SBX.Error != "" {
				m.unknownRuntime()
			} else {
				m.LastSuccess = s.SBX.CheckedAt
				if m.LastSuccess.IsZero() {
					m.LastSuccess = time.Now()
				}
			}
			m.reconcileSelection()
		}
		m.syncLists()
		if !m.pollScheduled {
			m.pollScheduled = true
			return m, poll()
		}
		return m, nil
	case tea.KeyPressMsg:
		k := v.String()
		if k == "ctrl+c" {
			m.cancel()
			return m, tea.Quit
		}
		if m.Searching {
			switch k {
			case "esc":
				m.Searching = false
				m.Search = ""
			case "enter":
				m.Searching = false
			case "backspace":
				r := []rune(m.Search)
				if len(r) > 0 {
					m.Search = string(r[:len(r)-1])
				}
			default:
				if v.Text != "" {
					m.Search += v.Text
				}
			}
			m.syncLists()
			return m, nil
		}
		switch k {
		case "q":
			m.cancel()
			return m, tea.Quit
		case "tab":
			m.Focus = (m.Focus + 1) % 4
			if m.Focus >= 2 {
				m.detailTab = m.Focus - 2
			}
		case "shift+tab":
			m.Focus = (m.Focus + 3) % 4
			if m.Focus >= 2 {
				m.detailTab = m.Focus - 2
			}
		case "/":
			m.Searching = true
			m.Search = ""
		case "esc":
			m.Search = ""
			m.Help = false
		case "?":
			m.Help = !m.Help
		case "r":
			return m, m.refresh(time.Now(), true)
		case "t":
			m.templateTab = !m.templateTab
			if m.templateTab {
				m.Focus = 2
				m.templatesWanted = true
				return m, m.refresh(time.Now(), false)
			}
		case "v":
			m.Unmanaged = !m.Unmanaged
			m.ProfileID = ""
			m.reconcileSelection()
		case "left", "right", "h", "l":
			if m.Focus >= 2 {
				m.detailTab = 1 - m.detailTab
				m.Focus = 2 + m.detailTab
			}
		case "up", "k", "down", "j":
			if m.Focus < 2 {
				m.syncLists()
				l := &m.workspaces
				if m.Focus == 1 {
					l = &m.sandboxes
				}
				if k == "up" || k == "k" {
					l.CursorUp()
				} else {
					l.CursorDown()
				}
				if item, ok := l.SelectedItem().(panelItem); ok {
					if m.Focus == 0 {
						m.WorkspaceID = item.id
						m.ProfileID = ""
						m.reconcileSelection()
					} else {
						m.ProfileID = item.id
					}
				}
			} else {
				var cmd tea.Cmd
				if m.Focus == 3 {
					m.logs, cmd = m.logs.Update(msg)
				} else {
					m.details, cmd = m.details.Update(msg)
				}
				return m, cmd
			}
		}
	}
	m.syncLists()
	return m, nil
}
func (m *Dashboard) unknownRuntime() {
	for i := range m.Snapshot.Workspaces {
		for j := range m.Snapshot.Workspaces[i].Sandboxes {
			m.Snapshot.Workspaces[i].Sandboxes[j].RuntimeState = "unknown"
		}
	}
	for i := range m.Snapshot.Unmanaged {
		m.Snapshot.Unmanaged[i].RuntimeState = "unknown"
	}
}
func (m *Dashboard) selectedWorkspace() *operations.Workspace {
	for i := range m.Snapshot.Workspaces {
		w := &m.Snapshot.Workspaces[i]
		if w.ID == m.WorkspaceID {
			return w
		}
	}
	return nil
}
func (m *Dashboard) rows() []operations.Sandbox {
	if m.Unmanaged {
		return m.Snapshot.Unmanaged
	}
	if w := m.selectedWorkspace(); w != nil {
		return w.Sandboxes
	}
	return nil
}
func sandboxID(s operations.Sandbox) string {
	if s.Target.Profile != "" {
		return s.Target.Profile
	}
	return s.Target.Entry.Sandbox
}
func (m *Dashboard) reconcileSelection() {
	found := false
	for _, w := range m.Snapshot.Workspaces {
		if w.ID == m.WorkspaceID || w.Path == m.WorkspaceID {
			m.WorkspaceID = w.ID
			found = true
			break
		}
	}
	if !found {
		m.WorkspaceID = ""
		if len(m.Snapshot.Workspaces) > 0 {
			m.WorkspaceID = m.Snapshot.Workspaces[0].ID
		}
	}
	rows := m.rows()
	for _, s := range rows {
		if sandboxID(s) == m.ProfileID {
			return
		}
	}
	m.ProfileID = ""
	if len(rows) > 0 {
		m.ProfileID = sandboxID(rows[0])
	}
}
func (m *Dashboard) syncLists() {
	matches := func(s string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(m.Search)) }
	var ws, ss []list.Item
	for _, w := range m.Snapshot.Workspaces {
		label := w.Path
		if label == "" {
			label = w.ID
		}
		if matches(label) {
			ws = append(ws, panelItem{id: w.ID, label: label})
		}
	}
	workspaceVisible := false
	for _, item := range ws {
		if item.(panelItem).id == m.WorkspaceID {
			workspaceVisible = true
			break
		}
	}
	if !workspaceVisible && len(ws) > 0 {
		m.WorkspaceID = ws[0].(panelItem).id
		m.ProfileID = ""
		m.reconcileSelection()
	}
	for _, s := range m.rows() {
		id := sandboxID(s)
		label := id + "  " + s.RuntimeState + "  " + s.Registration
		if s.Active {
			label += "  활성"
		}
		if m.Unmanaged {
			label += "  읽기 전용"
		}
		if matches(label) {
			ss = append(ss, panelItem{id: id, label: label})
		}
	}
	m.workspaces.SetItems(ws)
	m.sandboxes.SetItems(ss)
	profileVisible := false
	for _, item := range ss {
		if item.(panelItem).id == m.ProfileID {
			profileVisible = true
			break
		}
	}
	if !profileVisible && len(ss) > 0 {
		m.ProfileID = ss[0].(panelItem).id
	}
	for i, w := range ws {
		if w.(panelItem).id == m.WorkspaceID {
			m.workspaces.Select(i)
		}
	}
	for i, s := range ss {
		if s.(panelItem).id == m.ProfileID {
			m.sandboxes.Select(i)
		}
	}
}
func RunDashboard(b Backend, c config.Catalog, w string, in io.Reader, out io.Writer) error {
	m := NewDashboard(b, c, w)
	defer m.cancel()
	_, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out)).Run()
	return err
}
