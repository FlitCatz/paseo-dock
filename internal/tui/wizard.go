// Package tui implements the selection wizard; it never creates resources.
package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"io"
	"os"
	"paseo-dock/internal/config"
	"slices"
	"strings"
)

var ErrCancelled = errors.New("selection cancelled")

type Result struct {
	Selection config.Selection
	Category  config.Category
	Ports     []config.PortMapping
	Apply     bool
	Memory    string
}
type WizardFinishedMsg struct {
	Result Result
	Err    error
	source *Model
}
type Model struct {
	Catalog                  config.Catalog
	Category                 config.Category
	CodexUserNS              bool
	Ports                    []config.PortMapping
	Workspace, Memory        string
	Repository               config.Repository
	BuildFirst               bool
	PaseoHome                string
	memoryInput              textinput.Model
	editingMemory            bool
	Step, Cursor, agentIndex int
	agents, pairs, utilities map[string]bool
	selectedAgents           []string
	Result                   Result
	Confirmed                bool
	Err                      error
	Error                    string
	Width, Height            int
	Color                    bool
	embedded                 bool

	// Name and Connections carry container bundle choices; defaults are derived.
	Name        string
	Connections map[string]string
}

func NewEmbeddedModel(c config.Catalog, s config.Selection, workspace, memory string) *Model {
	m := NewModel(c, s, workspace, memory)
	m.embedded = true
	return m
}
func (m *Model) finish() tea.Cmd {
	if !m.embedded {
		return tea.Quit
	}
	result := m.Result
	result.Selection.Pairs = slices.Clone(result.Selection.Pairs)
	result.Selection.Utilities = slices.Clone(result.Selection.Utilities)
	err := m.Err
	return func() tea.Msg { return WizardFinishedMsg{Result: result, Err: err, source: m} }
}

func NewModel(c config.Catalog, s config.Selection, workspace, memory string) *Model {
	_, noColor := os.LookupEnv("NO_COLOR")
	m := &Model{Category: config.CategorySandbox, Step: -1, Catalog: c, Workspace: workspace, Memory: memory, Color: !noColor, agents: map[string]bool{}, pairs: map[string]bool{}, utilities: map[string]bool{}}
	m.BuildFirst, m.PaseoHome = true, "~/.paseo"
	if s.Schema != 0 {
		m.Category = s.Category
		if m.Category == "" {
			m.Category = config.CategoryContainer
		}
	}
	if s.Agent != "" {
		m.agents[s.Agent] = true
	}
	for _, p := range s.Pairs {
		m.agents[p.Agent] = true
		m.pairs[p.Agent+"-"+p.Methodology] = true
	}
	for _, u := range s.Utilities {
		m.utilities[u] = true
	}
	m.CodexUserNS = s.CodexUserNS
	m.Connections = map[string]string{}
	for agent, mode := range s.Connections {
		m.Connections[agent] = mode
	}
	// A derived default name follows later pair changes; only a chosen name sticks.
	if s.Name != config.DefaultBundleName(s.Pairs) {
		m.Name = s.Name
	}
	if m.Memory == "" {
		m.Memory = "2g"
	}
	m.memoryInput = textinput.New()
	m.memoryInput.Prompt = "Memory / sandbox: "
	return m
}
func (m *Model) Init() tea.Cmd { return nil }
func (m *Model) items() []config.Item {
	switch m.Step {
	case -1:
		return []config.Item{{ID: "sandbox", Label: "sandbox: agent 테스트"}, {ID: "container", Label: "container: 개발 환경"}}
	case 0:
		if m.Category == config.CategorySandbox {
			var items []config.Item
			for _, i := range m.Catalog.Agents {
				if i.ID == "codex" || i.ID == "claude" {
					items = append(items, i)
				}
			}
			return items
		}
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
	s := config.Selection{Schema: 2, Category: m.Category, CodexUserNS: m.CodexUserNS}
	if m.Category == config.CategorySandbox {
		chosen := m.chosenAgents()
		if len(chosen) == 1 {
			s.Agent = chosen[0]
		}
		return s
	}
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
	s.Name = m.Name
	s.UseBundle()
	for agent, mode := range m.Connections {
		if m.agents[agent] {
			if s.Connections == nil {
				s.Connections = map[string]string{}
			}
			s.Connections[agent] = mode
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
func (m *Model) userNSEligible() bool {
	if m.Category != config.CategoryContainer {
		return false
	}
	for _, pair := range m.selection().Pairs {
		if pair.Agent == "codex" {
			return true
		}
	}
	return false
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
		return m, m.finish()
	}
	if m.editingMemory {
		switch k.Code {
		case tea.KeyEscape:
			m.editingMemory = false
			m.Error = ""
		case tea.KeyEnter:
			value := strings.TrimSpace(m.memoryInput.Value())
			if value == "" {
				value = "2g"
			}
			if err := config.ValidateMemory(config.CategorySandbox, value); err != nil {
				m.Error = err.Error()
				return m, nil
			}
			m.Memory, m.editingMemory, m.Error = value, false, ""
		default:
			var cmd tea.Cmd
			m.memoryInput, cmd = m.memoryInput.Update(msg)
			return m, cmd
		}
		return m, nil
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
	if m.Step == -1 {
		switch k.Code {
		case tea.KeyEscape:
			m.Err = ErrCancelled
			return m, m.finish()
		case tea.KeyUp, tea.KeyDown:
			m.Cursor = 1 - m.Cursor
		case tea.KeySpace: // Enter accepts the focused category.
		case tea.KeyEnter:
			category := config.Category(m.items()[m.Cursor].ID)
			if category != m.Category {
				m.agents = map[string]bool{}
				m.pairs = map[string]bool{}
				m.utilities = map[string]bool{}
				m.CodexUserNS = false
				m.Result.Selection.CodexUserNS = false
			}
			m.Category = category
			m.Step = 0
			m.Cursor = 0
		}
		return m, nil
	}
	switch k.Code {
	case 'm':
		if k.Mod == 0 && m.Category == config.CategorySandbox && m.Step == 3 {
			m.editingMemory = true
			m.memoryInput.SetValue(m.Memory)
			return m, m.memoryInput.Focus()
		}
	case 'u':
		if k.Mod == 0 && m.Step == 3 && m.userNSEligible() {
			m.CodexUserNS = !m.CodexUserNS
			m.Result.Selection.CodexUserNS = m.CodexUserNS
		}
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		if k.Mod == 0 && m.Step == 3 && m.Category == config.CategoryContainer {
			m.cycleConnection(int(k.Code - '1'))
		}
	case tea.KeyEscape:
		switch m.Step {
		case 0:
			m.Step = -1
			m.Cursor = 0
			return m, nil
		case 1:
			if m.agentIndex > 0 {
				m.agentIndex--
			} else {
				m.Step = 0
			}
		case 2:
			if m.Category == config.CategorySandbox {
				s, err := m.Catalog.Normalize(m.selection())
				if err != nil {
					m.Error = err.Error()
					break
				}
				m.Result.Selection = s
				m.Step = 3
			} else {
				m.Step = 1
			}
			m.agentIndex = len(m.selectedAgents) - 1
		case 3:
			if m.Category == config.CategorySandbox {
				m.Step = 0
			} else {
				m.Step = 2
			}
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
			if m.Category == config.CategorySandbox {
				selected := m.agents[i.ID]
				m.agents = map[string]bool{}
				m.agents[i.ID] = !selected
			} else {
				m.agents[i.ID] = !m.agents[i.ID]
			}
			if m.Category != config.CategoryContainer || !m.agents["codex"] {
				m.CodexUserNS = false
				m.Result.Selection.CodexUserNS = false
			}
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
			if m.Category == config.CategorySandbox {
				s, err := m.Catalog.Normalize(m.selection())
				if err != nil {
					m.Error = err.Error()
					break
				}
				m.Result.Selection = s
				m.Step = 3
			} else {
				m.Step = 1
			}
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
			m.Result.Category = m.Category
			m.Result.Memory = ""
			if m.Category == config.CategorySandbox {
				if err := config.ValidateMemory(m.Category, m.Memory); err != nil {
					m.Error = err.Error()
					break
				}
				m.Result.Memory = m.Memory
			}
			if m.Category == config.CategoryContainer {
				m.Result.Ports = slices.Clone(m.Ports)
			}
			m.Confirmed = true
			return m, m.finish()
		}
	}
	return m, nil
}
func (m *Model) View() tea.View {
	if m.editingMemory {
		return m.boundedView([]string{"Sandbox 메모리 설정", m.memoryInput.View(), m.Error, "Enter 적용 · Esc 취소"})
	}
	if m.Step == -1 {
		width, height := m.Width, m.Height
		if width <= 0 || height <= 0 {
			width, height = 80, 24
		}
		v := tea.NewView(popupScreen(m.categoryPopup(width, height), width, height))
		v.AltScreen = true
		return v
	}
	if m.Category == config.CategorySandbox {
		lines := []string{"Category: sandbox · Agent 테스트", "Workspace: " + m.Workspace}
		if m.Step == 0 {
			for index, i := range m.items() {
				mark := "○ "
				if m.agents[i.ID] {
					mark = "✓ "
				}
				if index == m.Cursor {
					mark = "› " + mark
				}
				lines = append(lines, mark+i.Label)
			}
		} else {
			flow := "Existing template → sandbox"
			if m.BuildFirst {
				flow = "Build → template load → sandbox"
			}
			lines = append(lines, "최종 확인 · Agent: "+m.Result.Selection.Agent, "Image source: official stable package · Confirmed version: unresolved; recorded during build", "Mount: "+m.Workspace+" (read/write)", "Network: SBX policy · Ports: none", flow+" · CPU: 2 · Memory: "+m.Memory)
			if m.Repository.URL != "" {
				lines = append(lines, "Repository: "+m.Repository.URL, "Ref: "+m.Repository.Ref)
			}
			mark := "○ "
			if m.Result.Apply {
				mark = "✓ "
			}
			lines = append(lines, mark+"Paseo provider 등록 및 reload", "Enter 생성 · Space 등록 변경 · m 메모리 변경")
		}
		if m.Error != "" {
			lines = append(lines, m.Error)
		}
		lines = append(lines, "Space 선택 · Enter 다음 · Esc 이전 · Ctrl+C 취소")
		return m.boundedView(lines)
	}

	sb := &selectionBody{color: m.Color}
	paint := sb.paint
	section := func(title, hint string, items []config.Item, active bool, selected func(config.Item) bool, compatible func(config.Item) bool) {
		sb.section(title, hint, items, active, m.Cursor, selected, compatible)
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
	body, focus := sb.lines, sb.focus
	if m.Step == 3 {
		body = append(body, "", paint(purple, "┃  4  최종 확인"))
		for _, profile := range m.Result.Selection.Profiles() {
			body = append(body, "┃    "+paint(green, "✓ "+profile))
		}
		body = append(body, "┃    Utilities: "+strings.Join(m.Result.Selection.Utilities, ", "))
		if m.Result.Selection.Name != "" {
			body = append(body, "┃    Container: "+m.Result.Selection.Name+" (선택한 모든 조합을 container 하나에 설치)")
		}
		body = append(body, m.connectionLines()...)
		flow := "Existing image → Docker container"
		if m.BuildFirst {
			flow = "Build → Docker store"
		}
		body = append(body, "┃    "+flow+" · 호스트 자원 공유")
		if m.Repository.URL != "" {
			body = append(body, "┃    Repository: "+m.Repository.URL, "┃    Ref: "+m.Repository.Ref)
		}
		body = append(body, "┃    Category: container", "┃    Image source: versions.lock.json · Confirmed versions: unresolved in preview; pinned values recorded during build", "┃    Mount: "+m.Workspace+" (read/write)", "┃    Network: bridge", "┃    Ports: "+portText(m.Ports))
		if m.userNSEligible() {
			state := "off (default Docker policy)"
			if m.CodexUserNS {
				state = "on (codex-userns-v1)"
			}
			body = append(body, "┃    u: Codex workspace-write isolation · "+state,
				"┃    Opt-in expands kernel syscall access for all guest processes in each Codex container",
				"┃    Opt-in: no privileged mode; no added capabilities; no-new-privileges enabled")
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
		body = append(body, "┃  "+paint(pink, "›")+" "+paint(green, mark+" Paseo provider 등록 및 reload"), "┃    "+paint(muted, "Enter: "+flow+" → 생성 확정"))
		if m.embedded {
			body = append(body, "┃    Paseo: "+m.PaseoHome+"/config.json · backup / merge", "┃    다른 workspace provider 유지")
		}
	}
	agents := strings.Join(chosen, ", ")
	if agents == "" {
		agents = "선택 전"
	}
	header := []string{paint("38;5;81;1", "PASEO DOCK") + "  " + paint(muted, fmt.Sprintf("STEP %d / 4", m.Step+1)), paint(muted, "Workspace: "+m.Workspace), paint(muted, "Agent: "+agents)}
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
		body = clipAround(body, focus, max(1, m.Height-len(header)-len(footer)))
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

// categoryPopup is the category step as a box in the agent-selection style.
// Enter accepts the focused row, so the check mark follows the cursor.
func (m *Model) categoryPopup(width, height int) string {
	sb := selectionBody{color: m.Color}
	items := m.items()
	sb.section("Category · 실행 환경 선택", "Workspace: "+m.Workspace, items, true, m.Cursor, func(i config.Item) bool { return i.ID == items[m.Cursor].ID }, func(config.Item) bool { return true })
	footer := []string{"", sb.paint(muted, "↑/↓ 이동 · Enter 다음 · Esc 취소")}
	if m.Error != "" {
		footer = append([]string{sb.paint(errorRed, m.Error)}, footer...)
	}
	return selectionPopup("실행 환경 선택", sb, footer, width, height)
}

func Run(c config.Catalog, s config.Selection, workspace, memory string, in io.Reader, out io.Writer, apply ...bool) (Result, error) {
	return RunWithPorts(c, s, workspace, memory, nil, in, out, apply...)
}

// RunWithPorts preserves explicit publications through standalone confirmation.
func RunWithPorts(c config.Catalog, s config.Selection, workspace, memory string, ports []config.PortMapping, in io.Reader, out io.Writer, apply ...bool) (Result, error) {
	o := CreationOptions{Workspace: workspace, Memory: memory, Ports: ports, BuildFirst: true}
	if len(apply) > 0 {
		o.Apply = apply[0]
	}
	return RunCreation(c, s, o, in, out)
}

type CreationOptions struct {
	Workspace, Memory, PaseoHome      string
	Ports                             []config.PortMapping
	Repository                        config.Repository
	Apply, BuildFirst, SelectCategory bool
}

func RunCreation(c config.Catalog, s config.Selection, o CreationOptions, in io.Reader, out io.Writer) (Result, error) {
	m := NewModel(c, s, o.Workspace, o.Memory)
	m.Ports = slices.Clone(o.Ports)
	m.Repository, m.BuildFirst, m.Result.Apply = o.Repository, o.BuildFirst, o.Apply
	if o.PaseoHome != "" {
		m.PaseoHome = o.PaseoHome
	}
	if o.SelectCategory && m.Category == config.CategoryContainer {
		m.Cursor = 1
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

func (m *Model) boundedView(lines []string) tea.View {
	if m.Height > 0 && len(lines) > m.Height {
		if m.Step == 3 {
			lines = lines[len(lines)-m.Height:]
		} else {
			focus := 2 + m.Cursor
			start := min(max(0, focus-m.Height+1), len(lines)-m.Height)
			lines = lines[start : start+m.Height]
		}
	}
	if m.Width > 0 {
		for i := range lines {
			lines[i] = ansi.Truncate(lines[i], m.Width, "…")
		}
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}

// cycleConnection advances the Nth chosen agent to its next supported Paseo
// connection mode and revalidates the confirmed selection.
func (m *Model) cycleConnection(index int) {
	agents := m.chosenAgents()
	if index < 0 || index >= len(agents) {
		return
	}
	agent := agents[index]
	modes := config.ConnectionModes(agent)
	if len(modes) < 2 {
		return
	}
	current := config.DefaultConnection(agent)
	if mode, ok := m.Connections[agent]; ok {
		current = mode
	}
	m.Connections[agent] = modes[(slices.Index(modes, current)+1)%len(modes)]
	s, err := m.Catalog.Normalize(m.selection())
	if err != nil {
		m.Error = err.Error()
		return
	}
	m.Error = ""
	m.Result.Selection = s
}

// connectionLines shows each chosen agent's Paseo connection on the final step.
func (m *Model) connectionLines() []string {
	var lines []string
	for i, agent := range m.chosenAgents() {
		mode := config.DefaultConnection(agent)
		if chosen, ok := m.Result.Selection.Connections[agent]; ok {
			mode = chosen
		}
		hint := ""
		if modes := config.ConnectionModes(agent); len(modes) > 1 {
			hint = fmt.Sprintf(" · %d: %s", i+1, strings.Join(modes, "/"))
		}
		lines = append(lines, "┃    Paseo "+agent+": "+mode+hint)
	}
	return lines
}
