### Finding Verdicts

- **I1 — Frozen workspace provider scope is ignored at execution** — **ADDRESSED**. `internal/operations/service.go:372` gates full scope validation on register/unregister with nonnil Targets after the existing `operationlock.Acquire` at line 358 and before the mutation switch at line 377. `internal/operations/providers.go:99` resolves the confirmed runtime, validates registry/workspace identity, rejects missing or duplicate confirmed entries, revalidates every frozen Entry through `resolveTarget` at line 135, and checks the sorted complete active set at lines 144–148. A changed nonselected entry or activation now returns before registry/provider/launcher/backup writes. Nil Targets bypasses this new validation, preserving the legacy CLI path. `internal/tui/jobs.go:287` constructs a nonnil empty baseline; the existing `slices.Clone` copies preserve that distinction through confirmation/enqueue/dispatch and Execute.

### New Breakage in the Fix Diff

- None found. The fix retains the existing mutation locks and provider union/merge behavior; validation does not write artifacts. Comparing sorted clones treats active order as immaterial without changing the registry or request. Registry/runtime and selected workspace checks also prevent an explicit baseline from validating a different workspace than unregister will mutate.

### Out-of-Scope Observations

- None newly identified. Previously deferred M1 semantic-stage behavior is outside this fix scope and does not block this round.

### Checks and Verification Evidence

- Read the Task 4 brief, global constraints, previous review, appended fix report, and supplied `review-3c568a7..3fce6e7.diff`. The first combined response truncated part of the material; bounded reads recovered the report and fix package context. No new git diff or repository state commands were run.
- Focused source inspection confirmed the existing lock/validation/mutation sequence in `internal/operations/service.go:246`, frozen request copying in `internal/tui/jobs.go:56`, full Entry/runtime validation in `internal/operations/service.go:144`, registry validation in `internal/operations/snapshot.go:304`, activation semantics in `internal/config/catalog.go:178`, and unchanged register/unregister write boundaries in `internal/operations/providers.go:153`.
- Diff tests cover both provider kinds for changed nonselected entries and added/removed active profiles (`internal/operations/service_test.go:508`), explicit empty unregister versus later activation, unchanged/reordered scope and nil legacy scope (`internal/operations/service_test.go:571`), unrelated workspace/custom-provider preservation, and mismatched selected workspace/runtime (`internal/operations/service_test.go:650`). Rejection assertions check actual registry/config bytes, generated/launcher artifacts, backups and Changed=false. UI regressions cover queued frozen provider targets (`internal/tui/jobs_test.go:544`) and nonnil empty scope through dispatch (`internal/tui/jobs_test.go:570`). These correspond to the new production paths.
- The appended report names the covering focused race commands and supplies successful output (`task-4-report.md:230` and `task-4-report.md:232`), final package race output at lines 241–243, full race-suite output at lines 249–263, and clean vet/diff-check exit claims at line 266. Those claims are consistent with the supplied fix diff; suites were not rerun. No code doubt required a new focused test.

### Verdict

- **Fix round: All findings addressed, no new Critical/Important breakage.** I1 is closed; no open blocking findings in this scoped re-review.
