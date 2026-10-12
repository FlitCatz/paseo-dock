package tui

import (
	"fmt"
	"os"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Creation owns only input and selection. Execution always uses the dashboard queue.
type creation struct {
	path, memory, ports textinput.Model
	picker              filepicker.Model
	focus               int
	wizard              *Model
	request             operations.Request
}
type savedSelectionMsg struct {
	request   operations.Request
	selection config.Selection
	err       error
	flow      uint64
}

// Async confirmations belong to the input flow that requested them. Navigation
// into another action or abandonment advances the generation independently of
// job execution, so an old preview still finishes without taking over a modal.
func (m *Dashboard) newFlow() {
	m.flowGeneration++
	m.pruneBlocked = nil
}
func (m *Dashboard) ownsFlow(flow uint64) bool {
	return flow == m.flowGeneration && m.confirm == nil && m.creating == nil && !m.actionMenu && !m.advancedMenu && !m.exitPrompt
}

var memoryPattern = regexp.MustCompile(`^[1-9][0-9]*[kKmMgG]?[bB]?$`)

func (m *Dashboard) startCreation() tea.Cmd {
	m.newFlow()
	path, memory, ports := textinput.New(), textinput.New(), textinput.New()
	ports.Prompt = "Container ports (host:container, optional): "
	path.Prompt = "Workspace 경로: "
	memory.Prompt = "Memory / sandbox: "
	memory.SetValue("2g")
	picker := filepicker.New()
	picker.DirAllowed = true
	picker.FileAllowed = false
	picker.CurrentDirectory, _ = os.Getwd()
	if w := m.selectedWorkspace(); w != nil {
		path.SetValue(w.Path)
		picker.CurrentDirectory = w.Path
	}
	m.creating = &creation{path: path, memory: memory, ports: ports, picker: picker}
	m.advancedMenu = false
	m.actionMenu = false
	m.actionError = ""
	return tea.Batch(m.creating.path.Focus(), m.creating.picker.Init())
}

func (m *Dashboard) creationMessage(msg tea.Msg) (bool, tea.Cmd) {
	if v, ok := msg.(WizardFinishedMsg); ok {
		c := m.creating
		if c == nil || c.wizard == nil || v.source != c.wizard {
			return true, nil
		}
		m.creating = nil
		if v.Err != nil {
			if v.Err != ErrCancelled {
				m.actionError = safeText(v.Err.Error())
			}
			return true, nil
		}
		r := c.request
		r.Selection = v.Result.Selection
		r.Target.Category = v.Result.Category
		r.Ports = v.Result.Ports
		r.Apply = v.Result.Apply
		return true, m.enqueue(r)
	}
	if v, ok := msg.(savedSelectionMsg); ok {
		if !m.ownsFlow(v.flow) {
			return true, nil
		}
		if v.err != nil {
			m.actionError = safeText(v.err.Error())
			return true, nil
		}
		if v.request.Kind == operations.KindLoad && v.selection.Category == config.CategoryContainer {
			m.actionError = "container images are already in Docker store; template load is sandbox-only"
			return true, nil
		}
		s, err := m.Catalog.Normalize(v.selection)
		if err != nil {
			m.actionError = safeText(err.Error())
			return true, nil
		}
		v.request.Selection = s
		m.confirm = &confirmation{request: freezeRequest(v.request), label: "저장된 선택으로 " + string(v.request.Kind), warning: "선택된 조합과 utilities의 이미지 작업"}
		m.modal.GotoTop()
		return true, nil
	}
	c := m.creating
	if c == nil {
		return false, nil
	}
	switch msg.(type) {
	case pollMsg, snapshotMsg:
		return false, nil
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		if c.wizard != nil {
			c.wizard.Update(size)
		}
		c.path.SetWidth(max(1, size.Width-20))
		c.ports.SetWidth(max(1, size.Width-20))
		c.memory.SetWidth(max(1, size.Width-20))
		c.picker.SetHeight(max(1, size.Height-8))
		return false, nil
	}
	if c.wizard != nil {
		if _, ok := msg.(tea.KeyPressMsg); ok {
			_, cmd := c.wizard.Update(msg)
			return true, cmd
		}
		return false, nil
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc", "ctrl+c":
			m.creating = nil
			return true, nil
		case "tab", "shift+tab":
			if k.String() == "tab" {
				c.focus = (c.focus + 1) % 4
			} else {
				c.focus = (c.focus + 3) % 4
			}
			c.path.Blur()
			c.memory.Blur()
			c.ports.Blur()
			if c.focus == 0 {
				return true, c.path.Focus()
			}
			if c.focus == 1 {
				return true, c.memory.Focus()
			}
			if c.focus == 2 {
				return true, c.ports.Focus()
			}
			return true, nil
		case "enter":
			if c.focus != 3 {
				w, err := config.Canonical(strings.TrimSpace(c.path.Value()))
				if strings.TrimSpace(c.path.Value()) == "" {
					err = fmt.Errorf("Workspace 경로를 입력하세요")
				}
				if err == nil && !memoryPattern.MatchString(c.memory.Value()) {
					err = fmt.Errorf("Memory 예: 2g, 4096m")
				}
				if err != nil {
					m.actionError = safeText(err.Error())
					return true, nil
				}
				ports, portErr := parsePorts(c.ports.Value())
				if portErr != nil {
					m.actionError = portErr.Error()
					return true, nil
				}
				c.wizard = NewEmbeddedModel(m.Catalog, config.Selection{}, w, c.memory.Value())
				c.wizard.Ports = ports
				c.request = operations.Request{Kind: operations.KindCreate, Target: operations.Target{Workspace: w}, Memory: c.memory.Value(), BuildFirst: true, PaseoHome: "~/.paseo"}
				c.wizard.Width, c.wizard.Height = m.Width, m.Height
				m.actionError = ""
				return true, nil
			}
		}
	}
	var cmd tea.Cmd
	if c.focus == 0 {
		c.path, cmd = c.path.Update(msg)
	} else if c.focus == 1 {
		c.memory, cmd = c.memory.Update(msg)
	} else if c.focus == 2 {
		c.ports, cmd = c.ports.Update(msg)
	} else {
		c.picker, cmd = c.picker.Update(msg)
		if ok, path := c.picker.DidSelectFile(msg); ok {
			c.path.SetValue(path)
			c.focus = 0
			cmd = tea.Batch(cmd, c.path.Focus())
		}
	}
	// Directory reads and text cursor messages also belong to these components.
	if _, ok := msg.(tea.KeyPressMsg); ok {
		return true, cmd
	}
	var pickerCmd tea.Cmd
	c.picker, pickerCmd = c.picker.Update(msg)
	return true, tea.Batch(cmd, pickerCmd)
}
func (m *Dashboard) creationView() tea.View {
	c := m.creating
	if c.wizard != nil {
		return c.wizard.View()
	}
	content := "새 환경 생성 · CPU: 2\n" + c.path.View() + "\n" + c.memory.View() + "\n" + c.ports.View() + "\nTab: 경로 / Memory / ports / directory picker · Enter: 선택 wizard · Esc: 취소\n" + c.picker.View()
	if m.actionError != "" {
		content = m.actionError + "\n" + content
	}
	lines := strings.Split(content, "\n")
	if len(lines) > m.Height {
		lines = lines[:m.Height]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(1, m.Width), "…")
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}
func (m *Dashboard) advancedKey(k string) (bool, tea.Cmd) {
	if m.advancedMenu {
		switch k {
		case "esc":
			m.advancedMenu = false
		case "up", "k":
			m.advancedIndex = (m.advancedIndex + 4) % 5
		case "down", "j":
			m.advancedIndex = (m.advancedIndex + 1) % 5
		case "enter":
			m.advancedMenu = false
			if m.advancedIndex == 2 {
				m.templateTab = true
				m.Focus = 2
				m.templatesWanted = true
				return true, m.refresh(time.Now(), false)
			}
			if m.advancedIndex >= 3 {
				return true, m.enqueue(operations.Request{Kind: operations.KindPrune, DryRun: true, RemoveRuntimes: m.advancedIndex == 4, PaseoHome: "~/.paseo"})
			}
			w := m.selectedWorkspace()
			if w == nil || w.Missing || w.Error != "" {
				m.actionError = "확인 가능한 workspace 선택 필요"
				return true, nil
			}
			kind := operations.KindBuild
			if m.advancedIndex == 1 {
				kind = operations.KindLoad
			}
			r := operations.Request{Kind: kind, Target: operations.Target{Workspace: w.Path, Runtime: w.Runtime}}
			b := m.Backend
			flow := m.flowGeneration
			return true, func() tea.Msg {
				s, err := b.SavedSelection(r.Target.Workspace)
				return savedSelectionMsg{request: r, selection: s, err: err, flow: flow}
			}
		}
		return true, nil
	}
	switch k {
	case "n":
		return true, m.startCreation()
	case "a":
		m.newFlow()
		m.advancedMenu = true
		m.advancedIndex = 0
		m.modal.GotoTop()
		return true, nil
	}
	return false, nil
}
func (m *Dashboard) advancedContent() string {
	if !m.advancedMenu {
		return ""
	}
	lines := []string{"고급 작업 · Enter 선택 · Esc 닫기"}
	for i, label := range []string{"저장된 선택 이미지 build", "저장된 선택 template load", "templates 읽기 전용 보기", "누락 workspace prune 미리보기", "정리 + 남은 환경 삭제"} {
		mark := "  "
		if i == m.advancedIndex {
			mark = "› "
		}
		lines = append(lines, mark+label)
	}
	return strings.Join(lines, "\n")
}
func (m *Dashboard) prunePreview(r operations.Result, flow uint64, removeRuntimes bool) {
	if !m.ownsFlow(flow) {
		return
	}
	if removeRuntimes && len(r.Remaining) > 0 {
		m.pruneBlocked = freezeTargets(r.Remaining)
		m.actionError = "남은 환경 삭제 불가 · 실행하지 않음"
		return
	}
	if removeRuntimes && len(r.Missing) == 0 {
		m.actionError = "정리할 누락 workspace가 없습니다"
		return
	}
	targets := r.Missing
	if targets == nil {
		targets = make([]operations.Target, 0)
	}
	request := operations.Request{Kind: operations.KindPrune, RemoveRuntimes: removeRuntimes, PruneTargets: targets, PaseoHome: "~/.paseo"}
	label, warning := "확정된 누락 workspace registry 정리", "registry / selection / launch routing만 정리합니다. Sandbox 및 update 기록과 lock은 보존됩니다."
	if removeRuntimes {
		label, warning = "정리 + 남은 환경 삭제", "삭제 후 되돌릴 수 없습니다."
	}
	m.confirm = &confirmation{request: freezeRequest(request), label: label, warning: warning}
	m.modal.GotoTop()
}

func (m *Dashboard) restoreTargets(w *operations.Workspace) ([]operations.Target, error) {
	if w.Selection == nil {
		return nil, fmt.Errorf("완료된 saved selection 없음")
	}
	saved := *w.Selection
	saved.Pairs = slices.Clone(saved.Pairs)
	slices.SortFunc(saved.Pairs, func(a, b config.Pair) int {
		return strings.Compare(a.Agent+"-"+a.Methodology, b.Agent+"-"+b.Methodology)
	})
	targets := make([]operations.Target, 0, len(saved.Pairs))
	for _, profile := range saved.Profiles() {
		found := false
		for _, s := range w.Sandboxes {
			if s.Target.Profile == profile {
				entry := s.Target.Entry
				if entry.Image == "" || (entry.Sandbox == "" && entry.ContainerID == "") {
					return nil, fmt.Errorf("saved profile entry 확인 필요: %s", profile)
				}
				if saved.Category == config.CategorySandbox {
					if entry.Agent != saved.Agent || entry.EffectiveCategory() != saved.Category {
						return nil, fmt.Errorf("saved agent entry changed: %s", profile)
					}
				} else {
					matches := false
					for _, pair := range saved.Pairs {
						candidate := config.Entry{Category: saved.Category, Agent: pair.Agent, Tool: pair.Methodology}
						if candidate.Key() == profile && entry.Agent == pair.Agent && entry.Tool == pair.Methodology {
							matches = true
						}
					}
					if !matches {
						return nil, fmt.Errorf("saved profile entry changed: %s", profile)
					}
				}

				targets = append(targets, s.Target)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("saved profile entry 없음: %s", profile)
		}
	}
	return targets, nil
}

func parsePorts(text string) ([]config.PortMapping, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var ports []config.PortMapping
	for _, pair := range strings.Split(text, ",") {
		h, c, ok := strings.Cut(strings.TrimSpace(pair), ":")
		hp, he := strconv.Atoi(h)
		cp, ce := strconv.Atoi(c)
		if !ok || he != nil || ce != nil || hp < 1 || hp > 65535 || cp < 1 || cp > 65535 {
			return nil, fmt.Errorf("Ports: host:container (1–65535)")
		}
		ports = append(ports, config.PortMapping{HostAddress: "127.0.0.1", HostPort: hp, ContainerPort: cp})
	}
	return ports, nil
}
