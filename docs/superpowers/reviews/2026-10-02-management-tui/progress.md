# SDD ledger — plan: docs/superpowers/plans/2026-10-02-management-tui.md

Execution authorized: isolated worktree, subagent implementation and per-task review.
Worktree: /Users/dev-r/devsbx/paseo-sbx/.worktrees/management-tui
Branch: feat/management-tui
Branch start: e0f4263 (parent of implementation/planning commits).
Spec: docs/2026-10-02-management-tui-plan.md, including reviewed contracts.
Baseline: env GOCACHE=/private/tmp/paseo-sbx-management-go-cache make test → exit 0, all packages passed with race detector. Elevated execution permits httptest local listeners; no product failure remains.

## Checklist
- [x] Consent, ignored worktree, baseline
- [x] Plan/spec read, pre-flight table and rulings
- [x] Task 1 implementation → review
- [x] Task 2 implementation → review
- [x] Task 3 implementation → review
- [x] Task 4 implementation → review
- [x] Task 5 implementation → review
- [x] Task 6 implementation → review
- [ ] Final whole-branch review
- [ ] Final verification and branch finishing

## Pre-flight shared interfaces
| Tasks | Producer / consumer or shared files | Finding |
|---|---|---|
| 1 / 2 | Service.Execute, Request, Target, Result → snapshots/versions | versions Kind reserved in 1, implemented in 2; no import cycle |
| 1 / 3 | Service and CLI entry → Backend | keep CLI compatibility wrappers; operations cannot import tui |
| 1 / 4 | Execute/ShellCommand and immutable requests → jobs | stop/update lock same sandbox key; shell has no hidden update |
| 1 / 5 | workspace/provider logic → restore and frozen prune | RestoreSelection reserved; PruneTargets introduced in 5 |
| 1 / 6 | CLI/process boundary → PTY/lifecycle | process group applies to noninteractive jobs, terminal shell uses ExecProcess |
| 2 / 3 | Snapshot/Query → dashboard polling | independent connections retained; generic error does not mean empty list |
| 2 / 4 | Snapshot/Target → menu/jobs | Targets must deep-copy slices and selection before enqueue |
| 2 / 5 | Selection/ImageMismatch → creation/advanced menu | initial lock versions are configuration provenance, not guest truth |
| 2 / 6 | unknown states → PTY/error display | narrow terminals must retain error visibility |
| 3 / 4 | dashboard.go/panels.go/Backend → jobs | sequential implementers prevent conflicting file edits |
| 3 / 5 | Dashboard/Backend → embedded creation | wizard child emits distinct completion message; parent never quits |
| 3 / 6 | viewport/layout → terminal bounds and restoration | max width/height includes footer; NO_COLOR styling absent |
| 4 / 5 | frozen queue → create/build/prune | same queue and confirmations; no second scheduler |
| 4 / 6 | shell/events/logs → PTY and flood tests | terminal controls filtered; UTF-8 and byte bound coexist |
| 5 / 6 | complete user flow → live lifecycle | disposable target and isolated Paseo home only |

## Pre-flight per-task consistency
| Task | Test vs implementation / file consistency | Finding |
|---|---|---|
| 1 | Existing CLI tests, cancellation/locking tests; files specified | SelectionOptions fields underspecified; inherit existing flag resolver options. Result cancellation needs wrapping of updater errors. |
| 2 | read-only polling plus explicit versions are separate | update timestamp is matching prior record only; no false current verification |
| 3 | dashboard polls core frequently and slow metadata separately | exact versions deliberately resolved and pinned during component integration |
| 4 | mutable job state confined to tea.Update, log writer synchronized | needs deep frozen requests, not shallow copy |
| 5 | PruneTargets is an explicit extension; embedded completion distinct | dry-run Target must carry runtime identity and registry baseline |
| 6 | PTY assertions plus characterization allowed | live environment constraints remain reported rather than converted to a pass |

Ruling: SelectionOptions will mirror current resolveSelection inputs with named fields; updater callback is func(string) mapped to operations.Event — preserves current flag validation and keeps operations out of updater imports — cost if wrong: API field/adapter rename.
Ruling: Services execute per-request copies of streams/selection and never mutate the shared Service; shell is interactive terminal-owned, cancellation process groups apply to background commands — avoids races and terminal foreground conflicts — cost if wrong: runner refactor.
Ruling: Live sandbox lifecycle will only run if read-only preflight proves a usable environment; failure is an explicitly unverified criterion, not a code/test waiver — prevents using the user's existing resources as substitutes — cost if wrong: additional environment setup and lifecycle run.

Task 1: in progress (BASE 3143747, implementer /root/implement_task_1).

Environment preflight: elevated read-only sbx ls --json succeeded (wrapped sandboxes array, name/status/workspaces fields); Docker info reported server 29.4.0. Live environment may be usable; later disposable verification must recheck. Restricted failures were permission-context dependent.

Task 1: Ruling: add sandboxupdate.UpdateLockedContext for an already-held matching sandbox lock, preserving original lock-file identity and automatic skip behavior; revalidate registry target under the held lock — avoids recursive acquisition and check/use races — cost if wrong: narrow updater API revision.

Task 1: review failed (2 Important); HEAD 4dff4c4.
Task 1: minor (deferred): process-group test must require child readiness and PID before cancellation (process_test.go:35,44); final reviewer must triage.
Task 1: Ruling: prune must acquire all affected sandbox locks and preserve legacy updates/*.lock inodes when forgetting registry metadata — compatibility with updater lock identity outweighs deleting empty runtime directories — cost if wrong: metadata cleanup refinement.

Task 1: fix round 1/5 (2 addressed, 0 open; commits 4dff4c4..4e0e0d3).
Task 1: complete (commits 3143747..4e0e0d3, review clean).
Task 2: in progress (BASE 4e0e0d3; implementer /root/implement_task_2).

Task 2: Ruling: expose typed TemplateRows and Result.VersionCheckedAt/VersionErrors/Warnings, with separate version-check records and Sandbox.VersionCheck matched by sandbox/image — Connection alone cannot display template contents; manual probes must not reset updater policy — cost if wrong: small result/schema refinement.
Task 2: Ruling: private strict registry discovery surfaces malformed rows with stable runtime identity; existing mutation Registries API keeps its skip behavior — avoids changing legacy provider mutation semantics — cost if wrong: discovery helper revision.

Task 2: implementation verified, controller review pending. Named snapshot/provenance/provider/versions RED cases and malformed provider/Docker/utility RED regressions reached GREEN; operations/config race, full make test (elevated httptest), make vet and make build passed. Explicit checks preserve partial actual confirmations, separate ledger, updater policy and exec-start warning; GSD current methodology version deliberately remains unconfirmed without a verified installed version probe.
Task 2: implementation commit 593ff83; report task-2-report.md; controller review pending.

Task 2: review failed (Important I1: utility IDs collide with reserved agent/methodology confirmation keys); HEAD 593ff83.
Task 2: Ruling: explicit utility observations use utility:<id> keys, reserving agent and methodology for their own confirmations — supports valid custom utility IDs without false version provenance — cost if wrong: utility display/key adapter adjustment.

Task 2: fix round 1/5 (1 addressed, 0 open; commits 593ff83..714968f).
Task 2: complete (commits 4e0e0d3..714968f, review clean).
Task 3: in progress (BASE 714968f; implementer /root/implement_task_3).

Task 3: review failed (3 Important: duplicate poll chains after manual refresh; filtered visible row mismatches stored target; details keys can still scroll logs); HEAD a9e4b66.
Task 3: cannot-verify resolution: standalone wizard source unchanged and original wizard tests ran in full green suite; live PTY/terminal cancellation reserved for Task 6.

Task 3: additional spec gap: dashboard.refresh includes templates whenever templateTab is true, but spec requires tab entry or manual refresh only.
Task 3: Ruling: template observation occurs on tab entry or explicit refresh, not every core poll while the tab remains displayed — resolves brief wording against binding spec cadence — cost if wrong: small query scheduling change.

Task 3: fix round 1/5 (4 addressed, 0 open; commits a9e4b66..30e182c).
Task 3: complete (commits 714968f..30e182c, review clean).
Task 4: in progress (BASE 30e182c; implementer /root/implement_task_4).

Task 4: Ruling: registration uses existing active profiles; explicit restoration after unregister stays in Task 5, with a disabled reason until eligible — follows the producer/consumer task boundary and keeps legacy CLI validation unchanged — cost if wrong: menu eligibility refinement in Task 5.
Task 4: Ruling: request-local Execute copies clear stdin when an event observer is supplied; nil-observer CLI calls preserve existing stdin — background TUI jobs must not compete for terminal input — cost if wrong: explicit execution input option instead of observer-based mode.

Task 4: implementer interrupted by workspace credit error after focused queue/cancel/log/shell/stdin tests reported GREEN; user requested continuation and same implementer resumed. No task restart; final verification/report/commit still pending.

Task 4: implementation commit 3c568a7; full race suite, go vet and diff checks passed. Controller task review /root/review_task_4 pending; report task-4-report.md. Actual PTY and live lifecycle remain Task 6.

Task 4: review failed (Important I1: queued register/unregister ignores frozen active Targets and applies changed registry scope); HEAD 3c568a7. Fix round 1 dispatched to original implementer.
Task 4: minor (deferred): stdout/stderr log events replace semantic progress stages (jobs.go:497); final reviewer must triage, carry into downstream jobs integration.
Task 4: cannot-verify resolution: actual PTY restoration and disposable live lifecycle remain binding Task 6 checks, no success claimed yet.

Task 4: Ruling: nil provider Targets keeps legacy CLI resolution; non-nil Targets, including an empty slice, is an explicit frozen active scope validated under existing locks — distinguishes confirmed inactive scope from omitted CLI scope — cost if wrong: explicit scope flag replacing nil/empty distinction.

Task 4: fix round 1/5 (1 addressed, 0 open; commits 3c568a7..3fce6e7).
Task 4: complete (commits 30e182c..3fce6e7, review clean).
Task 5: in progress (BASE 3fce6e7; implementer /root/implement_task_5).

Task 5: Ruling: add RestoreTargets as frozen desired saved-selection entry identities, while Targets retains the confirmed prior active scope — restoration must validate inactive desired entries without losing prior-scope revalidation — cost if wrong: restore request identity schema refinement.

Task 5: implementation commit 79e961c, full race suite passed; task review /root/review_task_5 pending. Task 4 semantic-stage deferred minor claimed resolved with regression, reviewer to verify. Report task-5-report.md. PTY/live claims remain unverified pending Task 6.

Task 5: review failed (Important I1: delayed saved-selection lookup or prune preview completion overwrites a newer confirmation without parent-flow identity checks); HEAD 79e961c. Fix round 1 dispatched to original implementer.
Task 4: deferred semantic-stage minor resolved by Task 5 regression and independent review.

Task 5: cannot-verify resolution: snapshot.go:237 computes ImageMismatch and existing panels.go:122-123 displays reuse refusal; CLI/launcher regressions passed in full race suite and remain final whole-branch review checks; PTY/NO_COLOR/bounds and disposable lifecycle are explicit Task 6 checks, no live success claimed.

Task 5: minor (deferred, process evidence): initial embedded-wizard API RED was compilation-only; report explicitly discloses that completion assertions passed after implementation, while creation freezing/stale completion had behavioral RED→GREEN. Final reviewer must triage; do not fabricate historical TDD evidence.

Task 5: fix round 1/5 (1 addressed, 0 open; commits 79e961c..e485597).
Task 5: complete (commits 3fce6e7..e485597, review clean).
Task 6: in progress (BASE e485597; implementer /root/implement_task_6).

Task 6: Ruling: lifecycle application child environment isolates trusted runtime and Paseo home in temporary paths; Go proxies restore captured authentication environment only for authentic external CLIs, with disposable-target and explicit Paseo-home guards — enables live checks without altering caller environment/shared launcher/default provider configuration — cost if wrong: live verification must stop and isolation helper must be revised.

Task 6: implementation commit 3155087; review /root/review_task_6 clean on spec and quality, with one deferred minor report wording correction.
Task 6: minor (deferred): task-6-report.md overstates isolation/path checks as before every mutation; live-verify.go preflights once and proxies enforce per-call target/home guards. Final reviewer must triage.
Task 6: live criterion partially unverified: disposable create/stop/shell/register/unregister succeeded; explicit update failed on upstream Antigravity manifest HTTP 403, while isolated managed Codex/Claude provider install lacked an exact matching loaded template. PTY, bounds, NO_COLOR, required make test/vet/build passed. Live disposable sandbox and isolated daemon remain reported.
Task 6: complete (commits e485597..3155087, review clean; upstream live-update criterion unverified).
