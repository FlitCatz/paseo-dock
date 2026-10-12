package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"

	tea "charm.land/bubbletea/v2"
)

func removeAction(m *Dashboard) action {
	for _, a := range m.actions() {
		if a.kind == operations.KindRemove {
			return a
		}
	}
	return action{}
}

func typeKeys(m *Dashboard, s string) {
	for _, r := range s {
		keyCmd(m, string(r))
	}
}

func removeBackspace(m *Dashboard) {
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
}

func TestRemoveActionStatesAndMissingWorkspace(t *testing.T) {
	for _, tc := range []struct{ state, reason, label string }{
		{"running", "먼저 중지(x) 필요", "대상 삭제"},
		{"unknown", "상태 확인 불가 · 삭제 불가", "대상 삭제"},
		{"stopped", "", "대상 삭제"},
		{"missing", "", "registry 항목만 정리"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
			defer m.cancel()
			s := managementFixture()
			s.Workspaces[0].Missing = true
			s.Workspaces[0].Sandboxes[0].RuntimeState = tc.state
			deliver(m, s)
			a := removeAction(m)
			if a.kind != operations.KindRemove || a.reason != tc.reason || a.label != tc.label {
				t.Fatalf("remove action = %+v", a)
			}
			for _, a := range m.actions() {
				if a.kind != operations.KindRemove && a.kind != operations.KindStop && a.reason != "workspace 경로 없음" {
					t.Fatalf("missing workspace enabled %q: %+v", a.label, a)
				}
			}
		})
	}
}

func TestRemoveActionOtherDisabledReasons(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		change       func(*Dashboard)
	}{
		{"unmanaged", "읽기 전용 · 미등록 sandbox", func(m *Dashboard) { m.Unmanaged = true }},
		{"shell busy", "shell이 터미널을 사용 중", func(m *Dashboard) { m.shellRunning = true }},
		{"no workspace", "workspace 선택 필요", func(m *Dashboard) { m.WorkspaceID = "none" }},
		{"no sandbox", "sandbox 선택 필요", func(m *Dashboard) { m.ProfileID = "none" }},
		{"registry error", "registry 확인 불가", func(m *Dashboard) { m.Snapshot.Workspaces[0].Error = "corrupt" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
			defer m.cancel()
			deliver(m, managementFixture())
			tc.change(m)
			if got := removeAction(m).reason; got != tc.reason {
				t.Fatalf("disabled reason = %q, want %q", got, tc.reason)
			}
		})
	}
}

func TestRemoveShortcutRequiresExactPinnedProfile(t *testing.T) {
	b := &jobBackend{snapshot: managementFixture()}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	deliver(m, b.snapshot)
	keyCmd(m, "d")
	if m.confirm != nil || len(m.jobs) != 0 {
		t.Fatal("running target opened removal")
	}
	m.ProfileID = "a-two"
	keyCmd(m, "d")
	if m.confirm == nil || m.confirm.request.Kind != operations.KindRemove {
		t.Fatal("stopped target did not open removal")
	}
	for _, part := range []string{"Category:", "Workspace:", "Profile:", "Sandbox:", "Image:", "삭제되는 것", "보존되는 것", "되돌릴 수 없"} {
		if !strings.Contains(m.managementContent(), part) {
			t.Fatalf("removal confirmation lacks %q: %s", part, m.managementContent())
		}
	}
	typeKeys(m, "a-tw")
	if cmd := keyCmd(m, "enter"); cmd != nil || len(m.jobs) != 0 {
		t.Fatal("partial confirmation submitted")
	}
	keyCmd(m, "o")
	removeBackspace(m)
	if cmd := keyCmd(m, "enter"); cmd != nil || len(m.jobs) != 0 {
		t.Fatal("edited confirmation submitted")
	}
	keyCmd(m, "o")
	m.ProfileID = "a-one"
	m.WorkspaceID = "runtime-b"
	s := managementFixture()
	s.Workspaces[0].Sandboxes[1].Target.Entry.Sandbox = "replacement"
	deliver(m, s)
	cmd := keyCmd(m, "enter")
	if cmd == nil {
		t.Fatalf("exact confirmation did not submit: %+v", m.confirm)
	}
	runJobCommand(m, cmd)
	if len(b.requests) != 1 || b.requests[0].Kind != operations.KindRemove || b.requests[0].Target.Entry.Sandbox != "sandbox-two" {
		t.Fatalf("remove target changed: %+v", b.requests)
	}
}

func TestRemoveModalEscapeAndKeysInert(t *testing.T) {
	b := &jobBackend{snapshot: managementFixture()}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	deliver(m, b.snapshot)
	m.Searching = true
	keyCmd(m, "d")
	keyCmd(m, "x")
	if m.confirm != nil || len(m.jobs) != 0 {
		t.Fatal("shortcut fired in search")
	}
	keyCmd(m, "esc")
	m.ProfileID = "a-two"
	keyCmd(m, "d")
	keyCmd(m, "x")
	if m.confirm == nil || m.confirm.request.Kind != operations.KindRemove {
		t.Fatal("shortcut replaced modal")
	}
	keyCmd(m, "esc")
	if m.confirm != nil || len(m.jobs) != 0 {
		t.Fatal("escape submitted removal")
	}
	keyCmd(m, "x")
	if m.confirm == nil || m.confirm.request.Kind != operations.KindStop {
		t.Fatal("x did not open stop confirmation")
	}
}

func TestRemoveShortcutsInertDuringCreationInput(t *testing.T) {
	b := &jobBackend{snapshot: managementFixture()}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	deliver(m, b.snapshot)
	m.ProfileID = "a-two"
	keyCmd(m, "n")
	if m.creating == nil {
		t.Fatal("creation input did not open")
	}
	keyCmd(m, "d")
	keyCmd(m, "x")
	if m.confirm != nil || len(m.jobs) != 0 || m.creating == nil {
		t.Fatal("runtime shortcut escaped creation input")
	}
}

func TestRemoveRetryPinsFailedTarget(t *testing.T) {
	b := &jobBackend{snapshot: managementFixture(), execute: func(_ context.Context, r operations.Request, _ func(operations.Event)) (operations.Result, error) {
		return operations.Result{Remaining: []operations.Target{r.Target}}, errors.New("remove failed")
	}}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	deliver(m, b.snapshot)
	m.ProfileID = "a-two"
	keyCmd(m, "d")
	typeKeys(m, "a-two")
	runJobCommand(m, keyCmd(m, "enter"))
	if !strings.Contains(m.logContent(), "provider") || !strings.Contains(m.logContent(), "남은 대상") {
		t.Fatal("removal failure did not explain partial state")
	}
	m.ProfileID = "a-one"
	keyCmd(m, "R")
	if m.confirm == nil || m.confirm.request.Target.Entry.Sandbox != "sandbox-two" {
		t.Fatal("retry lost pinned target")
	}
	if cmd := keyCmd(m, "enter"); cmd != nil {
		t.Fatal("retry skipped strict confirmation")
	}
	typeKeys(m, "a-two")
	runJobCommand(m, keyCmd(m, "enter"))
	if len(b.requests) != 2 || b.requests[1].Target.Entry.Sandbox != "sandbox-two" {
		t.Fatalf("retry changed target: %+v", b.requests)
	}
}

func TestPruneRuntimesPreviewBlocksAndPins(t *testing.T) {
	for _, blocked := range []bool{true, false} {
		t.Run(map[bool]string{true: "blocked", false: "confirmed"}[blocked], func(t *testing.T) {
			missing := []operations.Target{{Workspace: "/gone", Runtime: "/registry", Profile: "one", Entry: config.Entry{Sandbox: "sbx-one"}}, {Workspace: "/gone", Runtime: "/registry", Profile: "two", Entry: config.Entry{Category: config.CategoryContainer, ContainerID: "cid-two"}}}
			b := &jobBackend{snapshot: managementFixture(), execute: func(_ context.Context, r operations.Request, _ func(operations.Event)) (operations.Result, error) {
				if !r.DryRun {
					return operations.Result{}, nil
				}
				result := operations.Result{Missing: missing}
				if blocked {
					result.Remaining = missing[1:]
				}
				return result, nil
			}}
			m := NewDashboard(b, config.DefaultCatalog(), "")
			defer m.cancel()
			deliver(m, b.snapshot)
			keyCmd(m, "a")
			if !strings.Contains(m.managementContent(), "정리 + 남은 환경 삭제") {
				t.Fatal("advanced menu omits runtime cleanup")
			}
			m.advancedIndex = 4
			runJobCommand(m, keyCmd(m, "enter"))
			if len(b.requests) != 1 || !b.requests[0].DryRun || !b.requests[0].RemoveRuntimes {
				t.Fatalf("runtime dry run absent: %+v", b.requests)
			}
			if blocked {
				if m.confirm != nil || !strings.Contains(m.managementContent(), "cid-two") {
					t.Fatal("blocked target not shown or confirmation offered")
				}
				return
			}
			if m.confirm == nil || !strings.Contains(m.managementContent(), "sbx-one") || !strings.Contains(m.managementContent(), "cid-two") {
				t.Fatal("resource list missing in prune confirmation")
			}
			typeKeys(m, "prun")
			if cmd := keyCmd(m, "enter"); cmd != nil {
				t.Fatal("prune submitted with partial word")
			}
			keyCmd(m, "e")
			missing[0].Entry.Sandbox = "changed"
			runJobCommand(m, keyCmd(m, "enter"))
			if len(b.requests) != 2 || b.requests[1].DryRun || !b.requests[1].RemoveRuntimes || len(b.requests[1].PruneTargets) != 2 || b.requests[1].PruneTargets[0].Entry.Sandbox != "sbx-one" {
				t.Fatalf("prune targets not pinned: %+v", b.requests)
			}
		})
	}
}

func TestPruneRuntimesEmptyPreviewDoesNotOfferExecution(t *testing.T) {
	b := &jobBackend{snapshot: managementFixture()}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	deliver(m, b.snapshot)
	keyCmd(m, "a")
	m.advancedIndex = 4
	runJobCommand(m, keyCmd(m, "enter"))
	if m.confirm != nil || len(b.requests) != 1 || !strings.Contains(m.actionError, "없") {
		t.Fatalf("empty preview offered execution: confirm=%+v requests=%+v error=%q", m.confirm, b.requests, m.actionError)
	}
}

func TestRemoveJobShowsOperationStages(t *testing.T) {
	b := &jobBackend{snapshot: managementFixture(), execute: func(_ context.Context, _ operations.Request, emit func(operations.Event)) (operations.Result, error) {
		for _, stage := range []string{"unregister", "sync-providers", "remove-resource", "verify-removal", "registry-cleanup"} {
			emit(operations.Event{Stage: stage, Text: stage})
		}
		return operations.Result{Changed: true}, nil
	}}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	deliver(m, b.snapshot)
	m.ProfileID = "a-two"
	keyCmd(m, "d")
	typeKeys(m, "a-two")
	runJobCommand(m, keyCmd(m, "enter"))
	log := m.logContent()
	for _, stage := range []string{"unregister", "sync-providers", "remove-resource", "verify-removal", "registry-cleanup"} {
		if !strings.Contains(log, stage) {
			t.Fatalf("stage %q not shown: %s", stage, log)
		}
	}
}
