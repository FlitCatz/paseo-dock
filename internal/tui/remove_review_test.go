package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
)

func TestQuitFromConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, key   string
		typed, busy bool
	}{
		{"control c typed idle", "ctrl+c", true, false},
		{"control c typed busy", "ctrl+c", true, true},
		{"control c regular idle", "ctrl+c", false, false},
		{"control c regular busy", "ctrl+c", false, true},
		{"q regular idle", "q", false, false},
		{"q regular busy", "q", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
			defer m.cancel()
			deliver(m, managementFixture())
			if tc.typed {
				m.ProfileID = "a-two"
				keyCmd(m, "d")
			} else {
				keyCmd(m, "x")
			}
			if m.confirm == nil {
				t.Fatal("confirmation fixture did not open")
			}
			if tc.busy {
				m.jobs = append(m.jobs, &job{state: "running"})
			}
			cmd := keyCmd(m, tc.key)
			if m.confirm != nil {
				t.Fatal("quit retained confirmation")
			}
			if tc.busy {
				if cmd != nil || !m.exitPrompt || m.ctx.Err() != nil {
					t.Fatalf("busy quit did not open exit prompt: prompt=%t cmd=%v err=%v", m.exitPrompt, cmd, m.ctx.Err())
				}
			} else {
				if cmd == nil || m.ctx.Err() == nil {
					t.Fatal("idle quit did not cancel dashboard")
				}
				if _, ok := cmd().(tea.QuitMsg); !ok {
					t.Fatal("idle quit did not return QuitMsg")
				}
			}
		})
	}
}

func TestQIsLiteralInsideTypedRemoveConfirmation(t *testing.T) {
	s := managementFixture()
	s.Workspaces[0].Sandboxes[1].Target.Profile = "q-two"
	m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
	defer m.cancel()
	deliver(m, s)
	m.ProfileID = "q-two"
	keyCmd(m, "d")
	if m.confirm == nil {
		t.Fatal("remove confirmation did not open")
	}
	flow := m.flowGeneration
	keyCmd(m, "q")
	if m.confirm == nil || m.confirm.typed != "q" || m.flowGeneration != flow || m.ctx.Err() != nil {
		t.Fatalf("q did not remain literal: confirm=%+v flow=%d err=%v", m.confirm, m.flowGeneration, m.ctx.Err())
	}
	typeKeys(m, "-two")
	if cmd := keyCmd(m, "enter"); cmd == nil {
		t.Fatal("profile containing q could not be confirmed")
	}
}

func TestPruneBlockedPanelConsumesActionKeys(t *testing.T) {
	m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
	defer m.cancel()
	deliver(m, managementFixture())
	target := operations.Target{Workspace: "/gone", Profile: "blocked", Entry: config.Entry{Sandbox: "sbx-blocked"}}
	m.prunePreview(operations.Result{Missing: []operations.Target{target}, Remaining: []operations.Target{target}}, m.flowGeneration, true)
	if len(m.pruneBlocked) != 1 || !strings.Contains(m.managementContent(), "Esc 닫기") {
		t.Fatal("blocked panel fixture absent or dismiss key hidden")
	}
	profile := m.ProfileID
	for _, key := range []string{"j", "k", "u", "s", "x", "d", "enter"} {
		if cmd := keyCmd(m, key); cmd != nil || m.ProfileID != profile || m.actionMenu || m.confirm != nil || len(m.jobs) != 0 || len(m.pruneBlocked) != 1 {
			t.Fatalf("%q acted behind blocked panel", key)
		}
	}
	keyCmd(m, "esc")
	if len(m.pruneBlocked) != 0 || m.managementContent() != "" {
		t.Fatal("Escape did not close blocked panel")
	}
	keyCmd(m, "x")
	if m.confirm == nil || m.confirm.request.Kind != operations.KindStop {
		t.Fatal("actions stayed disabled after closing blocked panel")
	}
}

func TestRemoveConfirmationShowsCategoryIdentityOnly(t *testing.T) {
	for _, tc := range []struct {
		name, want, absent string
		category           config.Category
	}{
		{"sandbox", "Sandbox: sandbox-two", "Container ID:", config.CategorySandbox},
		{"container", "Container ID: cid-two", "Sandbox:", config.CategoryContainer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := managementFixture()
			target := &s.Workspaces[0].Sandboxes[1].Target
			target.Category = tc.category
			target.Entry.Category = tc.category
			if tc.category == config.CategoryContainer {
				target.Entry.Sandbox = ""
				target.Entry.ContainerID = "cid-two"
			}
			m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
			defer m.cancel()
			deliver(m, s)
			m.ProfileID = sandboxID(s.Workspaces[0].Sandboxes[1])
			keyCmd(m, "d")
			content := m.managementContent()
			if !strings.Contains(content, tc.want) || strings.Contains(content, tc.absent) {
				t.Fatalf("wrong identity for %s: %s", tc.name, content)
			}
		})
	}
}
