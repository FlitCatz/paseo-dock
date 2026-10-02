# Management TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Go 관리 TUI에서 workspace와 sandbox 상태를 보고 생성·업데이트·shell·중지·등록·빌드·정리를 수행한다.

**Architecture:** 실행 서비스는 operations에, 입력·화면 상태는 tui에 둔다. CLI는 기존 인수·출력 계약을 유지하며 같은 서비스를 호출한다. 비동기 작업은 immutable request와 이벤트로 전달하고 TUI model은 Update에서만 변경한다.

**Tech Stack:** Go 1.26+, Bubble Tea v2.0.10, Bubbles v2, Lip Gloss v2, 기존 config·sandboxupdate. Bubbles/Lip Gloss는 Task 3에서 v2.0.10과 컴파일·모델 테스트로 호환성을 확인한 정확한 버전을 go.mod/go.sum에 고정한다.

**Spec:** `docs/2026-10-02-management-tui-plan.md` (재검토 기록 포함)

**실행 상태 (2026-10-02):** `.worktrees/management-tui`, `feat/management-tui`에서
subagent-driven-development로 Tasks 1–5 구현 및 독립 review를 마쳤다.
Task 6의 터미널·문서 검증은 아래에 기록한다. 아래 원래 RED 체크리스트는 계획 기록이며
Task 5 최초 embedded API 컴파일-only RED를 행동 RED로 표시하지 않는다.

## Global Constraints

- 새 기능, CLI, helper 및 테스트는 Go로 작성한다.
- 기존 CLI 인수·종료 코드·선택 검증·provider backup/merge와 launcher 자동 갱신 정책을 유지한다.
- 자동 조회는 `sbx exec`나 패키지 네트워크 조회를 호출하지 않는다.
- 관리 TUI는 위 순서로 구현하되 1차 사용자 흐름은 4단계까지 완성한다.
- sandbox 삭제·재생성, 새 모델 작업 실행, 모델 목록·인증 상태 조회는 후속 범위다.
- 취소를 위해 sandbox 전체를 중지하거나 완료된 변경을 자동 rollback하지 않는다.
- 삭제된 workspace의 registry를 정리해도 sandbox는 보존한다.
- 작업마다 실제 대상 ID·선택·옵션을 고정하고 실행 직전 검증한다.
- 모든 subprocess는 인수 배열로 실행한다. guest 파일을 호스트에서 실행하지 않는다.

## Review Focus

- 새로고침·검색·workspace 이동 중 대기 작업의 대상이 바뀌지 않아야 한다: Task 4 테스트.
- 잘못된 JSON·조회 timeout은 sandbox가 삭제된 상태로 해석하지 않아야 한다: Task 2/3 테스트.
- 취소된 sbx exec 뒤 guest installer가 살아 있을 수 있다: Task 1/4에서 확인 필요 상태 검증.
- 여러 workspace의 동일 provider와 unregister 이후 재등록: Task 1/5 테스트.
- 과도한 로그·ANSI 제어문자·한글·작은 PTY가 화면과 메모리를 무너뜨리지 않아야 한다: Task 4/6 테스트.

## Files and boundaries

- `internal/operations/service.go`, `requests.go`, `selection.go`, `images.go`, `workspaces.go`, `providers.go`: 공용 실행과 typed 결과.
- `internal/operations/snapshot.go`, `versions.go`: 부작용 없는 상태 조회와 명시적 guest 버전 확인.
- `internal/process/process.go`: context 및 process group 취소를 지원하는 비대화형 subprocess.
- `internal/operationlock/lock.go`: 프로세스 간 잠금과 다중 잠금 순서.
- `internal/tui/dashboard.go`, `panels.go`, `jobs.go`, `create.go`: dashboard 상태·뷰·작업·wizard 통합.
- `internal/cli/cli.go`, `selection.go`, `paseo.go`: flag parsing, standalone wizard, 기존 run plugin 진입.
- `internal/sandboxupdate/update.go`: 기존 Update API를 유지하며 context·이벤트 경로 추가.
- `internal/launcher/launcher.go`: 자동 updater의 공용 잠금 사용 확인.

각 task는 테스트를 먼저 작성하고 RED를 확인한 뒤 구현한다. 외부 sbx·Docker·Paseo만
fake subprocess 또는 backend로 대체하며 registry와 선택·잠금·상태 전이는 실제 코드를
사용한다. RED는 누락된 동작 때문이어야 한다. 계획에서 새 API를 사용하는 테스트는
컴파일 오류만 확인하고 끝내지 않고 최소 API 선언 후 실패 assertion도 확인한다.

### Task 1: 공용 실행 서비스와 취소·잠금

**Files:** 위 operations 실행 파일, process·operationlock, cli 세 파일,
sandboxupdate/update.go, launcher/launcher.go; 각 패키지의 `_test.go`.

**Interfaces:**
- Produces: `operations.New(root string) (*Service, error)`.
- Produces: `(*Service).Execute(ctx context.Context, request Request, emit func(Event)) (Result, error)`.
- Produces: `(*Service).ShellCommand(ctx context.Context, target Target) (*exec.Cmd, error)`.
- Produces: `(*Service).ResolveSelection(options SelectionOptions) (config.Selection, error)`.
- Produces: `(*Service).SavedSelection(workspace string) (config.Selection, error)`.
- `Service` has Root, Lock, Catalog, In, Out, Err (the current App fields except mutable Selection).
- `Target` has Workspace, Runtime, Profile strings and Entry config.Entry. Target includes the
  confirmed registry identity; execution rejects a changed Entry rather than silently resolving a replacement.
- `Request` has Kind, Target, Targets []Target, Selection, Namespace, Platforms, Memory, PaseoHome and bool
  Push, KeepArchive, BuildFirst, Apply, DryRun, RestoreSelection. Kind values: build, load,
  create, update, stop, register, unregister, prune, versions.
- Kind is a named string type; Selection is config.Selection; other options above are strings.
  Targets freezes workspace-wide updates; CLI callers may omit it and resolve targets at execution.
- `Event` has Stage, Text strings and Time time.Time. `Result` has Changed bool,
  NeedsVerification bool, Versions map[string]string, Remaining []Target, Missing []Target.
- `sandboxupdate.UpdateContext(ctx context.Context, runtime string, entry config.Entry,
  force bool, log io.Writer, emit func(string)) error` adds an external context
  and stage callback; existing `Update(runtime, entry, force, log)` remains a wrapper with existing policy.
- `operationlock.Acquire(ctx context.Context, base string, keys []string) (release func(), err error)`
  sorts keys, unwinds partial acquisition and returns a conflict error rather than waiting indefinitely.

- [ ] **Step 1: RED tests**
  - `TestExecuteRejectsChangedTarget`: confirm sandbox `one`, replace registry with `two`,
    stop/update must error and must execute neither target.
  - `TestUpdateContextCancellation`: cancel during an installer; returns an error, records
    partial state, reports NeedsVerification, never runs `sbx stop`.
  - `TestProcessCancellationKillsGroup`: cancel a Go helper with a child holding stdout;
    command returns without waiting for the child forever.
  - `TestSharedUpdateLock`: competing explicit update/automatic update/stop cannot mutate the
    same sandbox concurrently; unrelated sandbox proceeds; release allows retry.
  - Preserve existing CLI lifecycle, selection, source-boundary, provider union and updater tests.

- [ ] **Step 2: Run RED** — `go test ./internal/operations ./internal/process ./internal/operationlock ./internal/sandboxupdate`.
  Expected: new behavior assertions fail before implementation.
- [ ] **Step 3: Implement** — move execution bodies out of CLI, route parsed requests through
  Execute. Keep BakeConfig/selection compatibility with existing tests through exports and small
  forwarding wrappers where needed; wrappers contain no duplicated business logic. Use per-request
  selection rather than mutating shared Service. Stop and updater use the same sandbox lock;
  create/unregister use workspace locks; provider merge uses global runtime and home locks;
  build/load lock shared root outputs. Avoid nested re-acquisition by splitting locked boundaries
  from helpers. Emit stages and preserve partial registry writes on create failures.
- [ ] **Step 4: GREEN** — `go test -race ./...`. Expected: all packages pass, including existing CLI and launcher behavior.
- [ ] **Step 5: Commit** — `refactor: share sandbox operations with context and locks`.

### Task 2: Typed snapshots and version provenance

**Files:** operations/snapshot.go, versions.go and tests; config/config.go and tests.

**Interfaces:**
- Consumes: Task 1 Service, Target, Request, Result.
- Produces: `(*Service).Snapshot(ctx context.Context, query Query) (Snapshot, error)`.
- `Query` has IncludePaseo, IncludeDocker, IncludeTemplates bool and PaseoHome string.
- `Snapshot` has Workspaces []Workspace, Unmanaged []Sandbox, SBX, Paseo, Docker,
  Templates Connection. `Connection` has CheckedAt time.Time, Error string, Status string.
- `Workspace` has ID, Path, Runtime strings, Missing bool, Selection *config.Selection,
  Sandboxes []Sandbox, Error string. `Sandbox` has Target, RuntimeState string,
  Active bool, Registration string, ImageMismatch bool, Update *sandboxupdate.State,
  InitialVersions map[string]string. RuntimeState: running, stopped, missing, unknown.

- [ ] **Step 1: RED tests**
  - `TestSnapshotReadOnly`: polling only reads local JSON and status commands; no `exec`,
    install, build, reload, stop, create, or config writes occur.
  - `TestSnapshotJoinAndErrors`: join registry by exact sandbox name; unrelated rows are
    Unmanaged; valid empty array means missing; malformed JSON and unrecognized wrappers
    produce a connection error and unknown state, never missing.
  - `TestVersionProvenance`: wrong-image or wrong-sandbox update state is excluded;
    current matching state exposes its Attempt and per-component successes/errors.
  - `TestProviderRegistrationIsNotActivation`: active registry with absent/disabled provider
    is not shown as registered; unreadable config is unknown.
  - `TestVersionsDoesNotInstall`: explicit versions request probes binaries/manifests only;
    no package network calls/installers/activation changes; it warns that exec may start the guest.

- [ ] **Step 2: Run RED** — `go test ./internal/operations ./internal/config`.
  Expected: assertions expose missing snapshot/provenance behavior.
- [ ] **Step 3: Implement** — distinguish independent connection failures and malformed
  registries; use bounded subprocess capture and query timeout; support sbx array and wrapper.
  Faster registry/sbx queries do not implicitly request Paseo/Docker/templates. Versions checks
  use supported adapter binaries, guest installation manifest and plugin commit where applicable;
  they cannot manufacture confirmed methodology versions from image lock values.
- [ ] **Step 4: GREEN** — `go test -race ./internal/operations ./internal/config`.
  Expected: all tests pass.
- [ ] **Step 5: Commit** — `feat: expose read-only management snapshots`.

### Task 3: Dashboard, polling and terminal entry

**Files:** tui/dashboard.go, panels.go, dashboard_test.go; cli/cli.go, cli_test.go; go.mod/go.sum.

**Interfaces:**
- Consumes: Task 1/2 Service and Snapshot.
- Produces: `tui.Backend` interface exposing Snapshot, Execute, ShellCommand, SavedSelection.
- Produces: `tui.NewDashboard(backend Backend, catalog config.Catalog, workspace string) *Dashboard`.
- Produces: `tui.RunDashboard(backend Backend, catalog config.Catalog, workspace string, in io.Reader, out io.Writer) error`.
- Dashboard owns request IDs, panel focus, search, selected workspace/profile ID and last
  successful data plus connection errors. It does not own business execution bodies.

- [ ] **Step 1: RED tests**
  - `TestDashboardStaleResponseAndSelection`: delayed old response cannot replace newer
    selection/data; reorder preserves workspace/profile identity; disappearing selection is safe.
  - `TestDashboardRefreshCoalesces`: repeated ticks while query is pending start one query;
    completion permits the next. registry/sbx interval is 3 seconds, Paseo/Docker 15 seconds;
    templates only on its tab or explicit refresh.
  - `TestDashboardFailureKeepsLastSuccess`: failed refresh retains rows and last-success time
    but labels runtime unknown and exposes the current connection error.
  - `TestDashboardNavigationSearch`: Tab, arrows/jk, / search, escape, r, ? work without mutation;
    unmanaged rows remain read-only.
  - `TestTUIRequiresTerminal`: explicit non-TTY tui errors; non-TTY no-args preserves usage and
    exit 2; --help works without terminal or external services.

- [ ] **Step 2: Run RED** — `go test ./internal/tui ./internal/cli`.
  Expected: missing dashboard/entry assertions fail.
- [ ] **Step 3: Implement** — add terminal-only implicit entry and `tui --workspace`; use
  Bubbles list/viewport/help and Lip Gloss panels with NO_COLOR support. Wide screens show
  workspaces/sandboxes/details/logs, 80×24 uses detail/log tabs, smaller screens focus one panel.
  All render paths truncate by ANSI display width and keep focused rows visible.
- [ ] **Step 4: GREEN** — `go test -race ./internal/tui ./internal/cli`.
  Expected: dashboard and standalone wizard tests pass.
- [ ] **Step 5: Commit** — `feat: add state-driven sandbox dashboard`.

### Task 4: Management actions, queue, logs, cancellation and shell

**Files:** tui/jobs.go, jobs_test.go, dashboard.go, panels.go; operations/service.go tests if needed.

**Interfaces:** Consumes Backend and immutable Request/Target from earlier tasks. No additional
backend calls are invented; completion always schedules a fresh Snapshot.

- [ ] **Step 1: RED tests**
  - `TestJobTargetsRemainFrozen`: enqueue stop/update for one sandbox, change selection and
    refresh before dispatch, assert Execute receives original Entry; changed registry is rejected.
  - `TestOneMutationAtATime`: second mutation waits, read-only snapshots continue; errors do
    not wedge the queue. Workspace-wide update freezes active targets at confirmation.
  - `TestCancelAndExit`: cancel request is shown immediately, final unverified guest state stays
    NeedsVerification, q/ctrl+c while busy offers keep-running or cancel-and-wait; idle q exits.
  - `TestBoundedSafeLogs`: more than 256 KiB retains at most 256 KiB per job, queue/history at
    most 20 jobs; OSC/terminal controls are stripped, UTF-8 is retained; slow views do not block subprocesses.
  - `TestShellReturn`: shell is unavailable while mutation is running, callback error is shown,
    completion refreshes state and restores dashboard; dashboard does not auto-update on shell.

- [ ] **Step 2: Run RED** — `go test ./internal/tui -run 'Test(Job|OneMutation|Cancel|Bounded|Shell)'`.
  Expected: new queue/cancel/shell assertions fail.
- [ ] **Step 3: Implement** — Enter action menu with disabled reasons and fixed-target
  confirmation; u update, s shell, r refresh. Stop explicitly warns about sessions;
  update/versions warns about guest start. Use cancellable job contexts, bounded synchronized
  log storage and message-delivered stages; events do not mutate the model. ShellCommand
  validates target and returns an sbx interactive command for tea.ExecProcess. Register/unregister
  confirmations show workspace and Paseo home configuration scope.
- [ ] **Step 4: GREEN** — `go test -race ./internal/tui ./internal/operations`.
  Expected: all tests pass without races, job failures retain retry context and partial resources.
- [ ] **Step 5: Commit** — `feat: manage sandbox jobs from the dashboard`.

### Task 5: Embedded creation, advanced actions and re-registration

**Files:** tui/create.go, create_test.go, wizard.go, wizard_test.go, dashboard.go;
operations/workspaces.go, providers.go and tests.

**Interfaces:**
- Consumes: SavedSelection, Catalog, Request.Selection and RestoreSelection.
- Produces: `NewEmbeddedModel(c config.Catalog, s config.Selection, workspace, memory string) *Model`.
- Produces: `WizardFinishedMsg { Result Result; Err error }`. Standalone NewModel/Run keep tea.Quit.

- [ ] **Step 1: RED tests**
  - `TestEmbeddedWizardReturnsToDashboard`: finish/cancel child emits WizardFinishedMsg and
    never quits parent; standalone wizard retains prior completion/cancellation behavior.
  - `TestCreationRequestFrozen`: path picker and memory entry lead to wizard; final confirmation
    shows combinations, CPU 2, memory, utilities, build/load and Paseo scope; confirmed request is
    unchanged after wizard navigation; incompatible utilities fail before Docker runs.
  - `TestImageMismatchBlocksReuse`: incompatible registry image/settings cannot silently reuse
    an existing sandbox; selection-changing creation leaves previous activation on partial failure.
  - `TestRestoreRegistration`: after unregister, complete saved selection can reactivate verified
    profiles; missing selection/entry rejects restore; another workspace's shared provider survives.
  - `TestPrunePreviewIsFrozen`: dry-run lists missing runtimes, confirmation stores exact list,
    execute rechecks each path and does not remove newly-missing/unconfirmed or now-existing
    workspaces; no sandbox removal command is issued.

- [ ] **Step 2: Run RED** — `go test ./internal/tui ./internal/operations`.
  Expected: embedded wizard/restore/prune assertions fail.
- [ ] **Step 3: Implement** — n opens directory picker with path and memory text inputs; wizard
  returns to parent and queues create with BuildFirst. Advanced menu provides saved-selection
  build/load, templates read-only view and prune preview/confirmation. RestoreSelection is an
  explicit TUI request, keeping legacy CLI register validation unchanged. Prune uses Request's
  confirmed missing Target list (add `PruneTargets []Target` here to requests.go) and revalidates
  immediately before deletion; regular CLI prune still computes its own list.
- [ ] **Step 4: GREEN** — `go test -race ./...`.
  Expected: new flows and all CLI/launcher regressions pass.
- [ ] **Step 5: Commit** — `feat: integrate creation and advanced management actions`.

### Task 6: PTY verification, lifecycle evidence and documentation

**Files:** tui/pty_test.go, tui/dashboard_test.go, README.md, this plan and design status.

**Interfaces:** Consumes completed RunDashboard and existing CLI. Go helpers only; local PTY
test dependency, if needed, is fixed to an exact version and limited to test use.

- [x] **Step 1: Tests (characterization)** — `TestDashboardTerminalBounds` covers 120×40, 80×24, 40×10 and
  12×4 with Korean paths and long errors; lines stay within display width/height and selection
  remains visible. `TestDashboardPTYRestore` runs the app in a real PTY against fake external
  executables, resizes, enters/exits shell, checks dashboard redraw and exit terminal attributes;
  NO_COLOR output contains no color styling. Reuse state tests instead of fixed full-screen snapshots.
- [x] **Step 2: Run characterization** — `go test ./internal/tui -run 'TestDashboard(TerminalBounds|PTYRestore)'`.
  Expected: any discovered rendering/restore defects fail before fixes; features already correctly
  implemented are characterization tests and do not require inventing a failure.
- [x] **Step 3: Document verified behavior** — no product fix was needed; document
  no-args/tui, keyboard controls, explicit versions vs updates, guest start, queue/cancellation,
  advanced actions and partial failure recovery. Update status only to the verified level.
- [x] **Step 4: Final verification** — `make test`, `make vet`, `make build`.
  Expected: exit 0 for each. Also verify no-args with redirected stdin exits 2 and explicit help exits 0.
  Sandbox permissions/caches are environment constraints, never a reason to remove race tests.
- [ ] **Step 5: Live lifecycle** — only a disposable workspace/sandbox and isolated Paseo home;
  create → update → stop → shell → register → unregister. Record target names, exit codes and
  remaining state. Do not touch existing sandboxes or the default Paseo home as a fallback.
  If unavailable, record this criterion as unverified and report the exact blocker.
- [x] **Step 6: Candidate commit** — `docs: document and verify management TUI workflows`.

## Pre-flight self-review

- Requirements coverage: shared service Task 1; snapshots and provenance Task 2; entry/layout
  Task 3; everyday actions Task 4; create/advanced/cleanup Task 5; terminal/docs/live evidence Task 6.
- Shared interfaces: Tasks 2–5 consume Task 1 types; Task 3 produces Backend and Dashboard for
  Tasks 4–6; Task 5's WizardFinishedMsg is distinct from operation results.
- PruneTargets is declared as a planned Task 5 extension, not an implicit earlier field.
- Existing CLI register behavior stays unchanged; TUI restore is explicit and validates completed state.
- Update lock is not re-acquired by updater under Execute; implementation must use one locked boundary.
- Restricted sbx login service/Docker socket errors remain environment constraints. Elevated read-only rechecks succeeded; disposable lifecycle executed with update HTTP 403, so all-success lifecycle stays unverified.
- Execution used subagent-driven-development in `.worktrees/management-tui`; the planning-stage no-code-change statement is historical.

## Task 6 evidence and remaining criteria

- `TestDashboardTerminalBounds`: all four planned dimensions, Korean long paths/errors,
  focused workspace and sandbox markers, display width and height passed.
- `TestDashboardPTYRestore`: actual PTY, actual operations service and Go fake commands,
  resize, shell canonical attributes, dashboard redraw, NO_COLOR, multi-megabyte subprocess
  logs and exit attribute/alternate-screen restoration for q and Ctrl+C passed.
- `TestDashboardLogFloodBounds`: latest output retained within 256 KiB and viewport bounds;
  existing queue/stale-response tests were reused. Task 1 process cancellation was strengthened
  with actual child readiness/PID and shared process-group checks. Characterization tests
  needed no production fix and no fabricated RED.
- Final candidate `make test` (race), `make vet`, `make build` exited 0;
  redirected no-argument binary exited 2 and explicit `--help` exited 0.
- Live create/update/stop/shell/register/unregister exits were **0/1/0/0/0/0**. Update recorded
  Antigravity manifest HTTP 403/curl 22 and retained partial failure. Remaining disposable
  sandbox `paseo-antigravity-cli-superpowers-1a5e5790a6` is running; isolated registry active=[];
  saved profile, temp runtime/launcher/root/Paseo home and isolated daemon PID 90284 remain.
  Native plugin provider is unmanaged, so managed provider live installation is unverified.
- Isolation and detailed remaining state are in the binding spec's execution status.
  Exact commands/output and self-review are in
  `.superpowers/sdd/2026-10-02-management-tui/task-6-report.md`; raw PTY/live logs and
  the Go lifecycle evidence helper are retained alongside it.
