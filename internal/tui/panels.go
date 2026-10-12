package tui

import (
	"charm.land/bubbles/v2/help"
	keys "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"io"
	"os"
	"paseo-sbx/internal/operations"
	"slices"
	"strings"
	"time"
)

type panelItem struct{ id, label string }

func (i panelItem) FilterValue() string { return i.label }

type panelDelegate struct{}

func (panelDelegate) Height() int                         { return 1 }
func (panelDelegate) Spacing() int                        { return 0 }
func (panelDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (panelDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	i := item.(panelItem)
	mark := "  "
	if index == m.Index() {
		mark = "› "
	}
	line := ansi.Truncate(mark+i.label, m.Width(), "…")
	style := lipgloss.NewStyle()
	if os.Getenv("NO_COLOR") == "" && index == m.Index() {
		style = style.Foreground(lipgloss.Color("86"))
	}
	fmt.Fprint(w, style.Render(line))
}
func newPanelList() list.Model {
	l := list.New(nil, panelDelegate{}, 40, 10)
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	l.SetShowFilter(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()
	return l
}

type dashboardKeys struct{}

func (dashboardKeys) ShortHelp() []keys.Binding {
	return []keys.Binding{
		keys.NewBinding(keys.WithKeys("tab"), keys.WithHelp("Tab", "패널")), keys.NewBinding(keys.WithKeys("up", "down", "j", "k"), keys.WithHelp("↑↓/jk", "이동")), keys.NewBinding(keys.WithKeys("/"), keys.WithHelp("/", "검색")), keys.NewBinding(keys.WithKeys("r"), keys.WithHelp("r", "갱신")), keys.NewBinding(keys.WithKeys("?"), keys.WithHelp("?", "도움말")), keys.NewBinding(keys.WithKeys("q"), keys.WithHelp("q", "종료")),
	}
}
func (dashboardKeys) FullHelp() [][]keys.Binding {
	return [][]keys.Binding{dashboardKeys{}.ShortHelp(), {
		keys.NewBinding(keys.WithKeys("v"), keys.WithHelp("v", "미등록 sandbox (읽기 전용)")), keys.NewBinding(keys.WithKeys("t"), keys.WithHelp("t", "template 탭")), keys.NewBinding(keys.WithKeys("left", "right"), keys.WithHelp("←→", "상세/로그")), keys.NewBinding(keys.WithKeys("esc"), keys.WithHelp("Esc", "검색 취소")),
		keys.NewBinding(keys.WithKeys("enter"), keys.WithHelp("Enter", "작업")), keys.NewBinding(keys.WithKeys("u"), keys.WithHelp("u", "업데이트")), keys.NewBinding(keys.WithKeys("s"), keys.WithHelp("s", "shell")), keys.NewBinding(keys.WithKeys("c"), keys.WithHelp("c", "취소 요청")),
		keys.NewBinding(keys.WithKeys("R"), keys.WithHelp("R", "고정 대상 재시도")),
		keys.NewBinding(keys.WithKeys("n"), keys.WithHelp("n", "새 환경 생성")), keys.NewBinding(keys.WithKeys("a"), keys.WithHelp("a", "고급 build/load/prune")),
	}}
}

var _ help.KeyMap = dashboardKeys{}

func connectionText(name string, c operations.Connection) string {
	status := c.Status
	if status == "" {
		status = "미확인"
	}
	if c.Error != "" {
		status = "확인 불가: " + c.Error
	}
	return name + ": " + status
}
func versionLines(title string, values map[string]string) []string {
	var keys []string
	for k := range values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	lines := []string{}
	for _, k := range keys {
		lines = append(lines, title+" "+k+": "+values[k])
	}
	return lines
}
func (m *Dashboard) detailContent() string {
	if m.templateTab {
		lines := []string{"Templates (읽기 전용)", connectionText("template", m.Snapshot.Templates)}
		for _, t := range m.Snapshot.TemplateRows {
			lines = append(lines, t.Repository+":"+t.Tag+" "+t.ID)
		}
		return strings.Join(lines, "\n")
	}
	lines := []string{}
	if w := m.selectedWorkspace(); w != nil {
		lines = append(lines, "Workspace: "+w.Path, "Runtime: "+w.ID)
		if w.Missing {
			lines = append(lines, "경로 없음 · sandbox 보존")
		}
		if w.Error != "" {
			lines = append(lines, "Registry 오류: "+w.Error)
		}
		if w.Selection != nil {
			lines = append(lines, "저장된 선택: "+strings.Join(w.Selection.Profiles(), ", "), "Utilities: "+strings.Join(w.Selection.Utilities, ", "))
		}
	}
	for _, s := range m.rows() {
		if sandboxID(s) != m.ProfileID {
			continue
		}
		lines = append(lines, "Profile: "+sandboxID(s), "Sandbox: "+s.Target.Entry.Sandbox, "Image: "+s.Target.Entry.Image, "Runtime: "+s.RuntimeState, "Paseo 등록: "+s.Registration)
		lines = append(lines, runtimeSettings(s.Target.Entry)...)
		if s.RegistrationError != "" {
			lines = append(lines, "Paseo 등록 확인 오류: "+s.RegistrationError)
		}
		if m.Unmanaged {
			lines = append(lines, "읽기 전용 · 관리 registry에 등록되지 않음")
		}
		if s.ImageMismatch {
			lines = append(lines, "이미지 구성 불일치 · 재사용 불가")
		}
		lines = append(lines, versionLines("이미지 기준 (guest 미확인)", s.InitialVersions)...)
		if s.VersionCheck != nil {
			if !s.VersionCheck.CheckedAt.IsZero() {
				lines = append(lines, "버전 마지막 확인: "+s.VersionCheck.CheckedAt.Format(time.RFC3339))
				lines = append(lines, versionLines("마지막 확인", s.VersionCheck.Versions)...)
			}
			lines = append(lines, versionLines("버전 확인 오류", s.VersionCheck.Errors)...)
		}
		if s.Update != nil {
			status := "미확인"
			if !s.Update.Success.IsZero() {
				status = "성공"
			} else if !s.Update.Attempt.IsZero() {
				status = "확인 필요"
			}
			if len(s.Update.Errors) > 0 {
				status = "실패"
			}
			lines = append(lines, "업데이트: "+status)
			if !s.Update.Attempt.IsZero() {
				lines = append(lines, "업데이트 마지막 확인: "+s.Update.Attempt.Format(time.RFC3339))
				lines = append(lines, versionLines("업데이트 마지막 확인", s.Update.Versions)...)
				lines = append(lines, versionLines("업데이트 오류", s.Update.Errors)...)
			} else {
				lines = append(lines, "업데이트 버전 미확인")
			}
		}
	}
	if len(lines) == 0 {
		return "선택 항목 없음"
	}
	return strings.Join(lines, "\n")
}

// Section dimensions include the frame; tiny terminals retain the selected row.
func sectionDimensions(width, height int) (contentWidth, contentHeight, padding int, framed bool) {
	width, height = max(1, width), max(1, height)
	if width < 6 || height < 3 {
		titleHeight := 0
		if height > 2 {
			titleHeight = 1
		}
		return width, max(1, height-titleHeight), 0, false
	}
	if width >= 24 {
		padding = 1
	}
	return width - 2 - 2*padding, height - 2, padding, true
}

func renderSection(title, body string, width, height int, focused bool) string {
	width, height = max(1, width), max(1, height)
	contentWidth, contentHeight, padding, framed := sectionDimensions(width, height)
	mark := ""
	if focused {
		mark = "› "
	}
	rows := strings.Split(body, "\n")
	lines := make([]string, 0, height)
	border := lipgloss.RoundedBorder()
	if focused {
		border = lipgloss.DoubleBorder()
	}
	edge, heading := lipgloss.NewStyle(), lipgloss.NewStyle()
	if os.Getenv("NO_COLOR") == "" {
		edge = edge.Foreground(lipgloss.Color("240"))
		heading = heading.Bold(true).Foreground(lipgloss.Color("252"))
		if focused {
			edge = edge.Foreground(lipgloss.Color("86"))
			heading = heading.Foreground(lipgloss.Color("86"))
		}
	}
	if framed {
		label := ansi.Truncate(" "+mark+title+" ", width-3, "…")
		lines = append(lines, edge.Render(border.TopLeft+border.Top)+heading.Render(label)+edge.Render(strings.Repeat(border.Top, width-3-ansi.StringWidth(label))+border.TopRight))
	} else if height > 2 {
		lines = append(lines, heading.Render(ansi.Truncate(mark+title, width, "…")))
	}
	for i := 0; i < contentHeight; i++ {
		line := ""
		if i < len(rows) {
			line = ansi.Truncate(rows[i], contentWidth, "…")
		}
		line += strings.Repeat(" ", max(0, contentWidth-ansi.StringWidth(line)))
		if framed {
			line = edge.Render(border.Left) + strings.Repeat(" ", padding) + line + strings.Repeat(" ", padding) + edge.Render(border.Right)
		}
		lines = append(lines, line)
	}
	if framed {
		lines = append(lines, edge.Render(border.BottomLeft+strings.Repeat(border.Bottom, width-2)+border.BottomRight))
	}
	return strings.Join(lines, "\n")
}

func (m *Dashboard) panel(which, width, height int) string {
	width = max(1, width)
	height = max(1, height)
	title := []string{"Workspaces", "Sandbox / Container", "상세", "작업 로그"}[which]
	if which == 1 && m.Unmanaged {
		title = "미등록 · 읽기 전용"
	}
	if which == 2 && m.templateTab {
		title = "Templates"
	}
	if m.Width < 80 || m.Height < 20 {
		title = fmt.Sprintf("%d/4 %s", which+1, title)
	}
	contentWidth, available, _, _ := sectionDimensions(width, height)
	var body string
	switch which {
	case 0:
		m.workspaces.SetSize(contentWidth, available)
		body = m.workspaces.View()
	case 1:
		m.sandboxes.SetSize(contentWidth, available)
		body = m.sandboxes.View()
	case 2:
		m.details.SetWidth(contentWidth)
		m.details.SetHeight(available)
		m.details.SetContent(m.detailContent())
		body = m.details.View()
	case 3:
		m.logs.SetWidth(contentWidth)
		m.logs.SetHeight(available)
		m.logs.SetContent(m.logContent())
		body = m.logs.View()
	}
	return renderSection(title, body, width, height, which == m.Focus)
}
func (m *Dashboard) View() tea.View {
	if m.creating != nil && !m.exitPrompt {
		return m.creationView()
	}
	m.syncLists()
	width, height := max(1, m.Width), max(1, m.Height)
	header := "PASEO SBX  " + connectionText("sbx", m.Snapshot.SBX) + "  " + connectionText("Paseo", m.Snapshot.Paseo) + "  " + connectionText("Docker", m.Snapshot.Docker)
	if !m.LastSuccess.IsZero() {
		header += "  마지막 성공: " + m.LastSuccess.Format("15:04:05")
	}
	if m.Error != "" {
		header += "  " + m.Error
	}
	m.help.SetWidth(width)
	m.help.ShowAll = m.Help
	footer := m.help.View(dashboardKeys{})
	if m.Searching || m.Search != "" {
		footer = "검색: " + m.Search + " · Esc 취소"
	}
	footerLines := strings.Count(footer, "\n") + 1
	if height < 8 {
		footer = ""
		footerLines = 0
	}
	headerLines := 1
	if height <= 2 {
		headerLines = 0
	}
	available := max(1, height-headerLines-footerLines)
	var body string
	switch {
	case width >= 80 && height >= 20:
		top := (available - 1) / 2
		left := width * 2 / 5
		right := width - left - 1
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.panel(0, left, top), " ", m.panel(1, right, top)) + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, m.panel(2, left, available-top-1), " ", m.panel(3, right, available-top-1))
	default:
		body = m.panel(m.Focus, width, available)
	}
	if content := m.managementContent(); content != "" {
		contentWidth, contentHeight, _, _ := sectionDimensions(width, available)
		m.modal.SetWidth(contentWidth)
		m.modal.SetHeight(contentHeight)
		if !m.actionMenu {
			content = ansi.Hardwrap(content, contentWidth, true)
		}
		m.modal.SetContent(content)
		if m.actionMenu {
			line := m.actionIndex + 1
			if line < m.modal.YOffset() {
				m.modal.SetYOffset(line)
			}
			if line >= m.modal.YOffset()+contentHeight {
				m.modal.SetYOffset(line - contentHeight + 1)
			}
		}
		title := "고급 작업"
		switch {
		case m.exitPrompt:
			title = "종료 확인"
		case m.confirm != nil:
			title = "실행 확인"
		case m.actionMenu:
			title = "작업 메뉴"
		}
		body = renderSection(title, m.modal.View(), width, available, true)
	}
	if m.actionError != "" {
		footer = m.actionError + " · " + footer
	}
	lines := []string{}
	if headerLines > 0 {
		lines = append(lines, header)
	}
	lines = append(lines, strings.Split(body, "\n")...)
	if footer != "" {
		lines = append(lines, strings.Split(footer, "\n")...)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		if os.Getenv("NO_COLOR") != "" {
			lines[i] = ansi.Strip(lines[i])
		}
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}
