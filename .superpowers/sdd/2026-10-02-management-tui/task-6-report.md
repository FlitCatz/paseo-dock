# Task 6 report

Status: DONE_WITH_CONCERNS. PTY, documentation and required final checks completed; disposable live lifecycle was executed with an upstream HTTP 403 update failure. All-success live lifecycle remains unverified, as does live managed Codex/Claude provider installation (only an exactly matching loaded Antigravity native-plugin image was available).

Base: e485597. Worktree: `/Users/dev-r/devsbx/paseo-sbx/.worktrees/management-tui`; branch `feat/management-tui`.

## Implemented scope

No production behavior needed changing. Added terminal characterization tests and strengthened the deferred process cancellation test. README now describes actual no-args/tui entry, keys, image versus guest version provenance, explicit version checks versus installers, stopped guest start, serial queue and bounds, cancel/exit/retry, shell return, creation, advanced actions, provider restoration and partial failure recovery. Binding spec and implementation plan reflect verified status without marking all-success lifecycle or final review complete.

- `internal/tui/dashboard_test.go`: `TestDashboardTerminalBounds` checks 120×40, 80×24, 40×10 and 12×4; long Korean paths/errors, last selected Korean row, actual workspace and sandbox selection markers; `TestDashboardLogFloodBounds` checks multi-megabyte output retains its latest tail within 256 KiB and all four viewport bounds.
- `internal/tui/pty_test.go`: actual PTY session running real RunDashboard and operations.Service, real isolated registry, separately built Go fake external commands that reject unexpected arguments; 120×40 → 80×24 resize, shell start/exit and terminal ownership, post-shell redraw, real stop subprocess flood through events/logs, idle q and Ctrl+C, raw/canonical state and alternate-screen restoration, no color styling, bounded terminal output. The test is scoped to Darwin/Linux and uses no full-screen snapshots.
- `internal/process/process_test.go`: readiness/PID are acknowledged by the actual child through an inherited pipe; the parent forwards the confirmed PID; test verifies its process group before cancellation and verifies prompt termination of the stdout-holding group. This resolves the deferred Task 1 weakness without changing product code.
- `go.mod`, `go.sum`: test-only `github.com/creack/pty v1.1.24`, exact version. Existing pinned UI dependencies retained. `go mod tidy` completed.
- README, `docs/2026-10-02-management-tui-plan.md`, `docs/superpowers/plans/2026-10-02-management-tui.md`: actual user workflows and evidence/status.
- Local artifacts in this directory: report, raw checks/PTY/live output, reproducible Go `live-verify.go` evidence helper. These are execution evidence, not a new production CLI.

## Test-first / characterization evidence

Tests were written before any proposed product fix. Product behavior passed; no production fix and no invented RED. Existing stale-response, frozen request, queue/history, semantic stage, wizard/creation and provider/prune state tests were reused.

Initial characterization:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/tui -run 'TestDashboard(TerminalBounds|LogFloodBounds)' -v
=== RUN TestDashboardTerminalBounds
(all eight size/focus subtests PASS)
--- PASS: TestDashboardTerminalBounds (0.01s)
=== RUN TestDashboardLogFloodBounds
--- PASS: TestDashboardLogFloodBounds (0.02s)
PASS
ok paseo-sbx/internal/tui 0.033s
exit 0
```

PTY harness corrections are **not product RED evidence**: the initial master reader terminated on EAGAIN before rendering; reopening a duplicated nonblocking descriptor attached Go's poller. macOS `/var` symlink paths were canonicalized in the fixture, correcting registry/home expectations. A fake Paseo argument-count typo was corrected. macOS detaches the slave after the session leader exits, causing ENOTTY for a subsequent ioctl; the helper now acknowledges RunDashboard return and waits for the parent to inspect restored attributes before it exits. The initial process-test pointer typo was a compilation-only fixture error, fixed before the meaningful characterization run. These did not trigger any production changes.

Focused final characterization:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/tui -run 'TestDashboard(TerminalBounds|PTYRestore)' -v
--- PASS: TestDashboardTerminalBounds (0.00s)
(all eight size/focus subtests PASS)
=== RUN TestDashboardPTYRestore/exit-71
PTY 120x40 → 80x24, shell canonical attributes and redraw, flood bounds, NO_COLOR, exit restored; 3545 output bytes
=== RUN TestDashboardPTYRestore/exit-03
PTY 120x40 → 80x24, shell canonical attributes and redraw, flood bounds, NO_COLOR, exit restored; 3529 output bytes
--- PASS: TestDashboardPTYRestore (0.44s)
PASS
ok paseo-sbx/internal/tui 0.455s
exit 0
```

Cancellation readiness characterization:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./internal/process -run TestProcessCancellationKillsGroup -v
=== RUN TestProcessCancellationKillsGroup
--- PASS: TestProcessCancellationKillsGroup (0.01s)
PASS
ok paseo-sbx/internal/process 1.026s
exit 0
```

Covering package race checks:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./internal/tui ./internal/process -count=1
ok paseo-sbx/internal/tui 9.455s
ok paseo-sbx/internal/process 1.031s
exit 0
```

Repeated PTY/bounds race characterization (six terminal sessions), raw output `task-6-pty.log`:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./internal/tui -run 'TestDashboard(TerminalBounds|PTYRestore|LogFloodBounds)' -count=3 -v > .superpowers/sdd/2026-10-02-management-tui/task-6-pty.log 2>&1
(all three test sets PASS; q and Ctrl+C sessions in each run)
last run: 3725 / 3850 terminal bytes
PASS
ok paseo-sbx/internal/tui 12.372s
exit 0
```

Historical Task 5 process limitation is explicitly preserved: initial embedded-wizard API RED was compilation-only; later completion assertions were green after implementation, and separate freezing/stale completion tests had behavioral RED→GREEN. This task did not create retrospective evidence or relabel it.

## Primary dependency evidence

Official pinned README and source were read before integration:

- https://github.com/creack/pty/blob/v1.1.24/README.md
- https://github.com/creack/pty/blob/v1.1.24/start.go
- local `/Users/dev-r/go/pkg/mod/github.com/creack/pty@v1.1.24/{start.go,run.go}`.

`rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go get github.com/creack/pty@v1.1.24` initially failed restricted DNS (`lookup proxy.golang.org: no such host`), then authorized escalation succeeded:

```text
go: downloading github.com/creack/pty v1.1.24
go: added github.com/creack/pty v1.1.24
exit 0
```

No Python or other implementation language/runtime was added.

## Required final candidate checks

All commands ran from the worktree above. Full `make test` used authorized escalation for existing local httptest listeners; race coverage was preserved. Go cache was writable at `/private/tmp/paseo-sbx-management-go-cache`. Unchanged packages legitimately used Go test cache; changed tui/process tests executed.

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache make test > .superpowers/sdd/2026-10-02-management-tui/task-6-make-test.log 2>&1
go test -race ./...
? paseo-sbx/cmd/agent-wrapper [no test files]
? paseo-sbx/cmd/install-runtime [no test files]
? paseo-sbx/cmd/paseo-sbx [no test files]
ok paseo-sbx/internal/cli (cached)
ok paseo-sbx/internal/config (cached)
ok paseo-sbx/internal/gateway (cached)
ok paseo-sbx/internal/launcher (cached)
ok paseo-sbx/internal/operationlock (cached)
ok paseo-sbx/internal/operations (cached)
ok paseo-sbx/internal/process 1.023s
ok paseo-sbx/internal/runtimeinstall (cached)
ok paseo-sbx/internal/sandboxupdate (cached)
ok paseo-sbx/internal/tui 9.447s
exit 0

rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache make vet > .superpowers/sdd/2026-10-02-management-tui/task-6-make-vet.log 2>&1
go vet ./...
exit 0

rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache make build > .superpowers/sdd/2026-10-02-management-tui/task-6-make-build.log 2>&1
mkdir -p bin
CGO_ENABLED=0 go build -trimpath -o bin/paseo-sbx ./cmd/paseo-sbx
CGO_ENABLED=0 go build -trimpath -o bin/agent-wrapper ./cmd/agent-wrapper
CGO_ENABLED=0 go build -trimpath -o bin/install-runtime ./cmd/install-runtime
exit 0

rtk proxy ./bin/paseo-sbx < /dev/null > .superpowers/sdd/2026-10-02-management-tui/task-6-noargs.log 2>&1
usage: paseo-sbx {build|load|create|register|run|unregister|prune|list|shell|stop|update} [options]
exit 2 (expected)

rtk proxy ./bin/paseo-sbx --help < /dev/null > .superpowers/sdd/2026-10-02-management-tui/task-6-help.log 2>&1
usage: paseo-sbx {tui|build|load|create|register|run|unregister|prune|list|shell|stop|update} [options]
Run paseo-sbx COMMAND --help for command options.
exit 0

rtk git diff --check
exit 0
```

## Live lifecycle: actual external commands, separate from fake PTY

Restricted reads failed with `docker login service unavailable: cannot determine whether a refresh right now would be safe to guard` and Docker socket permission denial. Elevated **read-only** rechecks succeeded:

```text
rtk proxy sbx ls --json
exit 0 (existing sandboxes read only)
rtk proxy sbx template ls --json
exit 0 (16 loaded template rows read only)
rtk proxy docker info --format '{{.ServerVersion}}'
29.4.0
exit 0
```

Controller authorized child-only isolation with Go authentic external proxies and target/home guards. Before every mutation the helper verifies BaseDir/LauncherPath/root/PaseoConfig inside a new temp tree, distinct caller default base, and that the computed sandbox name is absent. Template choice required an exact match to current Lock/Catalog/normalized Selection; no live rebuild or retag. The matching selection was Antigravity CLI + Superpowers + RTK. SBX authentic child restores captured original HOME/PATH for authentication without modifying caller environment; Paseo authentic child retains isolated HOME and explicit isolated --home. No credentials are written or printed. The Go helper itself is preserved as `live-verify.go`.

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go build -o /private/tmp/paseo-sbx-task6-live-verify .superpowers/sdd/2026-10-02-management-tui/live-verify.go
exit 0
rtk proxy /private/tmp/paseo-sbx-task6-live-verify
ISOLATION VERIFIED
Root=/private/tmp/paseo-sbx-live-task6-3052631893/trusted-root
Workspace=/private/tmp/paseo-sbx-live-task6-3052631893/workspace
BaseDir=/private/tmp/paseo-sbx-live-task6-3052631893/home/.local/share/paseo-sbx
LauncherPath=/private/tmp/paseo-sbx-live-task6-3052631893/home/.local/share/paseo-sbx/launcher/agent-wrapper
PaseoConfig=/private/tmp/paseo-sbx-live-task6-3052631893/home/.paseo/config.json
CallerBase=/Users/dev-r/.local/share/paseo-sbx (excluded)
Sandbox=paseo-antigravity-cli-superpowers-1518fc463e
Image=paseo-sbx/antigravity-cli-superpowers:2026.09.30-943a560ba350
PREFLIGHT ONLY; no live resources mutated
exit 0
```

Actual run, authorized escalation, complete raw command/output trace in `task-6-live.log`:

The byte-exact raw log is retained locally. Its complete UTF-8 content is also committed as the JSON string in `task-6-live.json`, preserving PTY carriage returns and terminal escapes while avoiding Git whitespace errors. `encode-live-log.go` records that reversible encoding; no output was dropped.

```text
rtk proxy /private/tmp/paseo-sbx-task6-live-verify --run > .superpowers/sdd/2026-10-02-management-tui/task-6-live.log 2>&1
ISOLATION VERIFIED
Root=/private/tmp/paseo-sbx-live-task6-3925873850/trusted-root
Workspace=/private/tmp/paseo-sbx-live-task6-3925873850/workspace
BaseDir=/private/tmp/paseo-sbx-live-task6-3925873850/home/.local/share/paseo-sbx
LauncherPath=/private/tmp/paseo-sbx-live-task6-3925873850/home/.local/share/paseo-sbx/launcher/agent-wrapper
PaseoConfig=/private/tmp/paseo-sbx-live-task6-3925873850/home/.paseo/config.json
CallerBase=/Users/dev-r/.local/share/paseo-sbx (excluded)
Sandbox=paseo-antigravity-cli-superpowers-1a5e5790a6
Image=paseo-sbx/antigravity-cli-superpowers:2026.09.30-943a560ba350
```

Go helper runs actual operations.Service.Execute and ShellCommand, records each external invocation/exact external exit plus operation outcome (0/1). Helper process returned 0 after collecting all outcomes; this is **not** an all-step success indication.

| Step | Operation outcome | External result |
| --- | --- | --- |
| create | 0 | sbx create exited 0; exact disposable name/workspace/image, CPU 2, memory 2g, --pull never, --skills off |
| update | 1 | Antigravity manifest curl exited 22 (HTTP 403); methodology and RTK stages succeeded; result Changed=true, Remaining contains the confirmed target |
| stop | 0 | `sbx stop paseo-antigravity-cli-superpowers-1a5e5790a6`, exit 0 |
| shell | 0 | actual PTY `sbx exec -it --workdir <disposable workspace> <disposable sandbox> bash`, exit 0; restarted stopped guest and printed TASK6_LIVE_SHELL_OK |
| register | 0 | isolated config backup/merge, explicit isolated --home status/start 0; PID 90284, endpoint 127.0.0.1:51047 |
| unregister | 0 | isolated config backup/merge, explicit isolated --home status/reload 0; active_profiles emptied |

Exact upstream blocker:

```text
curl: (22) The requested URL returned error: 403
external sbx ["exec" "--workdir" "/tmp" "paseo-antigravity-cli-superpowers-1a5e5790a6" "curl" "--fail" "--silent" "--show-error" "--location" "--max-time" "30" "--proto" "=https" "https://antigravity-cli-auto-updater-974169037036.us-central1.run.app/manifests/linux_arm64.json"] exit=22
STEP update exit=1 ... error=antigravity-cli-superpowers: sandbox update incomplete: map[agent:exit status 22]
```

Persisted updater JSON was read after the run and confirms attempt, zero Success, successful actual component observations, and agent failure:

```json
{"sandbox":"paseo-antigravity-cli-superpowers-1a5e5790a6","image":"paseo-sbx/antigravity-cli-superpowers:2026.09.30-943a560ba350","attempt":"2026-10-02T18:07:37.500004+09:00","success":"0001-01-01T00:00:00Z","versions":{"methodology":"8ca22dba9a94f28898bbce59f2537ff4d87c747d","rtk":"rtk 0.50.0"},"errors":{"agent":"exit status 22"}}
```

Remaining state, verified by actual sbx listing and isolated registry/config reads:

```text
REMAINING sandbox=paseo-antigravity-cli-superpowers-1a5e5790a6 state=running (preserved)
REMAINING runtime=/private/tmp/paseo-sbx-live-task6-3925873850/home/.local/share/paseo-sbx/1a5e5790a6 active=[] profiles=[antigravity-cli-superpowers]
REMAINING isolated PaseoHome=/private/tmp/paseo-sbx-live-task6-3925873850/home/.paseo
```

The saved selection, registry/profile, update attempt/error, shared launcher **inside isolated base**, config backups and isolated daemon remain. Temp preflight directory also remains but has no sandbox. No existing sandbox/default home was targeted; proxies reject prune/reset/rm and unexpected mutation names/workspaces/homes. No sandbox was automatically deleted. The isolated daemon is retained and reported; no broader cleanup permission is inferred.

Because Antigravity's native plugin provider is unmanaged, the isolated config's `agents.providers` is `{}` after registration/unregistration. Live managed Codex/Claude provider merge was not demonstrated. Existing real-code tests cover provider merge/backup/union and explicit restoration. Full-success live update and managed-provider installation remain limitations, not replaced by fake-PTY evidence.

## Self-review

Read the complete changed test/code/documentation diff, matched keys and flows to dashboard/jobs/create code, preserved standalone CLI and pinned UI dependency contracts. PTY tests distinguish terminal controls from prohibited color styling and explicitly synchronize actual terminal ownership; dimensions/tail memory are separately asserted in model/log tests. Child-only HOME/PATH construction is Go-only, not a reserved caller shell-variable reassignment. Existing semantic-stage and stale/queue coverage is retained. No broad cleanup, product feature scope, guest file host execution, sandbox deletion/rollback, push or merge was added.

Concerns: live Antigravity manifest HTTP 403 leaves agent update incomplete; managed Codex/Claude provider live installation not demonstrated; disposable running sandbox, isolated daemon/home/runtime remain intentionally reported. Final controller review remains pending.
