package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
)

// Counts are observations, not provider registration or inferred health.
func (m *Dashboard) overview(width int) string {
	running, stopped, unknown := 0, 0, 0
	for _, w := range m.Snapshot.Workspaces {
		for _, s := range w.Sandboxes {
			switch s.RuntimeState {
			case "running":
				running++
			case "stopped":
				stopped++
			default:
				unknown++
			}
		}
	}
	values := []struct{ label, value, color string }{
		{"WORKSPACES", fmt.Sprintf("%02d registered", len(m.Snapshot.Workspaces)), accent},
		{"RUNTIMES", fmt.Sprintf("%d running · %d stopped", running, stopped), success},
		{"ATTENTION", fmt.Sprintf("%d unknown / missing", unknown), warning},
		{"JOBS", fmt.Sprintf("%d active · %d retained", m.activeJobs(), len(m.jobs)), violet},
	}
	var cards []string
	for i, v := range values {
		w := (width - 3) / 4
		if i == 3 {
			w = width - 3 - 3*w
		}
		cards = append(cards, renderSection(v.label, tone(v.color, v.value), w, 3, false))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cards[0], " ", cards[1], " ", cards[2], " ", cards[3])
}

func (m *Dashboard) activeJobs() int {
	n := 0
	for _, j := range m.jobs {
		if j.state == "running" || j.state == "queued" || j.state == "cancel requested" {
			n++
		}
	}
	return n
}

func connectionBadge(name string, c operations.Connection) string {
	state := c.Status
	if state == "" {
		state = "unknown"
	}
	if c.Error != "" {
		state = "error"
	}
	return tone(stateTone(state), stateMark(state)+" "+name+" "+safeText(state))
}

func (m *Dashboard) header(width int) string {
	left := tone(accent, " PASEO DOCK") + tone(secondary, " / Dashboard")
	right := connectionBadge("sbx", m.Snapshot.SBX) + "  " + connectionBadge("Paseo", m.Snapshot.Paseo) + "  " + connectionBadge("Docker", m.Snapshot.Docker) + " "
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return fillLine(tone(accent, "PASEO DOCK")+"  "+right, width, surface)
	}
	return fillLine(left+strings.Repeat(" ", gap)+right, width, surface)
}

func (m *Dashboard) contextLine(width int) string {
	text := " Workspace: "
	if w := m.selectedWorkspace(); w != nil {
		text += safeText(w.Path)
	} else {
		text += "선택 없음 · n 새 환경 생성"
	}
	if m.Unmanaged {
		text += " · 미등록 / 읽기 전용"
	}
	if m.Error != "" {
		text += " · 확인 불가: " + safeText(m.Error)
	}
	if !m.LastSuccess.IsZero() {
		text += " · 마지막 성공: " + m.LastSuccess.Format("15:04:05")
	}
	return fillLine(tone(secondary, text), width, canvas)
}

// Actual runtime composition must not be replaced with the workspace's saved
// choices: those can describe a different bundle or the next creation.
func compositionLines(e config.Entry) []string {
	lines := []string{sectionLabel("COMPOSITION  /  Agent → Methodology")}
	pairs := e.Pairs
	if len(pairs) == 0 && e.Agent != "" {
		pairs = []config.Pair{{Agent: e.Agent, Methodology: e.Tool}}
	}
	if len(pairs) == 0 {
		lines = append(lines, tone(secondary, "등록된 조합 정보 없음"))
	}
	for _, p := range pairs {
		method := p.Methodology
		if method == "" {
			method = "native"
		}
		lines = append(lines, tone(accent, safeText(p.Agent))+" → "+safeText(method))
	}
	utilities := strings.Join(e.Utilities, ", ")
	if utilities == "" {
		utilities = "없음"
	}
	lines = append(lines, "", sectionLabel("UTILITY"), safeText(utilities), "")
	return lines
}

func inspectorText(content string, width int) string {
	// Wrap identifiers and paths so no information disappears behind an ellipsis.
	return ansi.Hardwrap(content, max(1, width), true)
}
