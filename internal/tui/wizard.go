// Package tui implements the selection wizard; it never creates resources.
package tui

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"fmt"
	"io"
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
}

func NewModel(c config.Catalog, s config.Selection, workspace, memory string) *Model {
	m := &Model{Catalog: c, Workspace: workspace, Memory: memory, agents: map[string]bool{}, pairs: map[string]bool{}, utilities: map[string]bool{}}
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
	for _, a := range m.selectedAgents {
		if !slices.Contains(i.Agents, a) {
			return false
		}
	}
	return true
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	if k.String() == "ctrl+c" {
		m.Err = ErrCancelled
		return m, tea.Quit
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
	var b strings.Builder
	fmt.Fprint(&b, "Paseo SBX · 선택한 조합만 생성\n\n")
	fmt.Fprintf(&b, "Workspace: %s\nMemory: %s / sandbox · CPU: 2\n\n", m.Workspace, m.Memory)
	if m.Step == 3 {
		fmt.Fprintln(&b, "생성할 sandbox:")
		for _, p := range m.Result.Selection.Profiles() {
			fmt.Fprintln(&b, "  •", p)
		}
		us := strings.Join(m.Result.Selection.Utilities, ", ")
		if us == "" {
			us = "없음"
		}
		fmt.Fprintln(&b, "\nUtilities:", us)
		if slices.Contains(m.Result.Selection.Utilities, "headroom") {
			fmt.Fprintln(&b, "Headroom: sandbox 내부 Python 런타임 + MCP 도구 연결")
		}
		mark := " "
		if m.Result.Apply {
			mark = "x"
		}
		fmt.Fprintf(&b, "\n[%s] Paseo에 선택한 provider 등록 및 reload (Space로 변경)\n", mark)
		fmt.Fprintln(&b, "\nEnter: 이미지 빌드 → template 로드 → sandbox 생성")
	} else {
		title := []string{"Agent 선택", "Methodology 선택", "Utility 선택 (선택 사항)"}[m.Step]
		if m.Step == 1 {
			title += " · " + m.selectedAgents[m.agentIndex]
		}
		fmt.Fprintln(&b, title)
		for idx, i := range m.items() {
			selected := false
			switch m.Step {
			case 0:
				selected = m.agents[i.ID]
			case 1:
				selected = m.pairs[m.selectedAgents[m.agentIndex]+"-"+i.ID]
			case 2:
				selected = m.utilities[i.ID]
			}
			cursor := " "
			if idx == m.Cursor {
				cursor = "›"
			}
			mark := " "
			if selected {
				mark = "x"
			}
			label := i.Label
			if !m.compatible(i) {
				label += " (지원하지 않는 agent 포함)"
			}
			fmt.Fprintf(&b, "%s [%s] %s\n", cursor, mark, label)
			if m.Step == 2 && idx == m.Cursor && i.Note != "" {
				fmt.Fprintln(&b, "      "+i.Note)
			}
		}
	}
	if m.Error != "" {
		fmt.Fprintln(&b, "\n"+m.Error)
	}
	fmt.Fprintln(&b, "\n↑/↓ 이동 · Space 선택 · Enter 다음 · Esc 이전 · Ctrl+C 취소")
	v := tea.NewView(b.String())
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
