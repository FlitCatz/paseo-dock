# Task 3 report

Status: DONE

Implemented the read-only management dashboard, typed Backend contract, stable runtime/profile selection, request-ID stale response rejection, coalesced polling, current connection errors with preserved rows/last successful SBX timestamp, slow metadata retention, unmanaged read-only view, templates tab, search/navigation/help, display-width-safe responsive panels, and terminal CLI entry.

The dashboard uses Bubbles v2 list/viewport/help with Lip Gloss v2. Wide terminals show workspace/sandbox/detail/log panels; 80x24 uses detail/log tabs; smaller terminals show the focused panel. NO_COLOR removes ANSI styling, and lists retain a selected Korean row even at the end of a long list. Details distinguish image configuration, updater observations and independent VersionCheck observations, preserving their timestamps and utility keys.

CLI keeps non-TTY no-argument usage/exit 2, starts the dashboard implicitly for terminal streams, and exposes `tui --workspace`. `tui --help` and global help return before constructing operations.Service. Explicit tui on non-TTY streams fails with a terminal diagnostic before reading project configuration.

The status query uses operations.Service.Snapshot; its existing observe method leaves subprocess stdin nil. Dashboard commands pass a lifecycle context cancelled by q/Ctrl+C and by RunDashboard's defer, so pending operations observations use the existing cancellable process-group command path. No execution, mutation menu, shell or wizard behavior was added.

## Dependencies verified

- `charm.land/bubbles/v2 v2.0.0` pinned: official [go.mod](https://raw.githubusercontent.com/charmbracelet/bubbles/v2.0.0/go.mod) requires Bubble Tea v2.0.0 and Lip Gloss v2.0.0.
- `charm.land/lipgloss/v2 v2.0.0` pinned: official [go.mod](https://raw.githubusercontent.com/charmbracelet/lipgloss/v2.0.0/go.mod) requires Go 1.24.2, below this repository's Go 1.26.0.
- Existing `charm.land/bubbletea/v2 v2.0.10` retained exactly. `go mod tidy` resolved the imported components without upgrading Bubble Tea.
- Local `go doc` and module source confirmed list sizing/selection API and viewport New/options/setters before integration.

## TDD evidence

All commands used `GOCACHE=/private/tmp/paseo-sbx-management-go-cache` and the RTK proxy. Dependency retrieval and full/race tests used authorized escalation for network/cache/local-server access.

Initial CLI RED:

```text
go test ./internal/cli -run TestTUIRequiresTerminal -count=1
FAIL: args [tui]: output .../versions.lock.json: no such file or directory; want terminal
FAIL: args [tui --help]: code 1 ...; want 0 containing workspace
```

Dashboard behavior RED after a compilable no-op scaffold (a test-helper collision was corrected before the meaningful RED run):

```text
go test ./internal/tui ./internal/cli -run 'TestDashboard|TestTUIRequiresTerminal' -count=1
FAIL TestDashboardStaleResponseAndSelection: initial identity "/한국어/a" ""
FAIL TestDashboardRefreshCoalesces: initial query missing
FAIL TestDashboardFailureKeepsLastSuccess: failed refresh lost state
FAIL TestDashboardNavigationSearch: tab and j failed
FAIL TestDashboardNarrowKoreanNoColor: focused row hidden at [160 40]: ""
FAIL TestTUIRequiresTerminal: terminal/help assertions above
```

Focused GREEN after implementation:

```text
go test ./internal/tui ./internal/cli -run 'TestDashboard|TestTUIRequiresTerminal' -count=1
ok paseo-sbx/internal/tui 0.008s
ok paseo-sbx/internal/cli 0.010s
```

Additional RED/GREEN cycles caught implementation defects rather than missing symbols:

```text
go test ./internal/tui -run 'TestDashboardPendingManual|TestDashboardDetailLogTab|TestDashboardFailureDoesNotMutate' -count=1
FAIL: pending manual refresh lost metadata: IncludePaseo:false IncludeDocker:false IncludeTemplates:true
FAIL: detail/log tab did not switch
FAIL: dashboard mutated backend-owned observation

go test -race ./internal/tui ./internal/cli -count=1
ok paseo-sbx/internal/tui 1.054s
ok paseo-sbx/internal/cli 3.815s

go test ./internal/tui -run 'TestDashboardUnmanaged|TestDashboardTemplateTabVisible' -count=1
FAIL: unmanaged view does not retain an actual read-only selection
FAIL: template tab hidden (narrow screen continued displaying Workspaces)

go test -race ./internal/tui ./internal/cli -count=1
ok paseo-sbx/internal/tui 1.063s
ok paseo-sbx/internal/cli 3.822s
```

Self-review added one final RED for timestamp provenance:

```text
go test ./internal/tui -run TestDashboardUnconfirmedUpdateIsNotSuccess -count=1
FAIL: unconfirmed update shown as success: ... 업데이트: 성공 ... 업데이트 버전 미확인
```

Final GREEN after that correction (the suite was rerun because production rendering changed):

```text
go test -race ./... -count=1
? paseo-sbx/cmd/agent-wrapper [no test files]
? paseo-sbx/cmd/install-runtime [no test files]
? paseo-sbx/cmd/paseo-sbx [no test files]
ok paseo-sbx/internal/cli 4.030s
ok paseo-sbx/internal/config 1.022s
ok paseo-sbx/internal/gateway 1.020s
ok paseo-sbx/internal/launcher 1.154s
ok paseo-sbx/internal/operationlock 1.045s
ok paseo-sbx/internal/operations 2.863s
ok paseo-sbx/internal/process 1.164s
ok paseo-sbx/internal/runtimeinstall 1.079s
ok paseo-sbx/internal/sandboxupdate 1.045s
ok paseo-sbx/internal/tui 1.068s
```

`git diff --check` exited 0.

## Files changed

- internal/tui/dashboard.go: model, backend contract, queries, navigation, entry.
- internal/tui/panels.go: Bubbles components, details/provenance, responsive rendering/help.
- internal/tui/dashboard_test.go: behavior and regression tests.
- internal/cli/cli.go: terminal entry and tui flags/help.
- internal/cli/dashboard_test.go: terminal/help/legacy usage tests.
- go.mod/go.sum: compatible pinned UI dependencies.

## Self-review and limits

No blockers or unresolved interface questions. Registry corruption retains prior rows with the registry error; automatic queries do not execute guest commands. Slow metadata omitted from fast responses is retained. Manual refresh while a query is pending is remembered for the following query. A single query remains pending until its response arrives; successful/current responses arm the next 3-second tick. Slow observers are included at most once per 15-second observation cadence unless explicitly refreshed; templates are observed on their tab or explicit refresh.

PTY and real sandbox lifecycle verification belongs to later tasks and was not run here. The logs panel is intentionally a read-only placeholder for Task 4's job/log integration. No live sbx/Paseo/Docker success is claimed.

## Fix round 1 — review findings

Addressed all three Important review findings and the controller's template observation ruling, with four new named behavioral regression tests:

- `TestDashboardManualRefreshKeepsSinglePollingTimer`: three manual refreshes between scheduled ticks cannot arm additional polling timers; the original timer still triggers a query and rearms once. The model now tracks its single outstanding timer, clearing the flag only when a tick arrives.
- `TestDashboardSearchKeepsMarkerIdentityAndDetailsAligned`: filtering away the current workspace or profile selects the first visible matching identity; details and the Bubbles marker agree with it. Clearing search retains that actual selection. Empty result sets retain the authoritative identity without a conflicting visible marker.
- `TestDashboardDetailsScrollAfterReturningFromLogs`: Right to logs, Shift+Tab back to details, Down/j change only detail scroll offsets. Tab changes synchronize the detail/log tab and scrolling routes solely by focused panel. The fixture includes image provenance rows so both scroll steps have genuinely scrollable content.
- `TestDashboardTemplatesOnlyOnEntryOrManualRefresh`: three automatic core ticks while templates remain displayed omit template observations; manual refresh and leaving/reentering request templates. The query now consumes only the pending entry/manual request flag.

RED command:

```text
GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/tui -run 'TestDashboardManualRefreshKeepsSinglePollingTimer|TestDashboardSearchKeepsMarkerIdentityAndDetailsAligned|TestDashboardDetailsScrollAfterReturningFromLogs|TestDashboardTemplatesOnlyOnEntryOrManualRefresh' -count=1
FAIL TestDashboardManualRefreshKeepsSinglePollingTimer: manual response added another polling timer before existing timer fired
FAIL TestDashboardSearchKeepsMarkerIdentityAndDetailsAligned/workspace: filter left wrong identity runtime-a/a-one (visible marker /workspace/b)
FAIL TestDashboardSearchKeepsMarkerIdentityAndDetailsAligned/profile: filter left wrong identity runtime-a/a-one (visible marker a-two)
FAIL TestDashboardDetailsScrollAfterReturningFromLogs: details/log offsets 0/0, want 1/0
FAIL TestDashboardTemplatesOnlyOnEntryOrManualRefresh: displayed template tab caused repeated automatic template query
```

Individual GREEN commands confirmed timer, filtered identity and template behavior. The first scrolling GREEN attempt caught an insufficient fixture (the original detail body allowed only one scroll step), so the fixture was extended with two real initial-version rows; expectations were retained.

Covering package GREEN:

```text
GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./internal/tui ./internal/cli -count=1
ok paseo-sbx/internal/tui 4.125s
ok paseo-sbx/internal/cli 3.773s
```

Full suite GREEN, run once after final fixes:

```text
GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./... -count=1
? paseo-sbx/cmd/agent-wrapper [no test files]
? paseo-sbx/cmd/install-runtime [no test files]
? paseo-sbx/cmd/paseo-sbx [no test files]
ok paseo-sbx/internal/cli 4.044s
ok paseo-sbx/internal/config 1.016s
ok paseo-sbx/internal/gateway 1.024s
ok paseo-sbx/internal/launcher 1.159s
ok paseo-sbx/internal/operationlock 1.047s
ok paseo-sbx/internal/operations 2.883s
ok paseo-sbx/internal/process 1.171s
ok paseo-sbx/internal/runtimeinstall 1.081s
ok paseo-sbx/internal/sandboxupdate 1.045s
ok paseo-sbx/internal/tui 4.103s
```

Commands used the RTK proxy; race suite execution used authorized escalation for existing local-server tests. `git diff --check` exited 0. Self-review found no remaining issue in this fix scope. Only dashboard.go and dashboard_test.go changed; no new job/wizard/PTY scope was added. The earlier report's phrase “templates are observed on their tab” is superseded by the entry/manual-only behavior described here.
