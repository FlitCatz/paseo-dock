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
	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
	"slices"
	"strings"
	"time"
)

type panelItem struct{ id, label, suffix, state, compactLabel string }

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
	labelWidth := max(1, m.Width()-ansi.StringWidth(i.suffix))
	label := i.label
	if i.compactLabel != "" && ansi.StringWidth(mark+label) > labelWidth {
		label = i.compactLabel
	}
	line := ansi.Truncate(mark+label, labelWidth, "…")
	line += strings.Repeat(" ", max(0, labelWidth-ansi.StringWidth(line))) + i.suffix
	line = ansi.Truncate(line, m.Width(), "…")
	style := lipgloss.NewStyle()
	if colorsEnabled() {
		style = style.Foreground(lipgloss.Color(ink))
		if i.state != "" {
			style = style.Foreground(lipgloss.Color(stateTone(i.state)))
		}
		if index == m.Index() {
			style = style.Foreground(lipgloss.Color(accent)).Background(lipgloss.Color(selectionSurface)).Bold(true)
		}
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
		keys.NewBinding(keys.WithKeys("u"), keys.WithHelp("u", "업데이트")), keys.NewBinding(keys.WithKeys("s"), keys.WithHelp("s", "shell")), keys.NewBinding(keys.WithKeys("x"), keys.WithHelp("x", "중지")), keys.NewBinding(keys.WithKeys("d"), keys.WithHelp("d", "삭제")), keys.NewBinding(keys.WithKeys("n"), keys.WithHelp("n", "생성")), keys.NewBinding(keys.WithKeys("enter"), keys.WithHelp("Enter", "메뉴")), keys.NewBinding(keys.WithKeys("?"), keys.WithHelp("?", "도움말")),
	}
}
func (dashboardKeys) FullHelp() [][]keys.Binding {
	return [][]keys.Binding{dashboardKeys{}.ShortHelp(), {
		keys.NewBinding(keys.WithKeys("f", "L"), keys.WithHelp("f/L", "패널 확장/로그 화면 · Esc 복귀")),
		keys.NewBinding(keys.WithKeys("1", "2", "3"), keys.WithHelp("1/2/3", "목록 포커스")), keys.NewBinding(keys.WithKeys("tab", "shift+tab"), keys.WithHelp("Tab/Shift+Tab", "패널 순환")), keys.NewBinding(keys.WithKeys("[", "]", "left", "right", "h", "l"), keys.WithHelp("[ ]/←→/hl", "오른쪽 탭")), keys.NewBinding(keys.WithKeys("up", "down", "j", "k"), keys.WithHelp("↑↓/jk", "이동")), keys.NewBinding(keys.WithKeys("/"), keys.WithHelp("/", "검색")), keys.NewBinding(keys.WithKeys("r"), keys.WithHelp("r", "갱신")), keys.NewBinding(keys.WithKeys("q"), keys.WithHelp("q", "종료")), keys.NewBinding(keys.WithKeys("v"), keys.WithHelp("v", "미등록 sandbox (읽기 전용)")), keys.NewBinding(keys.WithKeys("t"), keys.WithHelp("t", "Templates 포커스")), keys.NewBinding(keys.WithKeys("esc"), keys.WithHelp("Esc", "검색 취소")),
		keys.NewBinding(keys.WithKeys("enter"), keys.WithHelp("Enter", "작업")), keys.NewBinding(keys.WithKeys("u"), keys.WithHelp("u", "관리 도구 업데이트 (OS·Node·이미지 제외)")), keys.NewBinding(keys.WithKeys("s"), keys.WithHelp("s", "shell")), keys.NewBinding(keys.WithKeys("x"), keys.WithHelp("x", "중지")), keys.NewBinding(keys.WithKeys("d"), keys.WithHelp("d", "삭제")), keys.NewBinding(keys.WithKeys("c"), keys.WithHelp("c", "취소 요청")),
		keys.NewBinding(keys.WithKeys("R"), keys.WithHelp("R", "고정 대상 재시도")),
		keys.NewBinding(keys.WithKeys("n"), keys.WithHelp("n", "새 환경 생성")), keys.NewBinding(keys.WithKeys("a"), keys.WithHelp("a", "고급 build/load/prune/삭제")),
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
	if m.Focus == 2 {
		lines := []string{"Templates (읽기 전용)", connectionText("template", m.Snapshot.Templates)}
		for _, t := range m.Snapshot.TemplateRows {
			if t.ID == m.TemplateID {
				lines = append(lines, "Repository: "+t.Repository, "Tag: "+t.Tag, "ID: "+t.ID)
				break
			}
		}
		return safeText(strings.Join(lines, "\n"))
	}
	lines := []string{}
	for _, s := range m.rows() {
		if sandboxID(s) == m.ProfileID {
			lines = append(lines, compositionLines(s.Target.Entry)...)
			lines = append(lines, sectionLabel("RUNTIME"), tone(stateTone(s.RuntimeState), stateMark(s.RuntimeState)+" "+safeText(s.RuntimeState)), "")
			break
		}
	}
	prefix := lines
	lines = nil
	if w := m.selectedWorkspace(); w != nil {
		lines = append(lines, "Workspace: "+w.Path, "Runtime ID: "+w.ID)
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
		lines = append(lines, "Profile: "+sandboxID(s))
		if s.Target.Entry.EffectiveCategory() == config.CategoryContainer {
			lines = append(lines, "Container ID: "+s.Target.Entry.ContainerID)
		} else {
			lines = append(lines, "Sandbox: "+s.Target.Entry.Sandbox)
		}
		lines = append(lines, "Image: "+s.Target.Entry.Image, "Runtime: "+s.RuntimeState)
		if m.Unmanaged {
			lines = append(lines, "읽기 전용 · 관리 registry에 등록되지 않음")
		}
		if s.ImageMismatch {
			lines = append(lines, "이미지 구성 불일치 · 재사용 불가")
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
			}
		}
	}
	if len(lines) == 0 {
		return "선택 항목 없음"
	}
	lines = append(lines, "", "CONNECTIONS", connectionText("sbx", m.Snapshot.SBX), connectionText("Paseo", m.Snapshot.Paseo), connectionText("Docker", m.Snapshot.Docker))
	return strings.Join(append(prefix, safeText(strings.Join(lines, "\n"))), "\n")
}
func (m *Dashboard) versionContent() string {
	lines := []string{}
	for _, s := range m.rows() {
		if sandboxID(s) == m.ProfileID {
			lines = append(lines, versionLines("이미지 기준 (guest 미확인)", s.InitialVersions)...)
			if s.VersionCheck != nil {
				if !s.VersionCheck.CheckedAt.IsZero() {
					lines = append(lines, "버전 마지막 확인: "+s.VersionCheck.CheckedAt.Format(time.RFC3339), "마지막 확인:")
					lines = append(lines, versionLines("마지막 확인", s.VersionCheck.Versions)...)
				}
				lines = append(lines, versionLines("버전 확인 오류", s.VersionCheck.Errors)...)
			}
			if s.Update != nil {
				lines = append(lines, versionLines("업데이트 마지막 확인", s.Update.Versions)...)
				lines = append(lines, versionLines("업데이트 오류", s.Update.Errors)...)
				if s.Update.Attempt.IsZero() {
					lines = append(lines, "업데이트 버전 미확인")
				}
			}
		}
	}
	if len(lines) == 0 {
		return "버전 정보 없음"
	}
	return strings.Join(lines, "\n")
}
func (m *Dashboard) settingsContent() string {
	for _, s := range m.rows() {
		if sandboxID(s) == m.ProfileID {
			lines := runtimeSettings(s.Target.Entry)
			lines = append(lines, "Paseo 등록: "+s.Registration)
			if s.RegistrationError != "" {
				lines = append(lines, "Paseo 등록 확인 오류: "+s.RegistrationError)
			}
			return strings.Join(lines, "\n")
		}
	}
	return "설정 정보 없음"
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
	if colorsEnabled() {
		edge = edge.Foreground(lipgloss.Color(rule))
		heading = heading.Bold(true).Foreground(lipgloss.Color(secondary))
		if focused {
			edge = edge.Foreground(lipgloss.Color(accent))
			heading = heading.Foreground(lipgloss.Color(accent))
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
	for i := range lines {
		lines[i] = fillLine(lines[i], width, surface)
	}
	return strings.Join(lines, "\n")
}

func (m *Dashboard) panel(which, width, height int) string {
	width = max(1, width)
	height = max(1, height)
	title := []string{"1 Workspaces", "2 Sandbox / Container", "3 Templates", ""}[which]
	if which == 1 && m.Unmanaged {
		title = "2 미등록 · 읽기 전용"
	}
	if which == 3 {
		tabs := []string{"상세", "로그", "버전", "설정"}
		if m.busy() {
			tabs[1] += "•"
		}
		for i := range tabs {
			if i == m.rightTab {
				tabs[i] = "[" + tabs[i] + "]"
			}
		}
		title = strings.Join(tabs, " · ")
	}
	if m.Width < 80 || m.Height < 20 {
		title = fmt.Sprintf("%d/4 %s", which+1, strings.TrimPrefix(title, fmt.Sprintf("%d ", which+1)))
	}
	contentWidth, available, _, _ := sectionDimensions(width, height)
	var body string
	switch which {
	case 0:
		m.workspaces.SetSize(contentWidth, available)
		body = m.workspaces.View()
		if len(m.workspaces.Items()) == 0 {
			body = "Workspace 없음\n\nn 새 환경 생성\nr 다시 조회"
		}
	case 1:
		m.sandboxes.SetSize(contentWidth, available)
		body = m.sandboxes.View()
		if len(m.sandboxes.Items()) == 0 {
			body = "실행 환경 없음\n\nn Agent + Methodology\n  조합으로 생성"
		}
	case 2:
		m.templates.SetSize(contentWidth, available)
		body = m.templates.View()
		if len(m.templates.Items()) == 0 {
			body = "Template 없음 / 미조회\n\nt 목록 조회 · r 갱신"
		}
	case 3:
		if m.rightTab == 1 {
			toolbar := "FOLLOW · End 최신 · L 확장"
			if m.logPaused {
				toolbar = "PAUSED · End 추적 재개"
			}
			reserved := 0
			if available >= 3 {
				reserved = 1
			}
			source := m.logContent()
			offset := m.logs.YOffset()
			if m.logPaused {
				offset = reflowLogOffset(source, offset, m.logs.Width(), contentWidth)
			}
			m.logs.SetWidth(contentWidth)
			m.logs.SetHeight(max(1, available-reserved))
			m.logs.SetContent(inspectorText(source, contentWidth))
			if !m.logPaused {
				m.logs.GotoBottom()
			} else {
				m.logs.SetYOffset(offset)
			}
			body = m.logs.View()
			if reserved > 0 {
				body = tone(accent, ansi.Truncate(toolbar, contentWidth, "…")) + "\n" + body
			}
		} else {
			m.details.SetWidth(contentWidth)
			m.details.SetHeight(available)
			switch m.rightTab {
			case 0:
				m.details.SetContent(inspectorText(m.detailContent(), contentWidth))
			case 2:
				m.details.SetContent(inspectorText(safeText(m.versionContent()), contentWidth))
			case 3:
				m.details.SetContent(inspectorText(safeText(m.settingsContent()), contentWidth))
			}
			body = m.details.View()
		}
	}
	if which < 3 && m.Search != "" {
		empty := which == 0 && len(m.workspaces.Items()) == 0 || which == 1 && len(m.sandboxes.Items()) == 0 || which == 2 && len(m.templates.Items()) == 0
		if empty {
			body = "검색 결과 없음\n\nEsc 검색 해제"
		}
	}
	return renderSection(title, body, width, height, which == m.Focus)
}
func overlayPopup(base, popup string, width, height int) string {
	baseLines, popupLines := strings.Split(base, "\n"), strings.Split(popup, "\n")
	startY := max(0, (height-len(popupLines))/2)
	popupWidth := ansi.StringWidth(popupLines[0])
	startX := max(0, (width-popupWidth)/2)
	for i, line := range popupLines {
		y := startY + i
		if y >= len(baseLines) {
			break
		}
		left := ansi.Truncate(baseLines[y], startX, "")
		left += strings.Repeat(" ", max(0, startX-ansi.StringWidth(left)))
		rightStart := startX + popupWidth
		right := ""
		if rightStart < width {
			right = ansi.TruncateLeft(baseLines[y], rightStart, "")
		}
		baseLines[y] = left + line + right
	}
	return strings.Join(baseLines, "\n")
}
func (m *Dashboard) View() tea.View {
	creationPopup := m.creating != nil && !m.exitPrompt
	if creationPopup && m.creationFullScreen() {
		v := m.creating.wizard.View()
		if m.notice != "" {
			v.Content = overlayPopup(v.Content, renderSection("작업 결과", m.notice, max(1, m.Width), min(3, max(1, m.Height)), false), m.Width, m.Height)
		}
		return v
	}
	m.syncLists()
	width, height := max(1, m.Width), max(1, m.Height)
	header := m.header(width)
	m.help.SetWidth(width)
	m.help.ShowAll = m.Help
	footer := "u 업데이트 · s shell · x 중지 · d 삭제 · n 생성 · Enter 메뉴 · ? 도움말"
	if width < 80 {
		footer = "Tab 패널 · n 생성 · Enter 작업 · ? 도움말"
	}
	if m.Help {
		footer = m.help.View(dashboardKeys{})
	}
	if m.Searching || m.Search != "" {
		footer = "검색: " + m.Search + " · Esc 취소"
	}
	footerLines := strings.Count(footer, "\n") + 1
	if height < 8 {
		footer = ""
		footerLines = 0
	}
	headerLines := 1
	if height >= 28 && width >= 80 && !m.expanded {
		header += "\n" + m.contextLine(width) + "\n" + m.overview(width)
		headerLines = 5
		if !m.Help && !m.Searching && m.Search == "" {
			footer += "\nTab 패널 · ↑↓/jk 이동 · [ ] 탭 · / 검색 · f 확장 · L 로그 · q 종료"
			footerLines++
		}
	}
	if height <= 2 {
		headerLines = 0
	}
	available := max(1, height-headerLines-footerLines)
	var body string
	switch {
	case m.expanded:
		body = m.panel(m.Focus, width, available)
	case width >= 110 && height >= 28:
		left := min(32, max(26, width/5))
		middle := max(32, (width-left)/2-5)
		right := width - left - middle - 2
		workspaceHeight := max(5, available*3/5)
		if m.Focus == 2 {
			workspaceHeight = max(5, available*2/5)
		}
		stack := m.panel(0, left, workspaceHeight) + "\n" + m.panel(2, left, available-workspaceHeight)
		body = lipgloss.JoinHorizontal(lipgloss.Top, stack, " ", m.panel(1, middle, available), " ", m.panel(3, right, available))
	case width >= 80 && height >= 20:
		left := max(28, width/3)
		right := width - left - 1
		heights := [3]int{available / 4, available / 4, available / 4}
		heights[min(m.Focus, 2)] += available - heights[0] - heights[1] - heights[2]
		stack := m.panel(0, left, heights[0]) + "\n" + m.panel(1, left, heights[1]) + "\n" + m.panel(2, left, heights[2])
		body = lipgloss.JoinHorizontal(lipgloss.Top, stack, " ", m.panel(3, right, available))
	default:
		body = m.panel(m.Focus, width, available)
	}
	if creationPopup {
		body = overlayPopup(body, m.creationPopupFor(width, available), width, available)
	} else if content := m.managementContent(); content != "" {
		if m.confirm != nil {
			content = "Enter 실행(조건 충족 시) · Esc 취소\n" + content
		}
		popupWidth := min(70, max(1, width-4))
		popupContentWidth, _, _, _ := sectionDimensions(popupWidth, max(3, available-2))
		wrapped := content
		if !m.actionMenu {
			wrapped = ansi.Hardwrap(content, popupContentWidth, true)
		}
		popupHeight := min(max(3, available-2), max(3, strings.Count(wrapped, "\n")+3))
		contentWidth, contentHeight, _, _ := sectionDimensions(popupWidth, popupHeight)
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
		case len(m.pruneBlocked) > 0:
			title = "실행 차단"
		case m.confirm != nil:
			title = "실행 확인"
		case m.actionMenu:
			title = "작업 메뉴"
		}
		popup := renderSection(title, m.modal.View(), popupWidth, popupHeight, true)
		body = overlayPopup(body, popup, width, available)
	}
	// Creation popups show their own errors.
	if m.actionError != "" && !creationPopup {
		footer = m.actionError + " · " + footer
	} else if m.Error != "" {
		footer = tone(danger, safeText(m.Error)) + " · " + footer
	}
	if m.notice != "" {
		footer = m.notice + " · 아무 키: 닫기"
		if footerLines == 0 {
			body = overlayPopup(body, ansi.Truncate(footer, width, "…"), width, available)
		}
	}
	lines := []string{}
	if headerLines > 0 {
		lines = append(lines, strings.Split(header, "\n")...)
	}
	lines = append(lines, strings.Split(body, "\n")...)
	if footer != "" {
		lines = append(lines, strings.Split(footer, "\n")...)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = fillLine(lines[i], width, canvas)
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}
