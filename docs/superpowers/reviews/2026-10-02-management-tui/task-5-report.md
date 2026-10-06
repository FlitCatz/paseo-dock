# Task 5 implementation report

Status: DONE. Candidate commit subject: `feat: integrate creation and advanced management actions`.

## Implementation

- `n` opens Bubbles directory picker plus workspace path and memory inputs. Enter opens the existing selection wizard embedded in the dashboard. Its final confirmation displays profile combinations, utilities, CPU 2, memory, build/load and Paseo home/backup/merge scope. Confirmed creation uses `BuildFirst` and the existing single queue.
- Added `NewEmbeddedModel` and `WizardFinishedMsg` with `tui.Result` and error. Standalone success/cancellation still returns `tea.Quit`; embedded completion returns to its parent. Completion clones selection and carries private child identity so delayed success/cancellation cannot target a newer child. Path and memory are frozen when entering the child and selection/options are frozen when accepted.
- `a` provides saved-selection image build, template load, read-only templates entry and prune preview. All execution, including dry-run preview, uses the existing queue. Templates retain entry/manual-only queries; polling is unchanged. Added `n`/`a` to help.
- Explicit inactive TUI registration uses `RestoreSelection`. Per controller ruling, `Targets` remains the frozen prior active scope and added `RestoreTargets` pins all desired saved-selection entries. Full saved-selection equality, complete identities, workspace/runtime, adapter, methodology and nonempty image are validated under existing mutation locks before activation. Active registration retains existing confirmed-scope behavior; legacy CLI empty-active register still rejects. Shared workspace/custom providers survive.
- Added `PruneTargets`; nil retains CLI discovery, nonnil including empty is the exact confirmed list. Prune checks complete profile identity/path, locks affected sandbox keys, rechecks after lock acquisition and immediately before each metadata deletion, and keeps now-existing/unconfirmed/newly-missing workspaces. Registry is deleted last, allowing validation before each preceding metadata removal. Update history and lock inodes are retained and no sandbox removal command executes.
- Addressed the Task4 deferred semantic-stage defect: stdout/stderr append logs without overwriting the current semantic stage.
- Filepicker added only its existing transitive `github.com/dustin/go-humanize v1.0.1`; Bubble Tea, Bubbles and Lip Gloss pins are unchanged.

## RED / GREEN evidence

All commands below used `rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache` in the management-tui worktree.

1. Initial behavioral RED: `go test ./internal/tui ./internal/operations`:

```text
--- FAIL: TestCreationRequestFrozen (0.00s)
    create_test.go:16: n did not open directory picker/path and memory entry
--- FAIL: TestJobOutputPreservesSemanticStage (0.00s)
    create_test.go:25: output replaced semantic stage: stderr
FAIL paseo-sbx/internal/tui 4.350s
```

The first restoration fixture used an unsupported native pair, so that failure was a fixture error, corrected before restoration implementation. The corrected meaningful RED was `go test ./internal/operations -run TestRestoreRegistration -count=1`:

```text
--- FAIL: TestRestoreRegistration (0.06s)
    restore_test.go:27: complete saved selection was not restored: no complete active selection; finish create before registering
FAIL paseo-sbx/internal/operations 0.064s
```

The first embedded API test also failed compilation because `NewEmbeddedModel`/`WizardFinishedMsg` did not yet exist. This confirms API absence, rather than a runtime behavioral RED. Its completion/cancellation assertions passed after adding the implementation.

2. Creation isolation RED: `go test ./internal/tui -run TestCreationRequestFrozen -count=1`:

```text
--- FAIL: TestCreationRequestFrozen (0.00s)
    create_test.go:40: creation request was mutable/incomplete: ... Memory:8g ...
FAIL paseo-sbx/internal/tui 0.008s
```

The child had confirmed 4g; its later mutation changed the queued request to 8g. Path/memory freezing fixed this. GREEN: `go test ./internal/tui -run 'TestCreationRequestFrozen|TestEmbeddedWizardReturnsToDashboard|TestJobOutputPreservesSemanticStage' -count=1`:

```text
ok paseo-sbx/internal/tui 0.008s
```

A later scope-disclosure RED showed the confirmation omitted `~/.paseo/config.json`; the embedded view now displays it. Focused GREEN was `ok paseo-sbx/internal/tui 0.007s`.

3. Inactive dashboard registration RED: `go test ./internal/tui -run TestDashboardRestoreFreezesDesiredEntries -count=1`:

```text
--- FAIL: TestDashboardRestoreFreezesDesiredEntries (0.00s)
    create_test.go:83: completed inactive workspace could not register 완료된 활성 조합 없음
FAIL paseo-sbx/internal/tui 0.006s
```

GREEN: `go test ./internal/tui ./internal/operations -run 'TestRestore|TestDashboardRestore' -count=1`:

```text
ok paseo-sbx/internal/tui 0.007s
ok paseo-sbx/internal/operations 0.458s
```

Restoration tests cover changed inactive desired entry, saved selection, prior activation, missing selection/entry, absent identity, partial desired scope, incompatible adapter/image, legacy rejection, and shared/custom provider preservation with no activation/provider write on rejection.

4. Frozen prune RED: `go test ./internal/operations -run 'TestPrunePreviewIsFrozen|TestPruneRechecksAfterProviderSync' -count=1`:

```text
--- FAIL: TestPrunePreviewIsFrozen (0.24s)
    --- FAIL: TestPrunePreviewIsFrozen/newly_missing (0.06s)
        prune_test.go:47: unconfirmed registry removed ... no such file or directory
    --- FAIL: TestPrunePreviewIsFrozen/entry_replaced (0.05s)
        prune_test.go:44: changed/unconfirmed registry removed ... no such file or directory
    --- FAIL: TestPrunePreviewIsFrozen/profile_added (0.05s)
        prune_test.go:44: changed/unconfirmed registry removed ... no such file or directory
    --- FAIL: TestPrunePreviewIsFrozen/empty_confirmation (0.05s)
        prune_test.go:44: changed/unconfirmed registry removed ... no such file or directory
--- FAIL: TestPruneRechecksAfterProviderSync (0.04s)
    prune_test.go:64: prune removed workspace metadata recreated during provider sync ... no such file or directory <nil>
FAIL paseo-sbx/internal/operations 0.290s
```

GREEN: `go test ./internal/operations -run 'TestPrunePreviewIsFrozen|TestPruneRechecksAfterProviderSync|TestPruneDuringUpdatePreservesSandboxLock|TestPruneReturnsMissingTargets' -count=1`:

```text
ok paseo-sbx/internal/operations 0.300s
```

5. Delayed child completion RED: `go test ./internal/tui -run TestOldWizardCompletionCannotCreateNewWorkspace -count=1`:

```text
--- FAIL: TestOldWizardCompletionCannotCreateNewWorkspace (0.00s)
    create_test.go:193: stale child completion created against the new workspace
FAIL paseo-sbx/internal/tui 0.008s
```

Private source identity fixed the reproduced mismatch. GREEN: `go test ./internal/tui -run 'TestOldWizardCompletionCannotCreateNewWorkspace|TestCreationRequestFrozen|TestEmbeddedWizardReturnsToDashboard' -count=1`:

```text
ok paseo-sbx/internal/tui 0.009s
```

The final test includes delayed success and delayed cancellation.

Additional behavior guards: advanced build/load requests retain saved selection and original workspace; template entry requests templates; prune preview-to-confirm keeps its original missing identities; incompatible registry image/settings cannot reuse or write a registry; incompatible utilities fail before Docker; partial selection-changing creation keeps previous activation/selection. Existing image/create safety behavior did not require production changes and its new boundary tests passed initially.

## Stabilization and final verification

The focused full package run found `TestJobProviderConfirmationsFreezeActiveScope/5` failing because the initial implementation required saved selection for existing active registration. Fixed this by enabling explicit restoration only for an empty frozen prior scope. `go test ./internal/tui -run 'TestJobProviderConfirmationsFreezeActiveScope|TestDashboardRestoreFreezesDesiredEntries' -count=1` passed: `ok paseo-sbx/internal/tui 0.007s`.

The first sandbox full race run failed solely at existing `TestPluginAgentCreation/create`: `httptest: failed to listen on a port: listen tcp6 [::1]:0: bind: operation not permitted`. Changed packages passed. An authorized escalation enabled the existing localhost test server. Final command after the delayed-completion guard and both success/cancellation cases: `rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./...`, exit 0:

```text
? paseo-sbx/cmd/agent-wrapper [no test files]
? paseo-sbx/cmd/install-runtime [no test files]
? paseo-sbx/cmd/paseo-sbx [no test files]
ok paseo-sbx/internal/cli (cached)
ok paseo-sbx/internal/config (cached)
ok paseo-sbx/internal/gateway (cached)
ok paseo-sbx/internal/launcher (cached)
ok paseo-sbx/internal/operationlock (cached)
ok paseo-sbx/internal/operations (cached)
ok paseo-sbx/internal/process (cached)
ok paseo-sbx/internal/runtimeinstall (cached)
ok paseo-sbx/internal/sandboxupdate (cached)
ok paseo-sbx/internal/tui 5.590s
```

`rtk proxy git diff --check`: exit 0, no output. Self-review covered changed production diffs and new tests. Only Go implementation/test code was added; no live commands/default home writes, pushes or merges were performed. No remaining correctness concerns identified within Task5; live/PTY checks remain the controller's later scope.

## Files

- `go.mod`, `go.sum`
- `internal/tui/create.go`, `create_test.go`, `wizard.go`, `wizard_test.go`, `dashboard.go`, `jobs.go`, `panels.go`
- `internal/operations/requests.go`, `service.go`, `providers.go`, `workspaces.go`, `service_test.go`, `create_test.go`, `restore_test.go`, `prune_test.go`

Controller ruling: `RestoreTargets` extension explicitly approved; frozen prior scope remains in `Targets`. Frozen prune filtering and repeated locked validation approved. The substantial existing jobs file was edited narrowly; creation/advanced input handling stays in planned `create.go`.

## Review fix round 1 — I1 parent confirmation ownership

Fix base: `79e961c`. Read the complete Task5 review and receiving-code-review/systematic-debugging guidance before changing code. Verified the root cause: child completion had source identity, but saved-selection lookup responses and prune job completion installed confirmations unconditionally, so request cloning did not protect which parent action was being confirmed.

Added a dashboard flow generation. New action/advanced/creation flows, Esc/exit/cancellation, confirmation acceptance and retry advance it. Saved-selection messages carry the initiating generation; preview jobs capture it when enqueued, preserving ownership while waiting behind another job. Both async response paths check generation and absence of a newer modal/child before showing confirmation or errors. Superseded previews still finish their queue work without presenting a confirmation. No operations API, request cloning, polling timer, queue scheduling, or backend execution behavior changed.

New deterministic `flow_test.go` tests exercise the actual dashboard, lookup commands and queue:

- Delayed advanced lookup cannot overwrite a newer frozen stop confirmation or a newer saved-selection load confirmation.
- Esc/cancel, creation entry, advanced menu entry and action menu entry invalidate abandoned lookup responses.
- A delayed preview and a preview queued behind another job cannot replace a newer frozen stop confirmation. Both still release the single queue.
- Abandoned preview completion after Esc/creation/new advanced menu cannot reopen a modal.
- Existing successful current-flow build/load and preview-to-confirm tests remain in the covering verification.

RED command (same cache/wrapper as above): `rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/tui -run 'TestAdvancedLookupRespectsParentFlow|TestPruneCompletionRespectsParentFlow|TestAbandonedPruneCompletionCannotOpenModal' -count=1`, exit 1:

```text
--- FAIL: TestAdvancedLookupRespectsParentFlow (0.00s)
    --- FAIL: TestAdvancedLookupRespectsParentFlow/stop_confirmation (0.00s)
        flow_test.go:45: delayed lookup replaced/reopened a newer or abandoned flow
    --- FAIL: TestAdvancedLookupRespectsParentFlow/esc (0.00s)
        flow_test.go:45: delayed lookup replaced/reopened a newer or abandoned flow
    --- FAIL: TestAdvancedLookupRespectsParentFlow/cancel (0.00s)
        flow_test.go:45: delayed lookup replaced/reopened a newer or abandoned flow
    --- FAIL: TestAdvancedLookupRespectsParentFlow/creation (0.00s)
        flow_test.go:45: delayed lookup replaced/reopened a newer or abandoned flow
    --- FAIL: TestAdvancedLookupRespectsParentFlow/advanced_menu (0.00s)
        flow_test.go:45: delayed lookup replaced/reopened a newer or abandoned flow
    --- FAIL: TestAdvancedLookupRespectsParentFlow/newer_lookup (0.00s)
        flow_test.go:45: delayed lookup replaced/reopened a newer or abandoned flow
    --- FAIL: TestAdvancedLookupRespectsParentFlow/action_menu (0.00s)
        flow_test.go:45: delayed lookup replaced/reopened a newer or abandoned flow
--- FAIL: TestPruneCompletionRespectsParentFlow (0.00s)
    --- FAIL: TestPruneCompletionRespectsParentFlow/delayed (0.00s)
        flow_test.go:96: delayed prune replaced newer frozen confirmation
    --- FAIL: TestPruneCompletionRespectsParentFlow/queued (0.00s)
        flow_test.go:96: delayed prune replaced newer frozen confirmation
--- FAIL: TestAbandonedPruneCompletionCannotOpenModal (0.00s)
    --- FAIL: TestAbandonedPruneCompletionCannotOpenModal/esc (0.00s)
        flow_test.go:126: abandoned prune reopened confirmation
    --- FAIL: TestAbandonedPruneCompletionCannotOpenModal/creation (0.00s)
        flow_test.go:126: abandoned prune reopened confirmation
    --- FAIL: TestAbandonedPruneCompletionCannotOpenModal/advanced_menu (0.00s)
        flow_test.go:126: abandoned prune reopened confirmation
FAIL paseo-sbx/internal/tui 0.010s
```

The first test attempt used a helper that did not process nested command batches and timed out for the queued fixture. Replaced it with the existing recursive async command helper before the meaningful RED above; no production changes preceded the reproduced assertion failures.

GREEN command: `rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/tui -run 'TestAdvancedLookupRespectsParentFlow|TestPruneCompletionRespectsParentFlow|TestAbandonedPruneCompletionCannotOpenModal|TestAdvancedSavedSelectionActions|TestDashboardPruneConfirmationFrozen' -count=1`, exit 0:

```text
ok paseo-sbx/internal/tui 0.011s
```

Covering race command: `rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./internal/tui -count=1`, exit 0:

```text
ok paseo-sbx/internal/tui 5.655s
```

Final full race command: `rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./...`, authorized localhost-listener escalation, exit 0:

```text
? paseo-sbx/cmd/agent-wrapper [no test files]
? paseo-sbx/cmd/install-runtime [no test files]
? paseo-sbx/cmd/paseo-sbx [no test files]
ok paseo-sbx/internal/cli 3.772s
ok paseo-sbx/internal/config (cached)
ok paseo-sbx/internal/gateway (cached)
ok paseo-sbx/internal/launcher (cached)
ok paseo-sbx/internal/operationlock (cached)
ok paseo-sbx/internal/operations (cached)
ok paseo-sbx/internal/process (cached)
ok paseo-sbx/internal/runtimeinstall (cached)
ok paseo-sbx/internal/sandboxupdate (cached)
ok paseo-sbx/internal/tui 5.661s
```

`rtk proxy git diff --check`: exit 0, no output. Self-reviewed all production changes. Files: `internal/tui/create.go`, `dashboard.go`, `jobs.go`, new `flow_test.go`. No remaining I1 concern or API/flow ambiguity; no live mutations, push or merge. Fix commit subject: `fix: preserve parent ownership of asynchronous confirmations`.
