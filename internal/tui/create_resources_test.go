package tui

import (
	tea "charm.land/bubbletea/v2"
	"os"
	"paseo-dock/internal/config"
	"path/filepath"
	"strings"
	"testing"
)

func TestContainerWizardDoesNotAdvertiseResourceLimits(t *testing.T) {
	m := NewModel(config.DefaultCatalog(), config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}, "/project", "4g")
	m.Step = 3
	m.Result.Selection = m.selection()
	view := m.View().Content
	if strings.Contains(view, "Memory") || strings.Contains(view, "CPU: 2") {
		t.Fatal("container advertises unapplied limits", view)
	}
}

func TestCreationMemoryOnlyAfterSandboxSelection(t *testing.T) {
	m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
	defer m.cancel()
	keyCmd(m, "n")
	if strings.Contains(m.View().Content, "Memory") || strings.Contains(m.View().Content, "CPU: 2") {
		t.Fatal("resources shown before category choice")
	}
	m.creating.path.SetValue(t.TempDir())
	keyCmd(m, "enter")
	child := m.creating.wizard
	key(child, tea.KeyEnter)
	key(child, tea.KeySpace)
	key(child, tea.KeyEnter)
	key(child, 'm')
	if !strings.Contains(child.View().Content, "Memory") {
		t.Fatal("sandbox cannot edit memory")
	}
	child.memoryInput.SetValue("bad")
	key(child, tea.KeyEnter)
	if !child.editingMemory || child.Confirmed {
		t.Fatal("invalid sandbox size accepted")
	}
	child.memoryInput.SetValue("4g")
	key(child, tea.KeyEnter)
	key(child, tea.KeyEnter)
	if !child.Confirmed || child.Result.Memory != "4g" {
		t.Fatalf("edited memory not in result: %+v", child.Result)
	}
}

func TestDashboardCreateConfigPreviewAndFrozenRequest(t *testing.T) {
	b := &jobBackend{}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	dir := t.TempDir()
	path := filepath.Join(dir, "create.json")
	body := `{"workspace":"./checkout","repository":{"url":"https://github.com/example/project.git","ref":"main"},"category":"container","pairs":[{"agent":"codex","methodology":"superpowers"}],"ports":["8080:3000"],"build":false,"apply":true}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	keyCmd(m, "n")
	m.creating.configPath.SetValue(path)
	keyCmd(m, "enter")
	if m.creating.wizard == nil {
		t.Fatal("config did not open wizard", m.actionError)
	}
	child := m.creating.wizard
	for i := 0; i < 4; i++ {
		key(child, tea.KeyEnter)
	}
	view := child.View().Content
	if !strings.Contains(view, "https://github.com/example/project.git") || !strings.Contains(view, "main") || !strings.Contains(view, "Existing image") {
		t.Fatal("preview lost configured operation", view)
	}
	cmd := keyCmd(m, "enter")
	_, dispatch := m.Update(cmd())
	runJobCommand(m, dispatch)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.requests) != 1 {
		t.Fatal("no create request")
	}
	r := b.requests[0]
	w, _ := config.Canonical(filepath.Join(dir, "checkout"))
	if r.Repository.Ref != "main" || r.Target.Workspace != w || r.Memory != "" || r.BuildFirst || !r.Apply || len(r.Ports) != 1 {
		t.Fatalf("config lost in request: %+v", r)
	}
}
