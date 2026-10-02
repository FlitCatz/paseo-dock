package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
	"testing"
)

// A delayed lookup must never replace another flow's confirmation or reopen
// a flow the user abandoned. The backend substitutes only external resources.
func TestAdvancedLookupRespectsParentFlow(t *testing.T) {
	for _, next := range []string{"stop confirmation", "esc", "cancel", "creation", "advanced menu", "newer lookup", "action menu"} {
		t.Run(next, func(t *testing.T) {
			b := &creationBackend{jobBackend: &jobBackend{snapshot: managementFixture()}, saved: config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}}
			m := NewDashboard(b, config.DefaultCatalog(), "")
			defer m.cancel()
			deliver(m, b.snapshot)
			keyCmd(m, "a")
			old := keyCmd(m, "enter")
			switch next {
			case "stop confirmation":
				m.ProfileID = "a-two"
				m.chooseAction(m.actions()[3])
			case "esc":
				keyCmd(m, "esc")
			case "cancel":
				keyCmd(m, "c")
			case "creation":
				keyCmd(m, "n")
			case "advanced menu":
				keyCmd(m, "a")
			case "newer lookup":
				keyCmd(m, "a")
				m.advancedIndex = 1
				newer := keyCmd(m, "enter")
				m.Update(newer())
			case "action menu":
				keyCmd(m, "enter")
			}
			confirmation, child, advanced, action := m.confirm, m.creating, m.advancedMenu, m.actionMenu
			m.Update(old())
			if m.confirm != confirmation || m.creating != child || m.advancedMenu != advanced || m.actionMenu != action {
				t.Fatal("delayed lookup replaced/reopened a newer or abandoned flow")
			}
			if next == "stop confirmation" && (m.confirm == nil || m.confirm.request.Target.Entry.Sandbox != "sandbox-two" || m.confirm.request.Kind != operations.KindStop) {
				t.Fatal("newer stop confirmation lost its frozen intent")
			}
		})
	}
}

func TestPruneCompletionRespectsParentFlow(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(map[bool]string{false: "delayed", true: "queued"}[queued], func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			b := &jobBackend{snapshot: managementFixture(), execute: func(_ context.Context, r operations.Request, _ func(operations.Event)) (operations.Result, error) {
				if r.Kind == operations.KindStop {
					close(started)
					<-release
					return operations.Result{}, nil
				}
				return operations.Result{Missing: []operations.Target{{Workspace: "/gone", Runtime: "/runtime", Entry: config.Entry{Sandbox: "kept"}}}}, nil
			}}
			m := NewDashboard(b, config.DefaultCatalog(), "")
			defer m.cancel()
			deliver(m, b.snapshot)
			var first <-chan tea.Msg
			if queued {
				first = startJobCommands(m.enqueue(operations.Request{Kind: operations.KindStop, Target: m.rows()[0].Target}))
				<-started
			}
			keyCmd(m, "a")
			m.advancedIndex = 3
			preview := keyCmd(m, "enter")
			var done tea.Msg
			if !queued {
				done = preview().(tea.BatchMsg)[0]()
			}
			m.ProfileID = "a-two"
			m.chooseAction(m.actions()[3])
			confirmation := m.confirm
			if confirmation == nil {
				t.Fatal("newer confirmation fixture absent")
			}
			if queued {
				close(release)
				_, cmd := m.Update(awaitJobDone(t, first))
				next := startJobCommands(cmd)
				m.Update(awaitJobDone(t, next))
			} else {
				m.Update(done)
			}
			if m.confirm != confirmation || m.confirm.request.Kind != operations.KindStop || m.confirm.request.Target.Entry.Sandbox != "sandbox-two" {
				t.Fatal("delayed prune replaced newer frozen confirmation")
			}
			if m.busy() {
				t.Fatal("discarded preview response wedged the single queue")
			}
		})
	}
}

func TestAbandonedPruneCompletionCannotOpenModal(t *testing.T) {
	for _, next := range []string{"esc", "creation", "advanced menu"} {
		t.Run(next, func(t *testing.T) {
			b := &jobBackend{snapshot: managementFixture()}
			m := NewDashboard(b, config.DefaultCatalog(), "")
			defer m.cancel()
			deliver(m, b.snapshot)
			keyCmd(m, "a")
			m.advancedIndex = 3
			cmd := keyCmd(m, "enter")
			done := cmd().(tea.BatchMsg)[0]()
			switch next {
			case "esc":
				keyCmd(m, "esc")
			case "creation":
				keyCmd(m, "n")
			case "advanced menu":
				keyCmd(m, "a")
			}
			m.Update(done)
			if m.confirm != nil {
				t.Fatal("abandoned prune reopened confirmation")
			}
		})
	}
}
