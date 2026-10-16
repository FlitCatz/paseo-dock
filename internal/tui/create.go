package tui

import (
	"fmt"
	"maps"
	"os"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Creation owns only input and selection. Execution always uses the dashboard queue.
type creation struct {
	path, configPath, ports textinput.Model
	picker                  filepicker.Model
	focus                   int
	wizard                  *Model
	request                 operations.Request
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

func (m *Dashboard) startCreation() tea.Cmd {
	m.newFlow()
	path, configPath, ports := textinput.New(), textinput.New(), textinput.New()
	path.Prompt, configPath.Prompt, ports.Prompt = "> ", "> ", "> "
	picker := filepicker.New()
	picker.DirAllowed = true
	picker.FileAllowed = false
	picker.ShowPermissions, picker.ShowSize = false, false
	picker.AutoHeight = false
	picker.Cursor = "›"
	picker.Styles.Cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true)
	picker.Styles.Selected = lipgloss.NewStyle().Foreground(lipgloss.Color("84"))
	picker.Styles.Directory = lipgloss.NewStyle()
	picker.CurrentDirectory, _ = os.Getwd()
	if w := m.selectedWorkspace(); w != nil {
		path.SetValue(w.Path)
		picker.CurrentDirectory = w.Path
	}
	m.creating = &creation{path: path, configPath: configPath, ports: ports, picker: picker}
	m.sizeCreation()
	m.advancedMenu = false
	m.actionMenu = false
	m.actionError = ""
	return tea.Batch(m.creating.path.Focus(), m.creating.picker.Init())
}

// creationArea is the dashboard body the creation popups are placed over.
func (m *Dashboard) creationArea() (int, int) {
	return max(1, m.Width), max(1, m.Height-2)
}

// sizeCreation fits the inputs and directory picker to the popup box.
func (m *Dashboard) sizeCreation() {
	c := m.creating
	w, h := popupSize(m.creationArea())
	contentWidth, contentHeight, _, _ := sectionDimensions(w, h)
	// Each row is indented by the section bar; inputs also carry a prompt.
	inputWidth := max(1, contentWidth-6)
	c.path.SetWidth(inputWidth)
	c.configPath.SetWidth(inputWidth)
	c.ports.SetWidth(inputWidth)
	// Three input sections, the picker heading, blank separators and the help footer.
	c.picker.SetHeight(min(10, max(3, contentHeight-creationFixedRows)))
}

const creationFixedRows = 13

// creationPopup renders the new-environment form in the agent-selection style.
func (m *Dashboard) creationPopup(width, height int) string {
	c := m.creating
	sb := selectionBody{color: os.Getenv("NO_COLOR") == ""}
	sb.field("1  Workspace 경로", c.focus == 0, []string{c.path.View()})
	sb.field("2  Create config JSON · 선택 사항", c.focus == 1, []string{c.configPath.View()})
	sb.field("3  Container ports · host:container, 선택 사항", c.focus == 2, []string{c.ports.View()})
	rows := strings.Split(strings.TrimRight(c.picker.View(), "\n"), "\n")
	rows = rows[:min(len(rows), max(1, c.picker.Height()))]
	sb.field("4  Directory picker · "+c.picker.CurrentDirectory, c.focus == 3, rows)
	help := "Tab/Shift+Tab 이동 · Enter 다음 · Esc 취소"
	if c.focus == 3 {
		help = "↑/↓ 이동 · →/l 열기 · ←/h 상위 · Enter 선택 · Esc 취소"
	}
	footer := []string{"", sb.paint(muted, help)}
	if m.actionError != "" {
		footer = append([]string{sb.paint(errorRed, m.actionError)}, footer...)
	}
	return selectionPopup("새 환경 생성", sb, footer, width, height)
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
		r.Memory = v.Result.Memory
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
		m.Width, m.Height = max(1, size.Width), max(1, size.Height)
		m.sizeCreation()
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
			c.configPath.Blur()
			c.ports.Blur()
			if c.focus == 0 {
				return true, c.path.Focus()
			}
			if c.focus == 1 {
				return true, c.configPath.Focus()
			}
			if c.focus == 2 {
				return true, c.ports.Focus()
			}
			return true, nil
		case "enter":
			if c.focus != 3 {
				cfg := config.CreateConfig{}
				var err error
				if strings.TrimSpace(c.configPath.Value()) != "" {
					cfg, err = config.LoadCreateConfig(strings.TrimSpace(c.configPath.Value()))
					if err == nil {
						var s config.Selection
						s, err = m.Catalog.Normalize(cfg.Selection())
						cfg.CreateSelection = config.CreateSelection(s)
					}
					if err == nil {
						err = config.ValidateMemory(cfg.Category, cfg.Memory)
					}
					if err != nil {
						m.actionError = safeText(err.Error())
						return true, nil
					}
				}
				workspace := cfg.Workspace
				if strings.TrimSpace(c.path.Value()) != "" {
					workspace = strings.TrimSpace(c.path.Value())
				}
				w, err := config.Canonical(workspace)
				if workspace == "" {
					err = fmt.Errorf("Workspace 경로를 입력하세요")
				}
				if err != nil {
					m.actionError = safeText(err.Error())
					return true, nil
				}
				portsText := strings.Join(cfg.Ports, ",")
				if strings.TrimSpace(c.ports.Value()) != "" {
					portsText = c.ports.Value()
				}
				ports, portErr := parsePorts(portsText)
				if portErr != nil {
					m.actionError = portErr.Error()
					return true, nil
				}
				c.wizard = NewEmbeddedModel(m.Catalog, cfg.Selection(), w, cfg.Memory)
				if cfg.Category == config.CategoryContainer {
					c.wizard.Cursor = 1
				}
				c.wizard.Ports = ports
				c.wizard.Repository = cfg.Repository
				c.wizard.Result.Apply = cfg.Apply
				build := true
				if cfg.Build != nil {
					build = *cfg.Build
				}
				c.wizard.BuildFirst = build
				if cfg.PaseoHome != "" {
					c.wizard.PaseoHome = cfg.PaseoHome
				}
				c.request = operations.Request{Kind: operations.KindCreate, Target: operations.Target{Workspace: w}, Repository: cfg.Repository, Namespace: cfg.Namespace, BuildFirst: build, PaseoHome: cfg.PaseoHome}
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
		c.configPath, cmd = c.configPath.Update(msg)
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

// The form and the category step are popups over the dashboard; the later
// wizard steps need the whole screen for their lists and final review.
func (m *Dashboard) creationFullScreen() bool {
	c := m.creating
	return c != nil && c.wizard != nil && (c.wizard.Step >= 0 || c.wizard.editingMemory)
}
func (m *Dashboard) creationPopupFor(width, height int) string {
	if w := m.creating.wizard; w != nil {
		return w.categoryPopup(width, height)
	}
	return m.creationPopup(width, height)
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
				return true, m.focusPanel(2)
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
				} else if saved.Name != "" {
					bundle := saved.BundleEntry()
					if entry.Name != bundle.Name || !slices.Equal(entry.Pairs, bundle.Pairs) || !maps.Equal(entry.Connections, bundle.Connections) {
						return nil, fmt.Errorf("saved bundle entry changed: %s", profile)
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
