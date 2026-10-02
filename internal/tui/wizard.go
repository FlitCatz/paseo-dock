// Package tui implements the selection wizard; it never creates resources.
package tui

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"io"
	"os"
	"paseo-sbx/internal/config"
	"slices"
	"strings"
)

var ErrCancelled = errors.New("selection cancelled")

type Result struct {
	Selection config.Selection
	Apply     bool
}
type Model struct {
	Catalog                  config.Catalog
	Workspace, Memory        string
	Step, Cursor, agentIndex int
	agents, pairs, utilities map[string]bool
	selectedAgents           []string
	Result                   Result
	Confirmed                bool
	Err                      error
	Error                    string
	Width, Height            int
	Color                    bool
}

func NewModel(c config.Catalog, s config.Selection, workspace, memory string) *Model {
	_, noColor := os.LookupEnv("NO_COLOR")
	m := &Model{Catalog: c, Workspace: workspace, Memory: memory, Color: !noColor, agents: map[string]bool{}, pairs: map[string]bool{}, utilities: map[string]bool{}}
	for _, p := range s.Pairs {
		m.agents[p.Agent] = true
		m.pairs[p.Agent+"-"+p.Methodology] = true
	}
	for _, u := range s.Utilities {
		m.utilities[u] = true
	}
	return m
}
func (m *Model) Init() tea.Cmd { return nil }
func (m *Model) items() []config.Item {
	switch m.Step {
	case 0:
		return m.Catalog.Agents
	case 1:
		var out []config.Item
		for _, i := range m.Catalog.Methodologies {
			if slices.Contains(i.Agents, m.selectedAgents[m.agentIndex]) {
				out = append(out, i)
			}
		}
		return out
	case 2:
		return m.Catalog.Utilities
	}
	return nil
}
func (m *Model) selection() config.Selection {
	s := config.Selection{Schema: 1}
	for _, a := range m.Catalog.Agents {
		if m.agents[a.ID] {
			for _, method := range m.Catalog.Methodologies {
				if m.pairs[a.ID+"-"+method.ID] {
					s.Pairs = append(s.Pairs, config.Pair{Agent: a.ID, Methodology: method.ID})
				}
			}
		}
	}
	for _, u := range m.Catalog.Utilities {
		if m.utilities[u.ID] {
			s.Utilities = append(s.Utilities, u.ID)
		}
	}
	return s
}
func (m *Model) compatible(i config.Item) bool {
	if m.Step != 2 {
		return true
	}
	for _, a := range m.chosenAgents() {
		if !slices.Contains(i.Agents, a) {
			return false
		}
	}
	return true
}
func (m *Model) chosenAgents() []string {
	var chosen []string
	for _, agent := range m.Catalog.Agents {
		if m.agents[agent.ID] {
			chosen = append(chosen, agent.ID)
		}
	}
	return chosen
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.Width, m.Height = size.Width, size.Height
		return m, nil
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	if k.String() == "ctrl+c" {
		m.Err = ErrCancelled
		return m, tea.Quit
	}
	if k.Code == 'x' && k.Mod == 0 {
		k.Code = tea.KeySpace
	}
	if k.Code == tea.KeyTab {
		if k.Mod&tea.ModShift != 0 {
			k.Code = tea.KeyEscape
		} else if m.Step < 3 {
			k.Code = tea.KeyEnter
		}
	}
	m.Error = ""
	switch k.Code {
	case tea.KeyEscape:
		switch m.Step {
		case 0:
			m.Err = ErrCancelled
			return m, tea.Quit
		case 1:
			if m.agentIndex > 0 {
				m.agentIndex--
			} else {
				m.Step = 0
			}
		case 2:
			m.Step = 1
			m.agentIndex = len(m.selectedAgents) - 1
		case 3:
			m.Step = 2
		}
		m.Cursor = 0
	case tea.KeyUp:
		if m.Cursor > 0 {
			m.Cursor--
		}
	case tea.KeyDown:
		if m.Cursor+1 < len(m.items()) {
			m.Cursor++
		}
	case tea.KeySpace:
		if m.Step == 3 {
			m.Result.Apply = !m.Result.Apply
			break
		}
		items := m.items()
		if len(items) == 0 {
			break
		}
		i := items[m.Cursor]
		if !m.compatible(i) && !m.utilities[i.ID] {
			m.Error = "선택한 agent가 모두 지원하는 utility를 선택하세요."
			break
		}
		switch m.Step {
		case 0:
			m.agents[i.ID] = !m.agents[i.ID]
		case 1:
			p := m.selectedAgents[m.agentIndex] + "-" + i.ID
			m.pairs[p] = !m.pairs[p]
		case 2:
			m.utilities[i.ID] = !m.utilities[i.ID]
		}
	case tea.KeyEnter:
		switch m.Step {
		case 0:
			m.selectedAgents = nil
			for _, a := range m.Catalog.Agents {
				if m.agents[a.ID] {
					m.selectedAgents = append(m.selectedAgents, a.ID)
				}
			}
			if len(m.selectedAgents) == 0 {
				m.Error = "Agent를 하나 이상 선택하세요."
				break
			}
			m.Step = 1
			m.agentIndex = 0
			m.Cursor = 0
		case 1:
			chosen := false
			for _, i := range m.items() {
				if m.pairs[m.selectedAgents[m.agentIndex]+"-"+i.ID] {
					chosen = true
				}
			}
			if !chosen {
				m.Error = "Methodology를 하나 이상 선택하세요."
				break
			}
			if m.agentIndex+1 < len(m.selectedAgents) {
				m.agentIndex++
			} else {
				m.Step = 2
			}
			m.Cursor = 0
		case 2:
			s, err := m.Catalog.Normalize(m.selection())
			if err != nil {
				m.Error = err.Error()
				break
			}
			m.Result.Selection = s
			m.Step = 3
			m.Cursor = 0
		case 3:
			// Revalidate the actual result immediately before accepting it.
			s, err := m.Catalog.Normalize(m.selection())
			if err != nil {
				m.Error = err.Error()
				break
			}
			m.Result.Selection = s
			m.Confirmed = true
			return m, tea.Quit
		}
	}
	return m, nil
}
func (m *Model) View() tea.View {
	paint := func(code, text string) string {
		if !m.Color {
			return text
		}
		return "\x1b[" + code + "m" + text + "\x1b[0m"
	}
	const purple = "38;5;141;1"
	const green = "38;5;84"
	const pink = "38;5;213;1"
	const muted = "38;5;245"
	var body []string
	focus := 0
	section := func(title, hint string, items []config.Item, active bool, selected func(config.Item) bool, compatible func(config.Item) bool) {
		if len(body) > 0 {
			body = append(body, "")
		}
		border := paint(muted, "│")
		if active {
			border = paint(purple, "┃")
			focus = len(body)
		}
		body = append(body, border+"  "+paint(purple, title))
		if hint != "" {
			body = append(body, border+"  "+paint(muted, hint))
		}
		for index, item := range items {
			mark, cursor, color := "○", " ", ""
			if selected(item) {
				mark, color = "✓", green
			}
			label := item.Label
			if !compatible(item) {
				color = muted
				label += " · 지원 대상 아님"
			}
			if active && index == m.Cursor {
				cursor = paint(pink, "›")
				focus = len(body)
				if compatible(item) {
					color = green
				}
			}
			body = append(body, border+"  "+cursor+" "+paint(color, mark+" "+label))
			if active && index == m.Cursor && item.Note != "" {
				body = append(body, border+"      "+paint(muted, item.Note))
			}
		}
	}
	allow := func(config.Item) bool { return true }
	section("1  Agent", "사용할 에이전트를 선택하세요. 여러 개 선택할 수 있습니다.", m.Catalog.Agents, m.Step == 0, func(i config.Item) bool { return m.agents[i.ID] }, allow)
	chosen := m.chosenAgents()
	if len(chosen) == 0 {
		section("2  Methodology", "Agent를 선택하면 에이전트별로 방법론을 고릅니다.", m.Catalog.Methodologies, false, func(config.Item) bool { return false }, allow)
	} else {
		for index, agentID := range chosen {
			agent, _ := m.Catalog.Agent(agentID)
			var methods []config.Item
			for _, method := range m.Catalog.Methodologies {
				if slices.Contains(method.Agents, agentID) {
					methods = append(methods, method)
				}
			}
			section("2  Methodology · "+agent.Label, "필요한 방법론을 하나씩 선택하세요. 여러 개 선택할 수 있습니다.", methods, m.Step == 1 && index == m.agentIndex, func(i config.Item) bool { return m.pairs[agentID+"-"+i.ID] }, allow)
		}
	}
	utilityCompatible := func(i config.Item) bool {
		for _, agent := range chosen {
			if !slices.Contains(i.Agents, agent) {
				return false
			}
		}
		return true
	}
	section("3  Utility", "선택 사항 · 모든 에이전트가 지원하는 항목만 선택할 수 있습니다.", m.Catalog.Utilities, m.Step == 2, func(i config.Item) bool { return m.utilities[i.ID] }, utilityCompatible)
	if m.Step == 3 {
		body = append(body, "", paint(purple, "┃  4  최종 확인"))
		for _, profile := range m.Result.Selection.Profiles() {
			body = append(body, "┃    "+paint(green, "✓ "+profile))
		}
		if slices.Contains(m.Result.Selection.Utilities, "headroom") {
			body = append(body, "┃    "+paint(muted, "Headroom: sandbox 내부 Python 런타임 + MCP 도구 연결"))
		}
		for _, pair := range m.Result.Selection.Pairs {
			if pair.Methodology == "openspec" {
				body = append(body, "┃    "+paint(muted, "OpenSpec: workspace에 spec·workflow 파일 초기화"))
				break
			}
		}
		mark := "○"
		if m.Result.Apply {
			mark = "✓"
		}
		focus = len(body)
		body = append(body, "┃  "+paint(pink, "›")+" "+paint(green, mark+" Paseo provider 등록 및 reload"), "┃    "+paint(muted, "Enter: 이미지 빌드 → template 로드 → sandbox 생성"))
	}
	agents := strings.Join(chosen, ", ")
	if agents == "" {
		agents = "선택 전"
	}
	header := []string{paint("38;5;81;1", "PASEO SBX") + "  " + paint(muted, fmt.Sprintf("STEP %d / 4", m.Step+1)), paint(muted, "Workspace: "+m.Workspace), paint(muted, fmt.Sprintf("Memory: %s / sandbox · CPU: 2 · Agent: %s", m.Memory, agents))}
	footer := []string{"", paint(muted, "↑/↓ 이동 · Space/x 선택 · Enter/Tab 다음 · Esc/Shift+Tab 이전"), paint(muted, "Ctrl+C 취소 · 최종 확인 화면에서 Enter로 생성")}
	if m.Error != "" {
		footer = append([]string{paint("38;5;203", m.Error)}, footer...)
	}
	if m.Height > 0 {
		// Keep the active row visible even in a very short terminal.
		if m.Height < len(header)+len(footer)+1 {
			header = header[:min(1, max(0, m.Height-2))]
			footer = footer[len(footer)-min(1, max(0, m.Height-len(header)-1)):]
		}
		available := max(1, m.Height-len(header)-len(footer))
		if len(body) > available {
			start := min(max(0, focus-available+min(3, available)), len(body)-available)
			body = body[start : start+available]
		}
	}
	lines := append(append(header, body...), footer...)
	if m.Width > 0 {
		for i, line := range lines {
			lines[i] = ansi.Truncate(line, m.Width, "…")
		}
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}
func Run(c config.Catalog, s config.Selection, workspace, memory string, in io.Reader, out io.Writer, apply ...bool) (Result, error) {
	m := NewModel(c, s, workspace, memory)
	if len(apply) > 0 {
		m.Result.Apply = apply[0]
	}
	_, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out)).Run()
	if err != nil {
		return Result{}, err
	}
	if m.Err != nil {
		return Result{}, m.Err
	}
	if !m.Confirmed {
		return Result{}, ErrCancelled
	}
	return m.Result, nil
}
