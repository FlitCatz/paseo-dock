# Local stacked PR preparation — 2026-10-12

Three local review branches have been prepared from the preserved original
`feat/management-tui` history. The user subsequently selected local merge.
Main advanced in order through `d1cbd99`, `9fe399f`, and `bede316` using
fast-forward merges. No branch has been pushed and no PR published.

| PR | Base | Head | Local commit | Source snapshot |
| --- | --- | --- | --- | --- |
| [1. Management TUI](pr-1-management-tui.md) | `main` | `review/management-tui` | `d1cbd99` | `c7fe51a` plus relevant maintenance |
| [2. Runtime categories/native](pr-2-runtime-categories.md) | `review/management-tui` | `review/runtime-categories` | `9fe399f` | `99a2b4e` plus relevant maintenance |
| [3. Codex userns](pr-3-codex-userns.md) | `review/runtime-categories` | `review/codex-userns` | `bede316` | `7efadae` plus the self-contained native test fixture |

The second and third review units use snapshot commits on their immediate review
base, making dependency and diff boundaries explicit without rewriting the
original feature branch. The TUI unit excludes the two prematurely committed
category design/plan documents; the category unit restores them. Native transport
stays with categories because they share persisted identity and backend contracts.

Applicable maintenance is carried at the earliest review unit that contains its
dependencies: CI Make targets and launcher fixture isolation in TUI; offline
category helper targets/embedded frames in categories; userns helper targets and
ownership/cancellation corrections in userns. Raw docs logs are absent from all
review heads, with summaries/hash inventories and ignored local archives retained.

## Verification

| Review head | `make test` | `make vet` | `make build` |
| --- | --- | --- | --- |
| TUI | Pass; full race suite, TUI 9.956s | Pass | Pass |
| Categories/native | Pass; CLI 9.603s, category helper 21.593s | Pass, including category helper | Pass |
| Userns | Pass; operations 143.358s, userns helper 1.026s, category helper 21.573s | Pass, including both helpers | Pass |

Tests requiring fake local relay sockets ran with local socket access. Unchanged
package results may use Go's cache; each review tree ran its own Make commands.
Initial category test execution exposed the existing native routing test's
dependency on a prebuilt `bin/agent-wrapper`. The isolated dry-run fixture now
provides its own trusted wrapper bytes and lock. A direct race run with the
review workspace's `bin` temporarily absent passed (1.032s). No real guest/model
operation was used for this fix. The final userns tree carries the same correction.

Independent split review checks source equivalence and stage dependencies, not
the entire historical branch's security/correctness. Its final verdict found no
Important/Critical split issues; lineage is `d1cbd99` → `9fe399f` → `bede316`.
Source/fixture boundaries and Make/CI dependencies were independently checked;
test outcomes above were run and recorded by the controller. The userns archive inventory matches
all 39 retained byte counts/SHA256. Publishing requires a separate integration
choice, which the user has now made for local integration; original source
history and review workspaces remain preserved. A diff
between the final review branch and original working tree is empty across
`cmd`, `internal` (including tests), `scripts`, Dockerfile, Go module/lock inputs,
Makefile, README, CI and ignore configuration. No product/test/config change was
lost in the split. The original branch receives the same isolated native fixture
correction and these preparation records, without rebasing or dropping its history.

## Local main integration

Main started at `e0f4263` and advanced through all three reviewed heads, without
conflicts. Product sources match the final userns review tree. This record and
the PR preparation drafts are copied from original source commit `c9258cc` into
main as documentation; the original branch is not merged a second time.

Before integration, the two uncommitted main plan files were verified equal to
source commit `3143747` and preserved in a specifically named Git stash. The
integrated versions include later committed review/status updates, so the old
drafts are not reapplied over them. The stash is retained as a backup.
Backup stash commit: `4608818b8d7a555269fcf941a3dc5ac14e92061e`, message
`preserve main plan documents before local review-stack merge`.

Paseo-managed review workspaces and the original development workspace remain
available for pending acceptance/evidence. They have not been forcibly removed.
Final main checks on integrated head `bede316` all passed:

| Check | Result |
| --- | --- |
| `make test` with fake local socket access | Exit 0; full race suite, operations 141.930s, userns helper 1.026s, category helper 21.600s |
| `make vet` | Exit 0, including both helpers |
| `make build` | Exit 0, all three product binaries |
| Product/test/config diff against `review/codex-userns` | Empty |
| Archived evidence verification | All 39 files match inventory byte counts/SHA256; no raw docs logs remain |
| `git diff --check` | Exit 0 |

The subsequent main commit contains only integration/preparation documentation;
it does not change the tested product tree. Local integration is complete.
Authenticated/model-driven acceptance and broader platform gates below remain
pending. No remote push or live acceptance action was performed.

## Subsequent dashboard section fix

After local integration, the user requested visible section boundaries. The
dashboard now shows four independently titled boxes at 80x20 and above, with a
double frame for the focused section and monochrome-safe boundaries. Narrow
screens retain one section with its position; tiny screens prioritize the
selected row. Action/confirmation overlays also account for frame dimensions.
README keyboard/layout guidance is updated. Backend/resource behavior is unchanged.

Frame/menu regressions were observed failing before implementation. Existing
Korean, tiny-terminal, long-selection and log bounds passed after the change.
The first full run exposed a small-screen warning assertion that assumed a
Korean word could not wrap; its visible-text assertion now spans frame/wrapped
rows. An unchanged CLI userns PTY heading-marker test also failed once; its
focused rerun and the final full suite passed without changing that wizard/test.
Final `make test` exits0 (CLI15.773s, TUI13.718s, category helper21.585s),
`make vet`, `make build` and diff check pass. Independent scoped review found no
Critical/Important layout findings. An actual rebuilt dashboard was opened in a
PTY, its four frames observed, and closed with `q`. That read-only render check
does not claim daemon/Docker connectivity or authenticated guest acceptance.

## Remaining acceptance

Authenticated model inference, actual model-driven MCP/native tool use, amd64
live confinement and AppArmor-enabled Linux host acceptance remain unverified.
The userns live helper is macOS/OrbStack-specific; this local preparation does
not add platform evidence or start an auth flow. Full product acceptance is
incomplete, even when all three offline review suites pass. See the original
[closure record](../2026-10-06-runtime-categories/branch-closure.md).

## Workspaces

These are Paseo-managed workspaces, retained for review:

- TUI: `wks_fcf09f4e8b763e29`, `/Users/dev-r/.paseo/worktrees/1tos7bf5/paseo-sbx-review-tui`
- Categories: `wks_c0902e84bb0e807b`, `/Users/dev-r/.paseo/worktrees/1tos7bf5/paseo-sbx-review-categories`
- Userns: `wks_6c1d11d9f3729bed`, `/Users/dev-r/.paseo/worktrees/1tos7bf5/paseo-sbx-review-userns`
