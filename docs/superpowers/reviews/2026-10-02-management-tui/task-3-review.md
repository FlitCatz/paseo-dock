### Spec Compliance

- ❌ Issues found: polling loses the single 3-second cadence after a completed manual refresh (`internal/tui/dashboard.go:153`, `internal/tui/dashboard.go:197`); filtering can show a selected row whose ID differs from the dashboard selection (`internal/tui/dashboard.go:334`); detail scrolling can target the logs viewport (`internal/tui/dashboard.go:237`). These affect the requested polling and navigation/selection behavior.
- ✅ The Backend and dashboard entry contracts are present (`internal/tui/dashboard.go:18`, `internal/tui/dashboard.go:55`, `internal/tui/dashboard.go:347`). Non-TTY explicit entry fails before configuration loading, no-argument non-TTY use retains exit 2, and help returns before service creation (`internal/cli/cli.go:52`, `internal/cli/cli.go:64`, `internal/cli/dashboard_test.go:9`). The added code and tests are Go.
- ⚠️ Cannot verify from diff: actual implicit terminal startup, PTY behavior, and live subprocess cancellation. The controller should use the later terminal/lifecycle validation; this review did not rerun reported passing tests. The unchanged wizard and existing execution behavior are outside the supplied hunks; the reported full race run is evidence, but no new live verification is claimed.

### Strengths

- Current-request filtering and the pending guard reject stale results and prevent concurrent snapshots (`internal/tui/dashboard.go:68`, `internal/tui/dashboard.go:96`). Selection reconciliation uses workspace/profile identities rather than row positions (`internal/tui/dashboard.go:283`, `internal/tui/dashboard_test.go:51`).
- Failed observations retain last successful data/time while marking runtime state unknown; copied workspace/sandbox slices protect backend-owned rows (`internal/tui/dashboard.go:100`, `internal/tui/dashboard.go:108`, `internal/tui/dashboard.go:142`, `internal/tui/dashboard_test.go:242`, `internal/tui/dashboard_test.go:286`). Omitted slow observations and failed template rows are retained (`internal/tui/dashboard.go:113`).
- Responsive layouts, display-width truncation, NO_COLOR stripping, and long Korean selections have concrete regression coverage (`internal/tui/panels.go:228`, `internal/tui/panels.go:256`, `internal/tui/dashboard_test.go:150`, `internal/tui/dashboard_test.go:225`). Details keep image provenance, updater state, and independent version checks separate (`internal/tui/panels.go:122`, `internal/tui/panels.go:128`).
- Automatic dashboard paths only call Snapshot; mutation and shell methods are absent from these paths and guarded by panic test doubles (`internal/tui/dashboard.go:81`, `internal/tui/dashboard_test.go:30`).

### Issues

#### Critical (Must Fix)

- None.

#### Important (Should Fix)

- **Duplicate polling chains — `internal/tui/dashboard.go:153`, `internal/tui/dashboard.go:197`.** Every accepted response schedules another tick, including a manual refresh completed while a previous tick is still scheduled. For example, the initial response schedules a tick for t=3; `r` at t=1 completes and schedules a second for t=4. With fast observations both ticks start queries, and each response perpetuates its own chain. Repeated manual refreshes can increase polling indefinitely; the pending guard only collapses ticks that overlap an active query. Maintain one scheduled polling timer (or reject obsolete timer messages) and add a regression covering a manual refresh between already scheduled ticks.
- **Filtered highlight and authoritative identity diverge — `internal/tui/dashboard.go:334`, `internal/tui/panels.go:31`.** `SetItems` retains a positional cursor, and `syncLists` only selects a matching authoritative ID if that ID survives filtering. Start with `ProfileID=a-one`, search `two`: the sole visible row `a-two` receives the selection marker, while `ProfileID` and details remain `a-one`. Workspace filtering has the same problem. The current search test only filters to the already selected item (`internal/tui/dashboard_test.go:133`). Keep the displayed selection consistent with the stored ID; if preserving a hidden selection, suppress the misleading marker and indicate that the selected item is filtered out. Cover filtering away the selected workspace/profile, then clearing search.
- **Visible details scroll the logs viewport — `internal/tui/dashboard.go:237`.** Open logs with Right (`Focus=3`, `detailTab=1`), then Shift+Tab to details (`Focus=2`). The renderer shows details (`internal/tui/panels.go:238`; wide screens also show both panels), but Up/Down or j/k still update logs because `detailTab==1`. Route scrolling by the actual focused visible panel, or synchronize the tab state when focus changes. Extend the detail/log navigation test to verify scroll offsets after returning to details.

#### Minor (Nice to Have)

- None beyond the regression coverage attached to the Important findings.

### Assessment

**Task quality:** Needs fixes.

**Reasoning:** The implementation has clear observation/state/rendering boundaries and substantial behavioral coverage, but the timer lifecycle and two UI identity/navigation mismatches prevent approval. Existing passing tests do not exercise these sequences.

### Checks and Review Limits

- Read the supplied `review-714968f..a9e4b66.diff` once in consecutive portions; no changed source file was reread and no git command was run.
- Focused cross-boundary risk check: inspected unchanged `internal/operations/snapshot.go` to assess partial SBX failures, runtime reconstruction, TemplateRows/VersionCheck compatibility, observation bounds, and subprocess cancellation/stdin claims. Snapshot reconstructs managed registry rows on SBX failure; observations use bounded capture, a 5-second timeout and process.Command, with no stdin assignment. No missing operations API was found.
- Focused dependency-contract check: inspected Bubbles v2.0.0 `list/list.go:380`, `list/list.go:395`, `list/list.go:467` to verify that SetItems retains positional selection rather than reconciling item identity; this substantiates the filtered-highlight finding.
- Reviewed the implementer's RED/GREEN and final race-suite output in `task-3-report.md`; final reported output is clean. No tests were rerun, no subagents were dispatched, and no source/index/branch state was changed. This report is the sole written artifact.
