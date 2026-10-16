package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"fmt"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
	"strings"
	"testing"
)

type creationBackend struct {
	*jobBackend
	saved config.Selection
}

func (b *creationBackend) SavedSelection(string) (config.Selection, error) { return b.saved, nil }

func TestAdvancedSavedSelectionActions(t *testing.T) {
	for index, kind := range []operations.Kind{operations.KindBuild, operations.KindLoad} {
		b := &creationBackend{jobBackend: &jobBackend{}, saved: config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}, Utilities: []string{"rtk"}}}
		m := NewDashboard(b, config.DefaultCatalog(), "")
		defer m.cancel()
		deliver(m, managementFixture())
		keyCmd(m, "a")
		m.advancedIndex = index
		cmd := keyCmd(m, "enter")
		if cmd == nil {
			t.Fatal("saved selection lookup missing")
		}
		m.Update(cmd())
		if m.confirm == nil || m.confirm.request.Kind != kind {
			t.Fatal("advanced action did not confirm saved selection")
		}
		b.saved.Utilities[0] = "changed"
		m.WorkspaceID = "runtime-b"
		runJobCommand(m, keyCmd(m, "enter"))
		b.mu.Lock()
		r := b.requests[0]
		b.mu.Unlock()
		if r.Target.Workspace != "/한국어/a" || r.Selection.Utilities[0] != "rtk" {
			t.Fatal("advanced action request changed", r)
		}
	}
	m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
	defer m.cancel()
	keyCmd(m, "a")
	m.advancedIndex = 2
	cmd := keyCmd(m, "enter")
	if cmd == nil || m.Focus != 2 {
		t.Fatal("template view did not open")
	}
	msg := cmd().(snapshotMsg)
	if !msg.query.IncludeTemplates {
		t.Fatal("template entry did not request templates")
	}
}

func TestDashboardPruneConfirmationFrozen(t *testing.T) {
	missing := []operations.Target{{Workspace: "/gone", Runtime: "/runtime", Profile: "codex-superpowers", Entry: config.Entry{Workspace: "/gone", Sandbox: "kept", Image: "image"}}}
	b := &jobBackend{execute: func(_ context.Context, r operations.Request, _ func(operations.Event)) (operations.Result, error) {
		return operations.Result{Missing: missing}, nil
	}}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	keyCmd(m, "a")
	m.advancedIndex = 3
	runJobCommand(m, keyCmd(m, "enter"))
	if m.confirm == nil || !strings.Contains(m.managementContent(), "/gone") || !strings.Contains(m.managementContent(), "보존") {
		t.Fatal("prune preview confirmation absent")
	}
	missing[0].Workspace = "/unconfirmed"
	b.execute = nil
	runJobCommand(m, keyCmd(m, "enter"))
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.requests) != 2 || !b.requests[0].DryRun || b.requests[1].DryRun || len(b.requests[1].PruneTargets) != 1 || b.requests[1].PruneTargets[0].Workspace != "/gone" {
		t.Fatal("preview targets changed", b.requests)
	}
}

// Missing n handling would prevent creation from ever reaching the child wizard.
func TestCreationRequestFrozen(t *testing.T) {
	b := &jobBackend{}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	keyCmd(m, "n")
	if !strings.Contains(m.View().Content, "Workspace 경로") || !strings.Contains(m.View().Content, "config") {
		t.Fatal("n did not open directory picker/path and config entry")
	}
	path, err := config.Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.creating.path.SetValue(path)
	keyCmd(m, "enter")
	if m.creating.wizard == nil {
		t.Fatal("valid path/memory did not lead to wizard")
	}
	child := m.creating.wizard
	key(child, tea.KeyDown)
	key(child, tea.KeyEnter)
	for _, code := range []rune{tea.KeySpace, tea.KeyEnter, tea.KeySpace, tea.KeyEnter, tea.KeySpace, tea.KeyEnter} {
		m.Update(tea.KeyPressMsg(tea.Key{Code: code}))
	}
	if child.Step != 3 {
		t.Fatal("wizard did not reach final confirmation", child.Step)
	}
	for _, want := range []string{"codex-superpowers", "호스트 자원 공유", "Utilities: rtk", "Build", "Paseo", "~/.paseo/config.json"} {
		if !strings.Contains(m.View().Content, want) {
			t.Fatalf("confirmation omits %s: %s", want, m.View().Content)
		}
	}
	cmd := keyCmd(m, "enter")
	if cmd == nil {
		t.Fatal("wizard completion missing")
	}
	finished := cmd()
	child.Memory = "8g"
	child.Result.Selection.Pairs[0].Agent = "claude"
	_, dispatch := m.Update(finished)
	if dispatch == nil {
		t.Fatal("create was not queued")
	}
	keyCmd(m, "n") // A later creation must not edit the prior queued request.
	runJobCommand(m, dispatch)
	b.mu.Lock()
	defer b.mu.Unlock()
	r := b.requests[0]
	if r.Kind != operations.KindCreate || r.Target.Workspace != path || r.Memory != "" || !r.BuildFirst || r.Selection.Pairs[0].Agent != "codex" || len(r.Selection.Utilities) != 1 || r.Selection.Utilities[0] != "rtk" {
		t.Fatalf("creation request was mutable/incomplete: %+v", r)
	}
}

func TestJobOutputPreservesSemanticStage(t *testing.T) {
	l := newJobLog()
	l.emit(operations.Event{Stage: "build", Text: "Building"})
	l.emit(operations.Event{Stage: "stdout", Text: "progress"})
	l.emit(operations.Event{Stage: "stderr", Text: "warning"})
	if l.currentStage() != "build" {
		t.Fatalf("output replaced semantic stage: %s", l.currentStage())
	}
}

func TestDashboardRestoreFreezesDesiredEntries(t *testing.T) {
	b := &jobBackend{}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	snapshot := managementFixture()
	w := &snapshot.Workspaces[0]
	w.Selection = &config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}, {Agent: "claude", Methodology: "superpowers"}}}
	for i := range w.Sandboxes {
		s := &w.Sandboxes[i]
		s.Active = false
		s.Target.Profile = w.Selection.Pairs[i].Agent + "-superpowers"
		s.Target.Entry.Agent = w.Selection.Pairs[i].Agent
		s.Target.Entry.Tool = "superpowers"
		s.Target.Entry.Image = "image"
	}
	deliver(m, snapshot)
	a := m.actions()[5]
	if a.reason != "" {
		t.Fatal("completed inactive workspace could not register", a.reason)
	}
	m.chooseAction(a)
	if m.confirm == nil || !m.confirm.request.RestoreSelection || len(m.confirm.request.RestoreTargets) != 2 {
		t.Fatal("registration did not confirm complete saved selection")
	}
	w.Sandboxes[1].Target.Entry.Sandbox = "replacement"
	w.Selection.Pairs[0].Agent = "replacement"
	runJobCommand(m, keyCmd(m, "enter"))
	b.mu.Lock()
	defer b.mu.Unlock()
	r := b.requests[0]
	if r.Targets == nil || len(r.Targets) != 0 || r.RestoreTargets[0].Entry.Sandbox != "sandbox-two" || r.Selection.Pairs[0].Agent != "codex" {
		t.Fatal("restoration identities were mutable", r)
	}
}

func TestOldWizardCompletionCannotCreateNewWorkspace(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) {
			m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
			defer m.cancel()
			keyCmd(m, "n")
			m.creating.path.SetValue(t.TempDir())
			keyCmd(m, "enter")
			for _, code := range []rune{tea.KeySpace, tea.KeyEnter, tea.KeySpace, tea.KeyEnter, tea.KeyEnter} {
				m.Update(tea.KeyPressMsg(tea.Key{Code: code}))
			}
			old := keyCmd(m, "enter")
			cancelled := keyCmd(m, "ctrl+c")
			m.Update(cancelled())
			if cancel {
				old = cancelled
			}
			keyCmd(m, "n")
			m.creating.path.SetValue(t.TempDir())
			keyCmd(m, "enter")
			m.Update(old())
			if len(m.jobs) != 0 || m.creating == nil || m.creating.wizard == nil {
				t.Fatal("stale child completion created against the new workspace")
			}
		})
	}
}
