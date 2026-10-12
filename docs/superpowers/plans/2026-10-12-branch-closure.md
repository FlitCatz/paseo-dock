# Branch closure maintenance plan

**Goal:** Address the external review without adding features or claiming full acceptance.

**Scope:** Finish the interrupted helper review, make offline helper tests part of
normal verification, replace raw docs logs with summaries, document pending
acceptance and a reviewable integration proposal. Existing authorization covers
this maintenance. Keep the worktree and main's uncommitted documents intact.

- [x] Independently review `737f398..69652ee` and record disposition.
- [x] Modify `Makefile`: add `test-helpers`, invoke it from `test`, include helper
  vet commands in `vet`. Explicit files are necessary for category-verify because
  its directory contains multiple standalone main programs. Keep live native
  acceptance opt-in and outside the offline helper target.
- [x] Run the new helper target and full `make test`, `make vet`, `make build`.
- [x] Move docs raw logs to ignored `.artifacts/review-logs`, add per-directory
  summary/inventory with hashes, and replace Markdown links to removed logs.
- [x] Record three candidate stacked PR boundaries, dependencies and validation
  gates. Do not rewrite history, merge, push, or remove main's duplicate docs.
- [x] Explicitly retain deferred authenticated inference/model-driven MCP,
  amd64 live testing and AppArmor-host acceptance; update stale status statements.
- [x] Review the maintenance diff, run diff checks, and commit the scoped closure.

Execution additions within the maintenance scope: fix canceled ownership
verification with RED→GREEN coverage; isolate launcher fixture environment;
replace the log-consuming parser test with embedded protocol frames; allow the
bounded nested race subprocess exit budget; explicitly skip macOS-only daemon
fixtures elsewhere and retain cross-platform path rejection. CI uses the same
Make targets. Final independent review and checks pass; this plan is included in
the scoped closure commit.

No new runtime behavior, auth flow, live resource or host policy changes are in
scope. Summaries preserve historical results; fresh offline tests do not count
as renewed live acceptance. Log removal reduces checkout clutter, not Git history.
