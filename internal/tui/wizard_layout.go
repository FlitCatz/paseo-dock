package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"paseo-dock/internal/config"
)

func (m *Model) progressRibbon() string {
	steps := []string{"Agent", "Methodology", "Utility", "Review"}
	for i, label := range steps {
		text := fmt.Sprintf("%d %s", i+1, label)
		switch {
		case i == m.Step:
			steps[i] = tone(accent, "["+text+"]")
		case i < m.Step:
			steps[i] = tone(success, "✓ "+label)
		default:
			steps[i] = tone(secondary, text)
		}
	}
	return strings.Join(steps, "  ·  ")
}

func (m *Model) compositionSummary() string {
	s := m.selection()
	lines := compositionLines(config.Entry{Pairs: s.Pairs, Agent: s.Agent, Utilities: s.Utilities})
	lines = append(lines, sectionLabel("DESTINATION"), "Workspace: "+safeText(m.Workspace), "", "Category: "+string(m.Category))
	if s.Name != "" {
		lines = append(lines, "Container: "+safeText(s.Name))
	}
	if len(s.Pairs) == 0 && len(m.chosenAgents()) > 0 {
		lines = append(lines, "Agent: "+strings.Join(m.chosenAgents(), ", "), "Methodology: 선택 중")
	}
	lines = append(lines, "", sectionLabel("NEXT"), "Enter/Tab 다음 단계", "Esc/Shift+Tab 이전 단계", "최종 Enter에서만 생성")
	return strings.Join(lines, "\n")
}

// The continuous form remains the source of truth. Wider screens give the
// same form a live composition inspector; short screens retain clipAround.
func (m *Model) wizardLayout(body []string, focus int, footer []string) tea.View {
	if m.Step == 3 {
		return m.reviewLayout(body)
	}
	width, height := m.Width, m.Height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = max(30, len(body)+8)
	}
	header := []string{tone(accent, " PASEO DOCK") + " / Create environment  " + tone(secondary, fmt.Sprintf("STEP %d / 4", m.Step+1)), " Workspace: " + safeText(m.Workspace), m.progressRibbon()}
	var content string
	if width >= 110 && height >= 28 {
		available := height - len(header) - len(footer)
		left := width * 3 / 5
		_, ch, _, _ := sectionDimensions(left, available)
		visible := clipAround(body, focus, ch)
		form := renderSection("SELECTION  /  Space 선택", strings.Join(visible, "\n"), left, available, true)
		right := width - left - 1
		rw, _, _, _ := sectionDimensions(right, available)
		summary := renderSection("선택 요약", inspectorText(m.compositionSummary(), rw), right, available, false)
		content = lipgloss.JoinHorizontal(lipgloss.Top, form, " ", summary)
	} else {
		// Match the compact form's original row budget.
		if height < len(header)+len(footer)+1 {
			header = header[:min(1, max(0, height-2))]
			footer = footer[len(footer)-min(1, max(0, height-len(header)-1)):]
		}
		content = strings.Join(clipAround(body, focus, max(1, height-len(header)-len(footer))), "\n")
	}
	lines := append(append(header, strings.Split(content, "\n")...), footer...)
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = fillLine(lines[i], width, canvas)
		if !m.Color {
			lines[i] = ansi.Strip(lines[i])
		}
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}

// Confirmation owns the full width. Every wrapped setting is reachable before
// Enter commits; scroll keys never change selections or create resources.
func (m *Model) reviewLayout(body []string) tea.View {
	width, height := m.Width, m.Height
	if width <= 0 {
		width = 120
	}
	if height <= 0 {
		height = len(body) + 8
	}
	header := []string{tone(accent, " PASEO DOCK") + " / Create environment  STEP 4 / 4", m.progressRibbon()}
	if m.Category == config.CategorySandbox {
		header = []string{tone(accent, " PASEO DOCK") + " / Create sandbox", tone(success, "✓ Agent") + "  ·  " + tone(accent, "[Review]")}
	}
	footer := []string{"↑↓/jk · PgUp/PgDn 읽기 · Space 등록 변경 · Enter 생성 · Esc 이전"}
	if m.Error != "" {
		footer = append([]string{tone(danger, safeText(m.Error))}, footer...)
	}
	if height < 6 {
		header = nil
		footer = nil
	}
	frameHeight := max(1, height-len(header)-len(footer))
	cw, ch, _, _ := sectionDimensions(width, frameHeight)
	wrapped := strings.Split(ansi.Hardwrap(strings.Join(body, "\n"), cw, true), "\n")
	m.reviewRows, m.reviewHeight = len(wrapped), ch
	if m.reviewOffset < 0 {
		// Show the selected profile first; later settings remain scrollable.
		m.reviewOffset = 0
	}
	m.reviewOffset = min(max(0, m.reviewOffset), max(0, len(wrapped)-ch))
	end := min(len(wrapped), m.reviewOffset+ch)
	title := fmt.Sprintf("REVIEW  /  최종 확인  %d–%d / %d", m.reviewOffset+1, end, len(wrapped))
	content := renderSection(title, strings.Join(wrapped[m.reviewOffset:end], "\n"), width, frameHeight, true)
	lines := append(append(header, strings.Split(content, "\n")...), footer...)
	for i := range lines {
		lines[i] = fillLine(lines[i], width, canvas)
		if !m.Color {
			lines[i] = ansi.Strip(lines[i])
		}
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}
