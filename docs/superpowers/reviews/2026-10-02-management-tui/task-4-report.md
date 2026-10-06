# Task 4 report: dashboard management jobs

Worktree: `/Users/dev-r/devsbx/paseo-sbx/.worktrees/management-tui`
Branch: `feat/management-tui`; base: `30e182c`.

## Implementation

- Enter opens an action menu with disabled reasons. Selected sandbox update (`u`), workspace active-target update, shell (`s`), stop, explicit guest version checks, and Paseo register/unregister use the existing Backend. Confirmation freezes target entries, target slices, selection pairs/utilities and options before enqueue; polling and navigation cannot retarget requests.
- Stop confirms its effect on active sessions; updates/version checks warn that sbx exec can start a stopped guest. Registration confirms workspace and `~/.paseo/config.json` configuration scope. Registration requires existing active profiles; no RestoreSelection producer or Task 5 wizard/build/load/prune work was added, per controller ruling.
- A dashboard-owned FIFO permits one Execute at a time while Snapshot continues. Failed jobs preserve their request/result, remaining resources and errors. `R` confirms a retry of the original retained request, independent of current selection. Queue plus completed history is limited to 20 jobs; only completed history is evicted.
- Each active job has a cancellable child context. `c` immediately shows cancellation requested and cancels queued requests. Busy q/ctrl+c offers keep-running or cancel-and-wait; idle quit cancels observation context. NeedsVerification remains explicit despite later read-only observations; guest installer termination is never inferred from a host cancellation or sbx runtime status.
- Worker events only update synchronized bounded storage and coalesced nonblocking wakeups. Pulse/completion messages own stage changes in the model. Progress shows stage, spinner and elapsed time. A completed worker terminates its event wait, avoiding a closed-channel busy loop.
- Logs retain at most 256 KiB of valid UTF-8 per job, stripping terminal controls incrementally across writer boundaries. Stdout/stderr parsers are separate; other stages share a bounded parser slot. CSI, OSC, string/charset escapes and C1 controls are stripped. Final rendering also sanitizes structured version/resource result metadata.
- Shell validates a frozen target through ShellCommand, hands the terminal to real tea.ExecProcess, shows callback failures and refreshes afterward. Mutation and queued work disable shell. It never calls Execute or adds hidden auto-update.
- Job/shell completion requests a fresh snapshot. A completion arriving during an earlier observation schedules another observation after that response; it does not add another polling timer or repeatedly query templates.
- Background Service execution clears In only on its private per-request copy when an event observer is supplied. Real subprocess tests prove terminal input is untouched by background jobs and remains available to existing CLI execution without an observer.

## TDD and debugging evidence

All commands below ran from the worktree with the custom cache. The RTK wrapper is part of each exact command.

### Initial management entry RED → GREEN

RED:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/tui -run 'Test(Job|OneMutation|Cancel|Bounded|Shell)'
--- FAIL: TestJobTargetsRemainFrozen (0.00s)
    jobs_test.go:38: missing fixed target confirmation/start warning: "PASEO SBX ..."
FAIL paseo-sbx/internal/tui 0.007s
```

The preexisting read-only dashboard ignored u. Adding action confirmation and dispatch produced GREEN on the same command: `ok paseo-sbx/internal/tui 0.007s`.

### Background stdin RED → GREEN

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/operations -run TestExecuteBackgroundDoesNotConsumeTerminalInput
--- FAIL: TestExecuteBackgroundDoesNotConsumeTerminalInput (0.01s)
    background subprocess consumed terminal input: remaining "" events "Starting stop[sbx stop one]stdin:terminal keys"
FAIL paseo-sbx/internal/operations 0.018s
```

Root cause: the execution copy inherited Service.In while changing only output streams. Clearing that copy's In for observed jobs produced GREEN: `ok paseo-sbx/internal/operations 0.022s`. The same test executes an actual Go helper subprocess without an observer and verifies the existing CLI receives `terminal keys` and Service.In remains the original reader.

### Queue/event/log edge cases RED → GREEN

Exact focused command:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/tui -run 'Test(Job|OneMutation|Cancel|Bounded|Shell)'
--- FAIL: TestJobCompletionRequestsFreshSnapshotEvenDuringObservation
    completion was lost behind in-flight snapshot
--- FAIL: TestJobFinishedPulseDoesNotSpin
    finished worker spawned another immediate pulse before completion delivery
--- FAIL: TestBoundedSafeLogs
    fragmented controls/UTF-8 leaked: "한빨강끝B"
FAIL paseo-sbx/internal/tui 0.009s
```

Root causes were an already-pending refresh discarding a completion request, a closed worker notification channel rearming an immediate waiter, and ESC intermediate charset bytes ending sanitizer state too early. A pending-work-refresh flag, finished pulse marker, and intermediate escape state fixed their respective boundary. The same command passed: `ok paseo-sbx/internal/tui 0.034s`.

### Retained retry RED → GREEN and real ExecProcess callback

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/tui -run 'Test(JobRetry|ShellExec)'
--- FAIL: TestJobRetryUsesFailedRequest
    retry lost failed immutable target
FAIL paseo-sbx/internal/tui 0.529s
```

No retained-request retry existed. Adding R confirmation using the retained request fixed that behavior. Actual tea.Program/tea.ExecProcess ran a Go child exiting 7, delivered its callback error, refreshed and restored the model without Execute. The subsequent covering race command passed:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./internal/tui -run 'Test(Job|OneMutation|Cancel|Bounded|Shell)'
ok paseo-sbx/internal/tui 1.790s
```

### Small-screen confirmation RED → GREEN

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/tui -run TestJobConfirmationScrollsOnSmallScreen
--- FAIL: TestJobConfirmationScrollsOnSmallScreen
    small screen cannot reach start warning: "PASEO SBX ..."
FAIL paseo-sbx/internal/tui 0.007s
```

Root cause: the overlay consumed navigation and clipped raw content. A wrapped confirmation viewport with keyboard scrolling and visible action selection fixed this. Same command GREEN: `ok paseo-sbx/internal/tui 0.007s`.

### Message-owned stage and typed cancellation RED → GREEN

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/tui -run 'Test(JobStages|CancelWord)'
--- FAIL: TestJobStagesAreDeliveredByMessages
    worker mutated rendered stage before message delivery
--- FAIL: TestCancelWordInFailureDoesNotClaimCancellation
    unrelated error text claimed confirmed cancellation
FAIL paseo-sbx/internal/tui 0.007s
```

Concrete reproduction traced the first defect to rendering the worker's synchronized stage directly rather than the stage received by the model. The second defect matched cancellation words in error messages instead of context error identity. Rendering the message-owned stage and using errors.Is fixed their boundaries. Subsequent `go test -race ./internal/tui ./internal/operations` passed (`tui 5.609s`, `operations 2.658s`). Systematic debugging guidance was read; these reproductions and source-level root causes are preserved here.

### Structured result log safety RED → GREEN

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/tui -run TestBoundedSafeLogsSanitizeResultMetadata
--- FAIL: TestBoundedSafeLogsSanitizeResultMetadata
    guest result metadata bypassed log safety: "... agent: \x1b]52;c;SECRET\av1 ..."
FAIL paseo-sbx/internal/tui 0.007s
```

Root cause: only streamed text passed the sanitizer; result versions/errors were rendered directly. Sanitizing the complete log rendering fixed the untrusted structured-data path. Covering command GREEN:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./internal/tui -run 'TestBoundedSafeLogs'
ok paseo-sbx/internal/tui 1.224s
```

## Final verification

The initial full race command in the ordinary sandbox failed in existing `internal/cli/TestPluginAgentCreation/create`: `httptest: failed to listen on a port: listen tcp6 [::1]:0: bind: operation not permitted`. All other packages passed. The authorized escalation allowed the existing loopback tests; no tests or production behavior were changed to bypass them.

After the final production change:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./...
? cmd/agent-wrapper [no test files]
? cmd/install-runtime [no test files]
? cmd/paseo-sbx [no test files]
ok internal/cli 3.822s
ok internal/config (cached)
ok internal/gateway (cached)
ok internal/launcher (cached)
ok internal/operationlock (cached)
ok internal/operations (cached)
ok internal/process (cached)
ok internal/runtimeinstall (cached)
ok internal/sandboxupdate (cached)
ok internal/tui 5.566s
exit 0
```

`rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go vet ./...` exited 0 with no output. An earlier vet found an unreachable return in the test command helper; it was removed, then vet reran successfully. `rtk proxy git diff --check` exited 0 with no output.

The 17 management tests include a real operations.Service and Go sbx subprocess integration: one stop waits while another stays queued, actual snapshot commands continue, the next stop starts after completion, and changing a queued registry entry rejects the stale request without stopping either the old or replacement sandbox. Concurrent stdout/stderr writers and readers verify separate fragmented UTF-8 streams, valid bounded snapshots, and race-free synchronization. History eviction is exercised through real job completions.

## Changed files

- `internal/tui/jobs.go`: actions, immutable confirmation, queue, cancellation, message transitions, shell lifecycle and bounded safe logs.
- `internal/tui/jobs_test.go`: behavioral management tests and Go subprocess fixtures.
- `internal/tui/dashboard.go`: management dispatch, modal state and completion-refresh integration.
- `internal/tui/panels.go`: keyboard help, job logs and small-screen modal rendering.
- `internal/operations/service.go`: private background stdin isolation.
- `internal/operations/service_test.go`: actual background/CLI stdin regression coverage.
- This report.

## Self-review and limitations

Reviewed the diff and task contracts. Requests do not share target/selection slices; model fields only change on Update; job logging is synchronized; failures cannot wedge dispatch; shell cannot overlap active/queued work. Existing service revalidation, lock ordering and updater remain the execution path. Polling still has one timer; template reads remain tab-entry/manual only. No new dependency, second scheduler, guest stop, rollback, registration restoration or Task 5 advanced action was introduced.

All required Task 4 automated checks passed. Real tea.ExecProcess callback behavior is covered without a terminal renderer. Actual PTY terminal restoration and disposable live sbx/Docker/Paseo lifecycle validation remain later integration scope; no existing live resource or configuration was mutated and no live/PTY success is claimed. `jobs.go` contains the planned task responsibilities in one file and is substantial; future expansion should keep action/queue/log responsibilities deliberate.

## Review fix round 1 — Important I1

Fix base: `3c568a7`. Scope is the frozen provider mutation scope only; the deferred semantic-stage finding and Task 5 registration restoration are unchanged.

Read the review and receiving-code-review guidance, verified the feedback against the actual service/provider path, and used the previously read systematic debugging guidance to isolate the boundary. Root cause: the dashboard already froze each active target, but Execute only resolved the selected target and discarded the full active-list baseline. register/unregister then reread current activation while writing registry/provider state. Thus a queued change to another entry or Active altered the confirmed effect.

The fix adds shared-service validation after the existing mutation locks and selected-target validation, before any registry/launcher/generated-provider/config/backup write. It checks every explicit target's entry and workspace/runtime identity and compares the complete active profile set, irrespective of order. Invalid or changed scope returns an error without mutation. The existing merge/union logic and lock acquisition remain unchanged.

Per controller ruling, nil Targets retains legacy CLI resolution; a nonnil slice, including empty, confirms the complete active baseline. The dashboard now constructs an explicit empty baseline for workspace actions and its existing freeze/enqueue/dispatch copies preserve that distinction. The request field documents the contract.

### RED evidence

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/operations -run TestExecuteProviderScope
--- FAIL: TestExecuteProviderScopeRejectsChangedConfirmation
    register/nonselected_entry: provider mutation accepted changed confirmed nonselected entry
    register/active_removed: provider mutation accepted changed confirmed active removed
    register/active_added: provider mutation accepted changed confirmed active added
    unregister/nonselected_entry: provider mutation accepted changed confirmed nonselected entry
    unregister/active_removed: provider mutation accepted changed confirmed active removed
    unregister/active_added: provider mutation accepted changed confirmed active added
    unregister/empty_became_active: provider mutation accepted changed confirmed empty became active
    [Each case also showed Changed=true, registry/config or artifact writes and a backup.]
FAIL paseo-sbx/internal/operations 0.549s
```

This run additionally exposed three unchanged-unregister fixture assertions caused by an uncanonicalized initial managed launcher command (`/var` versus `/private/var`). Canonicalizing that test fixture made the unchanged/legacy cases pass without a production change:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/operations -run TestExecuteProviderScopeUnchangedAndLegacyCLI
ok paseo-sbx/internal/operations 0.251s
```

The separate UI regression caught loss of the explicit empty baseline before the fix:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/tui -run 'TestJob(ProviderConfirmations|InactiveUnregister)'
--- FAIL: TestJobInactiveUnregisterKeepsExplicitEmptyScope
    inactive unregister omitted its explicit activation baseline
FAIL paseo-sbx/internal/tui 0.007s
```

Checking the full explicit identity found that a request without a selected Profile could name workspace B with runtime A while its Targets named A. The scope validator must reject that identity disagreement as well:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/operations -run TestExecuteProviderScopeRejectsMixedWorkspace
--- FAIL: TestExecuteProviderScopeRejectsMixedWorkspace/register
    explicit provider scope ignored selected workspace identity
--- FAIL: TestExecuteProviderScopeRejectsMixedWorkspace/unregister
    explicit provider scope ignored selected workspace identity
FAIL paseo-sbx/internal/operations 0.095s
```

### GREEN evidence

After validating the frozen scope and preserving nonnil empty construction:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/operations -run TestExecuteProviderScope
ok paseo-sbx/internal/operations 0.439s
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/tui -run 'TestJob(ProviderConfirmations|InactiveUnregister)'
ok paseo-sbx/internal/tui 0.007s
```

After the mixed-workspace identity check, the final focused race commands passed:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./internal/operations -run TestExecuteProviderScope
ok paseo-sbx/internal/operations 1.528s
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./internal/tui -run 'TestJob(ProviderConfirmations|InactiveUnregister)'
ok paseo-sbx/internal/tui 1.023s
```

Coverage verifies both provider actions reject changed nonselected entries and Active additions/removals; explicit empty unregister rejects later activation; unchanged and reordered confirmed scopes succeed; nil-target CLI requests still use current activation/entries; unrelated workspace and custom providers survive the existing union/merge. UI queue tests confirm changes to a nonselected row and activation cannot alter the retained request, and an inactive unregister baseline stays explicitly empty through confirmation/enqueue/dispatch. Failure assertions inspect actual registry/config bytes, generated/launcher artifacts and backups rather than only error strings.

### Final verification and changed files

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./internal/tui ./internal/operations
ok paseo-sbx/internal/tui 5.732s
ok paseo-sbx/internal/operations 3.152s
```

Because this changes shared mutation validation, the full suite ran once on the final candidate with authorized escalation for the existing httptest listeners:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./...
? paseo-sbx/cmd/agent-wrapper [no test files]
? paseo-sbx/cmd/install-runtime [no test files]
? paseo-sbx/cmd/paseo-sbx [no test files]
ok paseo-sbx/internal/cli 3.821s
ok paseo-sbx/internal/config (cached)
ok paseo-sbx/internal/gateway (cached)
ok paseo-sbx/internal/launcher (cached)
ok paseo-sbx/internal/operationlock (cached)
ok paseo-sbx/internal/operations (cached)
ok paseo-sbx/internal/process (cached)
ok paseo-sbx/internal/runtimeinstall (cached)
ok paseo-sbx/internal/sandboxupdate (cached)
ok paseo-sbx/internal/tui (cached)
exit 0
```

`rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go vet ./...` and `rtk proxy git diff --check` exited 0 with no output.

Changed files: `internal/operations/providers.go`, `service.go`, `requests.go`, `service_test.go`; `internal/tui/jobs.go`, `jobs_test.go`; this report. Self-review checked the validation occurs under the original mutation locks and precedes every provider-side write; nil behavior is gated in Execute; confirmation copies preserve an explicit empty slice. No second scheduler, new lock scheme, restoration, merge change or live resource mutation. No remaining I1 concern found; previously reported PTY/live integration limits remain.
