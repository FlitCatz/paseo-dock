# Management TUI evidence preservation

Base: `3155087`. This documentation-only follow-up preserves the task-by-task review record before the subagent-driven-development scratch workspace is removed. No product behavior or live resource was changed.

## Preserved record

- Tracked reports for Tasks 1, 4 and 6, Task 6 check/help/no-args/PTY logs and `task-6-live.json` were moved with `git mv` from `.superpowers/sdd/2026-10-02-management-tui/` into this directory.
- Ignored reports for Tasks 2, 3 and 5, all six independent task reviews, the Task 1–5 fix-round rereviews and `progress.md` were copied here. The scratch ledger remains available for controller updates; this copy is a snapshot before final whole-branch review and may be refreshed afterward.
- The two standalone Go helpers were moved unchanged to `internal/tui/testdata/management-tui-live/`. Go package discovery excludes `testdata`; each helper remains buildable by its explicit file path from the repository root. Both files declare `main`, so build them separately.
- Temporary review diff packages, briefing files, preflight template output and redundant untracked raw live output were omitted. The complete raw live trace is recoverable from the committed JSON string. Historical review citations and command strings retain the paths used at review/execution time; scratch report/log paths map to this directory and scratch helper paths map to the testdata directory.
- The implementation plan and Task 6 report now reference the preserved artifacts. Other historical report/review content was copied without rewriting.

SHA-256 before and after relocation:

| Artifact | SHA-256 |
| --- | --- |
| `task-6-live.json` | `4265bd5cb87eeccd4da55f7c86723d9b80a5e0f87bd0d28fbdb1a706fa79d058` |
| `live-verify.go` | `ac15e1c8f49a379807a162ebf74fe1a43217fbccf57eb57f5178e785da1a94c0` |
| `encode-live-log.go` | `e28c39b8c6f4e0a4a970a1baf9b83a5a76997f0ee37b8ac63b7652e91ff8ebde` |

## Task 6 reviewer correction

Provenance: the independent [Task 6 review](task-6-review.md), Minor finding, identified that the original report overstated isolation revalidation. Original wording: “Before every mutation the helper verifies BaseDir/LauncherPath/root/PaseoConfig inside a new temp tree, distinct caller default base, and that the computed sandbox name is absent.”

The corrected [Task 6 report](task-6-report.md) states that these checks are a one-time preflight before the lifecycle sequence, while each subsequent external proxy call enforces permitted command and target/workspace/home guards. The helpers themselves were not edited. This matches the preserved helper's `child` preflight at lines 188–213 and `proxy` guards at lines 133–148. The original reviewer finding is preserved verbatim; no live rerun was needed.

## Current read-only follow-up

The historical 2026-10-02 lifecycle trace ended with disposable sandbox `paseo-antigravity-cli-superpowers-1a5e5790a6` running and the isolated resources listed in the Task 6 report present. On 2026-10-06, the controller reported an elevated read-only `sbx ls --json` showing the same sandbox still present with status `stopped`. The controller's additional read-only filesystem check found `/private/tmp/paseo-sbx-live-task6-3925873850`, its isolated `.paseo` and workspace paths no longer exist. These follow-ups are controller-supplied evidence, distinct from the historical live trace; this preservation task made no live call or cleanup. Missing temporary paths do not establish the current status of the historically recorded isolated daemon.

The HTTP 403 update failure and unverified live managed-provider installation remain unchanged limitations. Final whole-branch review remains controller work.

## Verification

`git diff --check` and `git diff --cached --check` exited 0. Offline `go list ./...` (`GOPROXY=off GOSUMDB=off`, writable Go cache) exited 0 and listed the existing 13 packages, with no evidence helper package. SHA-256 values above matched after relocation. No behavior test or live lifecycle rerun was needed for this documentation-only move.
