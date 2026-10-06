# Sandbox and Container Categories Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add agent-only SBX sandboxes and full-stack Docker development containers to the management TUI while preserving existing sandbox records and CLI behavior.

**Architecture:** A category-bearing trusted registry and typed target feed two runtime backends behind the existing operations service. SBX owns agent-only template creation; Docker owns pinned full-stack images and long-lived managed containers. Shared queue, provider merge, and UI pass immutable target identities to the selected backend.

**Tech Stack:** Go, Bubble Tea v2, Docker Buildx/CLI, SBX CLI, existing JSON registries and operation locks. No new runtime language.

**Spec:** `docs/superpowers/specs/2026-10-06-runtime-categories-design.md`

## Global Constraints

- Work only in `.worktrees/management-tui` and preserve the uncommitted final-review fixes until they are verified and committed.
- Existing category-less records are legacy SBX; retain their profile names and provider IDs without rewriting on read.
- A new sandbox installs only the selected agent and required base runtime. A container retains the existing locked methodology and optional utilities.
- Docker defaults to bridge networking. Only explicitly requested ports bind to host `127.0.0.1`; never mount the host home or trusted runtime.
- Never mutate a Docker resource without matching registry identity, project label, image ID, bind mount, network and port inspection.
- A failed external version lookup, installer, daemon connection or relay check is a failure or unknown state, never an inferred success.
- All new executable logic and tests are Go; Shell/Dockerfile edits remain configuration.

## Review Focus

1. A category-less registry with a removed catalog utility still permits exact legacy stop/unregister; Task 1 and Task 4 test it.
2. A foreign Docker container that reuses a managed name is rejected before exec/stop; Task 3 tests it.
3. Same workspace with both categories and no explicit target reports ambiguity; Task 5 tests it.
4. A broken Docker daemon leaves the SBX rows usable and marks Docker unknown; Task 4 tests it.
5. A container relay unreachable from Docker never appears registered or receives host credentials; Task 6 tests it.

## File map

- `internal/config/category.go`, `config.go`, `catalog.go`: category and versioned registry/selection decoding, legacy compatibility, target keys.
- `internal/operations/backend.go`, `sbx_backend.go`, `docker_backend.go`: runtime contract and category-specific commands/inspection.
- `internal/operations/requests.go`, `service.go`, `snapshot.go`, `images.go`, `versions.go`, `providers.go`: immutable dispatch, status and update behavior.
- `Dockerfile`, `Dockerfile.sandbox`, `internal/runtimeinstall/*`: full-stack and minimal agent image recipes plus manifests.
- `internal/launcher/launcher.go`, `internal/gateway/*`: provider identity, runtime dispatch and credential-safe relay.
- `internal/cli/*`, `internal/tui/*`: category flags, target selection, wizard, rows and confirmation.

### Task 1: Category schema and legacy reads

**Files:** Create `internal/config/category.go`, `internal/config/category_test.go`; modify `internal/config/config.go`, `internal/config/catalog.go`, `internal/operations/requests.go`.

**Interfaces:** Produce `type Category string` with `CategorySandbox`, `CategoryContainer`, `CategoryLegacySandbox`; `func (e Entry) EffectiveCategory() Category`; `func (e Entry) Key() string`; `func (r Registry) ValidateIdentity(key string, expected Entry) error`. Add `Category`, `ContainerID`, `ImageID`, network/port fields to `Entry`, and schema 2 to newly written selections/registries. Existing `Target` carries `Category` and the complete `Entry`. Preserve category-less JSON on unrelated writes.

- [ ] Write Go tests named `TestLegacyRegistryRoundTrip`, `TestCategoryKeysDoNotCollide`, `TestExistingTargetIgnoresCurrentCatalog`, and `TestRegistryIdentityRejectsChangedCategoryAndID`. Assert old JSON reads as legacy, unchanged old records serialize without injected category, and exact entry comparison rejects a changed ID/image/mount/port.
- [ ] Run `go test ./internal/config ./internal/operations -run 'TestLegacyRegistryRoundTrip|TestCategoryKeysDoNotCollide|TestExistingTargetIgnoresCurrentCatalog|TestRegistryIdentityRejectsChangedCategoryAndID' -count=1`; expect RED.
- [ ] Implement the named types and conversion; reject unknown category/schema instead of treating it as absent. Leave legacy profile/provider lookup unchanged.
- [ ] Run the same command; expect PASS, then `go test ./internal/config ./internal/operations -count=1`.
- [ ] Commit this task's files only.

### Task 2: Separate image recipes and recorded agent versions

**Files:** Create `Dockerfile.sandbox`, `internal/operations/agentversion.go` and tests; modify `Dockerfile`, `internal/operations/images.go`, `internal/runtimeinstall/install.go` only where manifest output needs it.

**Interfaces:** Produce `func ResolveStableAgent(ctx context.Context, agent string) (version, source string, err error)` and `func ImageSpec(category config.Category, entry config.Entry, lock config.Lock) (tag string, labels map[string]string, err error)`. Sandbox build resolves current stable official package before build and records resolved version/source; container build uses current lock and installer with category/selection fingerprint labels.

- [ ] Write tests `TestSandboxRecipeOmitsTools`, `TestStableLookupFailureDoesNotReuseLatest`, `TestContainerRecipeUsesLockedStack`, and `TestImageFingerprintSeparatesCategories`; inspect build args/manifest and assert exact version/source, no methodology/utility stage for sandbox, pinned values for container.
- [ ] Run `go test ./internal/operations -run 'TestSandboxRecipeOmitsTools|TestStableLookupFailureDoesNotReuseLatest|TestContainerRecipeUsesLockedStack|TestImageFingerprintSeparatesCategories' -count=1`; expect RED.
- [ ] Implement `ResolveStableAgent` for official stable Codex and Claude Code packages, rejecting unsupported agents until a verifiable release source exists. Keep lookup injectable for deterministic tests; do not silently use `latest` as stored version.
- [ ] Split the Docker build recipes and manifest/label generation; SBX template load and `--skills off` apply only to sandbox.
- [ ] Run focused tests and `go test ./internal/operations ./internal/runtimeinstall -count=1`; expect PASS.
- [ ] Commit this task's files only.

### Task 3: Docker managed-container backend

**Files:** Create `internal/operations/backend.go`, `docker_backend.go`, `docker_backend_test.go`; modify `internal/operations/images.go` for Docker store use.

**Interfaces:** Define `type Backend interface { List(context.Context) ([]RuntimeState, error); Create(context.Context, config.Entry) (config.Entry, error); Inspect(context.Context, config.Entry) (RuntimeState, error); Exec(context.Context, config.Entry, []string, io.Reader, io.Writer, io.Writer) error; Stop(context.Context, config.Entry) error }`. `RuntimeState` includes exact runtime ID, image ID, running state and observed mount/network/ports. Docker backend accepts only inspected managed IDs and labels.

- [ ] Write fake-Docker-CLI tests `TestDockerRejectsForeignSameName`, `TestDockerRejectsChangedImageMountOrPorts`, `TestDockerUsesBridgeAndLoopbackPorts`, `TestDockerPreservesHomeOnRestart`, and `TestDockerDaemonFailureIsUnknown`. Assert no stop/exec call after mismatch; assert `--mount` for one canonical workspace path, project label, bridge mode and explicit `127.0.0.1` publications.
- [ ] Run `go test ./internal/operations -run 'TestDockerRejects|TestDockerUsesBridge|TestDockerPreservesHome|TestDockerDaemonFailure' -count=1`; expect RED.
- [ ] Implement Docker build/run/inspect/exec/stop with `process.Command(ctx, ...)`; validate the inspected ID, label, image, mount, network and ports before every mutation. Use a writable layer or managed volume for home; report incompatible reuse without recreation.
- [ ] Run focused tests and `go test ./internal/operations -count=1`; expect PASS.
- [ ] Commit this task's files only.

### Task 4: Operations dispatch, snapshots and updates

**Files:** Create `internal/operations/sbx_backend.go`, `category_service_test.go`; modify `requests.go`, `service.go`, `snapshot.go`, `versions.go`, `providers.go`, `internal/sandboxupdate/update.go`, `internal/operationlock/lock.go`.

**Interfaces:** Service selects backend from `Target.Category`; category-bearing lock key and registry re-read happen before execution. `Snapshot` retains independent SBX and Docker errors; an explicit version/update uses the selected backend transport. Legacy uses the current SBX updater; new sandbox updates agent only; container updates full stack.

- [ ] Write tests `TestDispatchUsesExactCategory`, `TestSnapshotIsolatesDockerFailure`, `TestLegacyStopWithRemovedUtility`, `TestNewSandboxUpdateTouchesAgentOnly`, `TestContainerUpdateKeepsStackStages`, `TestCancellationMarksGuestUncertain`; assert exact backend calls and result/ledger state.
- [ ] Run `go test ./internal/operations -run 'TestDispatchUsesExactCategory|TestSnapshotIsolatesDockerFailure|TestLegacyStopWithRemovedUtility|TestNewSandboxUpdateTouchesAgentOnly|TestContainerUpdateKeepsStackStages|TestCancellationMarksGuestUncertain' -count=1`; expect RED.
- [ ] Move SBX command behavior behind `Backend`, dispatch after frozen registry validation, separate catalog validation for image-changing requests, and preserve existing typed events/results.
- [ ] Run focused tests and `go test ./internal/operations ./internal/sandboxupdate -count=1`; expect PASS.
- [ ] Commit this task's files only.

### Task 5: CLI and TUI category flows

**Files:** Modify `internal/cli/cli.go`, `selection.go`; `internal/tui/wizard.go`, `create.go`, `dashboard.go`, `jobs.go`, `panels.go`; add focused Go tests beside each.

**Interfaces:** `--category sandbox --agent codex|claude` creates agent-only SBX; existing `--profiles <agent>-<methodology>` create/build selects container; existing-target commands use stored category. The wizard returns a typed category choice. Dashboard rows use category-bearing stable keys.

- [ ] Write `TestCLIProfileCreateMapsToContainer`, `TestCLISandboxRequiresAgent`, `TestOmittedTargetIsAmbiguousAcrossCategories`, `TestWizardHidesStackForSandbox`, `TestDashboardKeepsCategoryRowsDistinct`, `TestConfirmationShowsMountNetworkPorts`; assert parsed requests and rendered data, not snapshots of the entire screen.
- [ ] Run `go test ./internal/cli ./internal/tui -run 'TestCLIProfileCreateMapsToContainer|TestCLISandboxRequiresAgent|TestOmittedTargetIsAmbiguousAcrossCategories|TestWizardHidesStackForSandbox|TestDashboardKeepsCategoryRowsDistinct|TestConfirmationShowsMountNetworkPorts' -count=1`; expect RED.
- [ ] Implement CLI parsing and wizard/dashboard flow using the Task 1 keys and Task 4 service; no host catalog validation for existing-target stop/unregister/versions.
- [ ] Run focused tests and `go test ./internal/cli ./internal/tui -count=1`; expect PASS.
- [ ] Commit this task's files only.

### Task 6: Launcher provider routing and secure relay

**Files:** Modify `internal/launcher/launcher.go`, `internal/gateway/relay.go`, `internal/gateway/policy.go`, `internal/operations/providers.go`; add Go tests in those packages.

**Interfaces:** New provider IDs are `sbx-<agent>` and `container-<agent>-<methodology>`; legacy IDs are unchanged. Launcher resolves a provider to a trusted category target, then runs `sbx exec` or `docker exec`. Docker relay is enabled only after reachability from the container is checked, with one-use capability and host-held bearer token.

- [ ] Write `TestLauncherRoutesCategoryAndPreservesLegacyID`, `TestDockerRelayUnreachableRefusesRegistration`, `TestDockerRelayKeepsHostCredentialOutOfGuest`, `TestLauncherSignalStopsDockerExecGroup`, and `TestProviderUnionRetainsUserEntries`.
- [ ] Run `go test ./internal/launcher ./internal/gateway ./internal/operations -run 'TestLauncherRoutesCategory|TestDockerRelay|TestLauncherSignal|TestProviderUnion' -count=1`; expect RED.
- [ ] Implement category routing and relay transport. Do not call `sbx policy` for Docker. Preserve stdin/stdout, workspace and signal cleanup contracts.
- [ ] Run focused tests and `go test ./internal/launcher ./internal/gateway ./internal/operations -count=1`; expect PASS.
- [ ] Commit this task's files only.

### Task 7: Whole-flow verification and disposable live environment

**Files:** Extend Go integration tests under `internal/tui` and `internal/operations`; update README and the management TUI verification report with observed evidence.

**Interfaces:** No new production API. Test harness uses isolated trusted runtime, Paseo home and disposable workspace/container/sandbox names.

- [ ] Run `make test`, `make vet`, `make build` and record exit codes; repair concrete regressions before claiming completion.
- [ ] Exercise one Codex and one Claude Code agent-only sandbox; record installed agent/version/source, absence of managed tools, shell, stop and provider behavior.
- [ ] Exercise at least one pinned-stack Docker container: build, selection utility, DNS/HTTPS, workspace read/write and ownership, explicit loopback port, restart/home persistence, shell, stop, update and versions.
- [ ] Test same-workspace dual rows and both provider launches, Docker relay reachability and session termination with disposable resources. Separate login/model-call limits from transport results.
- [ ] Obtain an independent whole-branch review, resolve Important findings, repeat affected checks and update the evidence ledger; commit docs/tests.

## Handoff

The user selected per-task subagent implementation and independent review. After plan review, use `superpowers:subagent-driven-development` in this existing isolated worktree. Complete and review the preserved management TUI final-review fixes before Task 1.
