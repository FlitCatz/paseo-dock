package tui

import (
	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
	"strings"
	"testing"
	"time"
)

func TestSBXFailurePreservesDockerShell(t *testing.T) {
	m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "/workspace")
	defer m.cancel()
	docker := operations.Sandbox{Target: operations.Target{Category: config.CategoryContainer, Profile: "container/codex-superpowers", Entry: config.Entry{Category: config.CategoryContainer, Agent: "codex", Tool: "superpowers", ContainerID: "retained-cid"}}, RuntimeState: "running"}
	sbx := operations.Sandbox{Target: operations.Target{Category: config.CategorySandbox, Profile: "sandbox/codex", Entry: config.Entry{Category: config.CategorySandbox, Agent: "codex", Sandbox: "sbx-one"}}, RuntimeState: "unknown"}
	deliver(m, operations.Snapshot{
		SBX:        operations.Connection{Status: "error", Error: "sbx unavailable", CheckedAt: time.Now()},
		Docker:     operations.Connection{Status: "connected", CheckedAt: time.Now()},
		Workspaces: []operations.Workspace{{ID: "runtime", Path: "/workspace", Sandboxes: []operations.Sandbox{docker, sbx}}},
	})
	m.ProfileID = "container/codex-superpowers"
	if got := m.selectedSandbox().RuntimeState; got != "running" {
		t.Fatalf("healthy Docker state lost after SBX error: %s", got)
	}
	for _, a := range m.actions() {
		if a.shell && a.reason != "" {
			t.Fatalf("healthy Docker shell disabled by SBX error: %s", a.reason)
		}
	}
	if m.Snapshot.Workspaces[0].Sandboxes[1].RuntimeState != "unknown" || !strings.Contains(m.Error, "sbx unavailable") {
		t.Fatal("SBX error was hidden")
	}
}

func TestJobLogReportsUnrecordedDockerIdentity(t *testing.T) {
	m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
	defer m.cancel()
	cid := strings.Repeat("b", 64)
	m.jobs = []*job{{id: 1, state: "failed", errorText: "registry write failed", result: operations.Result{NeedsVerification: true, Remaining: []operations.Target{{Category: config.CategoryContainer, Profile: "container/codex-superpowers", Entry: config.Entry{Category: config.CategoryContainer, ContainerID: cid}}}}, log: &jobLog{}}}
	if got := m.logContent(); !strings.Contains(got, cid) || !strings.Contains(got, "container") {
		t.Fatalf("retained Docker CID missing from recovery output: %s", got)
	}
}
