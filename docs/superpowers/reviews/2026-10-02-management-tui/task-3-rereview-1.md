- **I1: Completed manual refreshes create additional polling chains** — ADDRESSED. `internal/tui/dashboard.go:43` tracks the outstanding timer; accepted snapshot responses arm a timer only when none is outstanding (`:155–159`), and the timer message clears that state before attempting its query (`:94–96`). A manual response arriving before the original timer fires therefore cannot create a second chain. If that timer fires during a pending manual query, its completion arms the next timer. `TestDashboardManualRefreshKeepsSinglePollingTimer` (`internal/tui/dashboard_test.go:310–338`) checks three completed manual refreshes, the surviving original timer, one subsequent timer, and the query count.
- **I2: Filtering can highlight a different row while stored selection/details retain the previous ID** — ADDRESSED. `internal/tui/dashboard.go:333–344` chooses a visible workspace identity when the previous selection is filtered away and reconciles its profile before building sandbox rows. `:360–378` chooses a visible profile identity and positions both list cursors by the stored IDs. Empty filtered lists have no competing row marker and retain the stored identity. `TestDashboardSearchKeepsMarkerIdentityAndDetailsAligned` (`internal/tui/dashboard_test.go:340–379`) checks workspace and profile filtering against authoritative IDs, detail content, the actual Bubbles selected item, and clearing search.
- **I3: Returning from logs to details leaves arrows/jk scrolling logs** — ADDRESSED. Tab and Shift+Tab synchronize the detail/log tab with the focused panel (`internal/tui/dashboard.go:190–199`), and scrolling now selects logs only for `Focus == 3` (`:247–253`). `TestDashboardDetailsScrollAfterReturningFromLogs` (`internal/tui/dashboard_test.go:381–403`) returns through Shift+Tab and confirms Down and j advance the details viewport while the logs offset stays unchanged.
- **Binding spec gap: Automatic core polls include templates whenever the template tab is displayed** — ADDRESSED. `internal/tui/dashboard.go:76` now includes templates solely from `templatesWanted`; the flag is consumed only after the pending-query guard (`:69–77`). Explicit refresh sets it (`:65–67`), and template entry sets it (`:210–215`), so a request queued during an existing query is retained without recurring template polling. `TestDashboardTemplatesOnlyOnEntryOrManualRefresh` (`internal/tui/dashboard_test.go:405–434`) verifies entry, three automatic polls while the tab stays displayed, explicit refresh, and reentry.

### New Breakage in the Fix Diff

- None. The timer changes preserve pending-query coalescing and stale-response rejection; the selection changes build sandbox rows after the workspace identity changes; the scrolling changes follow the focused panel; template request flags remain pending until an actual query starts.

### Out-of-Scope Observations

- None.

### Checks and Test Evidence

- Read the supplied `review-a9e4b66..30e182c.diff` once, the task brief, global constraints, previous review, and appended fix report. No git commands were run.
- Inspected line-numbered current `internal/tui/dashboard.go` to confirm the replacement expressions obscured by the supplied formatted diff and the immediate control-flow context of the fixes. The checkout has no `.codegraph/` directory.
- The appended report names all four new covering regression tests, supplies their initial failure output, and supplies successful final output for `go test -race ./internal/tui ./internal/cli -count=1` and `go test -race ./... -count=1`. The four added tests in the fix diff exercise the reported failure sequences and assert the corrected behavior. The scroll fixture adds real detail rows while retaining the two-step scrolling expectation. These reported runs cover the changed production package and tests; no claim of independently rerunning those suites is made.
- No specific unresolved doubt required an additional test run. No suite was rerun, no subagent was dispatched, and no product, index, HEAD, or branch state was changed. This report is the sole written artifact.

### Verdict

- **Fix round:** All findings addressed, no new Critical/Important breakage. No findings remain open.
