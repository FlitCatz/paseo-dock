# Task 2 report — typed snapshots and version provenance

Status: DONE; controller review pending.
Base: 4e0e0d3.
Worktree: /Users/dev-r/devsbx/paseo-sbx/.worktrees/management-tui.

## Implementation

- Added the specified Query, Connection, Snapshot, Workspace and Sandbox contracts and immutable Service.Snapshot.
- Polling discovers local registry JSON strictly, checks saved workspace existence, joins runtime rows by exact sandbox name, and separates unmanaged rows. Valid empty arrays mean missing; malformed JSON/wrappers/null/invalid rows mean a connection error and unknown managed runtime states. Malformed registries retain a stable canonical runtime ID and a visible Workspace.Error.
- Supports verified sbx `{sandboxes:[...]}` and direct-array schemas. Requested templates support verified `{images:[...]}` and direct arrays, with typed repository/tag/id rows. Extra real schema fields are accepted.
- Core polling only runs `sbx ls --json` and reads local JSON; Paseo, Docker and template subprocess queries require explicit Query flags. Each connection exposes independent error/status/time. Subprocess stdout and stderr are bounded to 1 MiB each and each query has a five-second child timeout plus caller cancellation/process-group termination.
- Registration checks actual enabled managed provider, adapter, exact shared launcher command and profile. It does not infer registration from active_profiles. Unreadable or malformed config is unknown. An inactive workspace can still observe the shared provider retained for another workspace. Native Antigravity is marked unmanaged because the Paseo plugin, rather than ProviderConfig, owns that provider.
- Matching update state exposes the real Attempt, Success, Versions and Errors. A mismatched sandbox/image record is excluded. ImageMismatch reflects saved selection image tags. InitialVersions is explicitly documented as configured lock provenance and carries no confirmation time.
- Implemented the reserved KindVersions route. It revalidates the frozen target under the existing sandbox lock, emits an exec-may-start warning before the first guest command, and probes adapter binaries, guest installation manifests, plugin git HEAD, OpenSpec, RTK and Headroom binaries. It runs no installer, package-network query, git fetch, activation or provider mutation.
- Explicit observations persist separately under `version-checks/<same sandbox hash>.json`, preserving updater retry/auto-update policy. Partial successful probes retain real confirmation timestamps and per-component errors; cancelled unsuccessful probes never manufacture confirmations. A started exec conservatively sets Changed and cancellation NeedsVerification, since it may have started the guest. The separate local ledger is itself an authorized mutation only during the explicit operation.

## Approved interface refinements

Controller approved, and progress.md already records:

- `Snapshot.TemplateRows []Template`, with `Template{Repository, Tag, ID string}`.
- `Result.VersionCheckedAt time.Time`, `VersionErrors map[string]string`, `Warnings []string`.
- `Sandbox.VersionCheck *VersionCheck`; VersionCheck carries sandbox/image, CheckedAt, Versions, Errors and is matched by both sandbox and image.
- Private strict registry discovery in snapshot.go, preserving existing config.Registries and config.Installed APIs/legacy mutation skip semantics. Consequently config production files did not need changes.
- GSD remains unconfirmed when there is no authoritative current installed-version probe; original installed lock values are never substituted. This is the controller-approved behavior, not an invented VERSION path or a package-network lookup.

## Test approach

All new runtime logic runs directly against real filesystem registries/config/ledgers and the actual operations service. Only external sbx/Paseo/Docker processes are replaced by the Go test executable, using exact argv-keyed response fixtures that fail unknown commands. Temporary HOME/Paseo homes and temporary runtime directories isolate every test; no live sandbox or real Paseo configuration was accessed or changed.

All five required named tests are present:

- TestSnapshotReadOnly
- TestSnapshotJoinAndErrors
- TestVersionProvenance
- TestProviderRegistrationIsNotActivation
- TestVersionsDoesNotInstall

Additional coverage includes requested independent connections, direct/wrapped template schema validation, bounded captures, caller cancellation, image mismatches, shared provider semantics, malformed provider/Docker structures, explicit-record identity matching/restart persistence, partial methodology failure, changed-target rejection and malformed utility manifests.

16 new top-level tests, with table subcases, pass. Existing operations and CLI regression tests also pass.

## TDD evidence

All commands used explicit workdir above and RTK wrappers. GOCACHE was `/private/tmp/paseo-sbx-management-go-cache`.

### RED — initial snapshot behavior

Command:

```
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/operations ./internal/config -run 'TestSnapshotReadOnly|TestSnapshotJoinAndErrors|TestVersionProvenance|TestProviderRegistrationIsNotActivation' -count=1
```

Before implementation, a minimal declared API shell returned an empty Snapshot so tests could fail on behavior rather than undefined symbols.

Relevant output (exit 1):

```
--- FAIL: TestSnapshotReadOnly
    lost managed workspace: {Workspaces:[] ...}
--- FAIL: TestSnapshotJoinAndErrors
    ... array/wrapper/empty/bad-json/wrong-wrapper/null/bad-row/broken-registry failed ...
--- FAIL: TestVersionProvenance
    ... matching/wrong-image/wrong-sandbox failed ...
--- FAIL: TestProviderRegistrationIsNotActivation
    ... absent/disabled/unreadable/wrong-command/wrong-adapter/managed failed ...
FAIL paseo-sbx/internal/operations
ok paseo-sbx/internal/config [no tests to run]
```

GREEN: same command, exit 0:

```
ok paseo-sbx/internal/operations 0.244s
ok paseo-sbx/internal/config 0.005s [no tests to run]
```

### RED — explicit versions

Command:

```
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/operations ./internal/config -run 'TestVersions|TestVersionCheck' -count=1
```

Before replacing the reserved route, relevant output (exit 1):

```
--- FAIL: TestVersionsDoesNotInstall
    versions is not implemented yet
--- FAIL: TestVersionsPartialFailureDoesNotConfirmImageLock
    unconfirmed lock value or partial success lost: {Changed:false ... Versions:map[] ...} versions is not implemented yet
--- FAIL: TestVersionsCancellationDoesNotConfirm
    cancellation invented confirmation or missed exec start: {Changed:false ...} versions is not implemented yet
FAIL paseo-sbx/internal/operations
```

GREEN after implementation and fixture corrections:

```
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/operations ./internal/config -run 'TestSnapshot|TestVersion|TestProviderRegistration' -count=1
ok paseo-sbx/internal/operations 0.567s
ok paseo-sbx/internal/config 0.005s [no tests to run]
```

During the focused optional tests, initial fixture mistakes were corrected: the fake Paseo argv expected a noncanonical temporary path whereas production correctly canonicalized it, and the image-match fixture selected codex-native although the real default catalog only permits native for Antigravity. Those were test-fixture failures, not product defects. The image test now uses supported codex-superpowers. The first compilation of versions.go also reported one unused import, which was removed before GREEN.

### RED — malformed readable structures

Command:

```
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/operations -run 'TestSnapshotMalformedProviderStructureIsUnknown|TestSnapshotMalformedDockerResponseIsError' -count=1
```

Relevant output (exit 1):

```
--- FAIL: TestSnapshotMalformedProviderStructureIsUnknown
    malformed provider config treated as readable: ... Registration:unregistered ...
--- FAIL: TestSnapshotMalformedDockerResponseIsError
    malformed independent response accepted: ... Docker:{Error: Status:connected} ...
FAIL paseo-sbx/internal/operations
```

Implemented strict object validation; GREEN included in the operations/config race run below.

### RED — malformed utility manifest suppression

Command:

```
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/operations -run TestVersionsMalformedUtilityManifestIsErrorWithEmptySelection -count=1
```

Relevant output (exit 1):

```
--- FAIL: TestVersionsMalformedUtilityManifestIsErrorWithEmptySelection
    malformed installed manifest suppressed: {Changed:true Versions:map[agent:2.0] ... VersionErrors:map[] ...} <nil>
FAIL paseo-sbx/internal/operations
```

Fixed the error branch so a malformed readable manifest remains an error even with no selected utilities. Only an unreadable optional utilities manifest is suppressed when saved selection independently confirms no utilities.

## Final verification

Required package GREEN:

```
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./internal/operations ./internal/config
ok paseo-sbx/internal/operations 2.398s
ok paseo-sbx/internal/config (cached)
```

Full repository suite, run once after behavior changes, with require_escalated for local httptest listeners:

```
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache make test
```

Exit 0, output:

```
go test -race ./...
? paseo-sbx/cmd/agent-wrapper [no test files]
? paseo-sbx/cmd/install-runtime [no test files]
? paseo-sbx/cmd/paseo-sbx [no test files]
ok paseo-sbx/internal/cli 3.990s
ok paseo-sbx/internal/config (cached)
ok paseo-sbx/internal/gateway (cached)
ok paseo-sbx/internal/launcher (cached)
ok paseo-sbx/internal/operationlock (cached)
ok paseo-sbx/internal/operations (cached)
ok paseo-sbx/internal/process (cached)
ok paseo-sbx/internal/runtimeinstall (cached)
ok paseo-sbx/internal/sandboxupdate (cached)
ok paseo-sbx/internal/tui (cached)
```

Also exit 0:

```
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache make vet
# go vet ./...
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache make build
# builds paseo-sbx, agent-wrapper and install-runtime with CGO_ENABLED=0
rtk git diff --check
# no output
```

## Files changed

- internal/operations/snapshot.go
- internal/operations/snapshot_test.go
- internal/operations/versions.go
- internal/operations/versions_test.go
- internal/operations/requests.go — controller-approved Result provenance fields.
- internal/operations/service.go — dispatch reserved KindVersions route.
- internal/operations/service_test.go — external Go process fake opt-in only.
- This ignored report and ignored progress.md execution ledger.

## Self-review and limitations

Read the new production files and changed existing contracts. Verified immutable service state, exact target/command boundaries, independent connection failures, malformed-vs-empty distinction, sandbox/image identity filters, real timestamp generation, no host execution of guest paths, unchanged updater records, and no polling guest exec/network/mutation. Fixed the malformed-utility suppression uncovered during review through RED/GREEN.

Snapshot owns only the current observation; retaining last successful rows/times across polling failures belongs to the later dashboard state, as required by the immutable service contract. Optional connections are zero/unqueried until explicitly requested.

No unresolved implementation concerns. GSD current version is intentionally unconfirmed with an explanatory VersionErrors entry. Native Antigravity provider registration is unmanaged rather than falsely asserted. Live disposable lifecycle/PTY verification remains for later tasks and was not performed in Task 2.

Commit: `593ff83 feat: expose read-only management snapshots` (7 production/test files, 1116 insertions, 2 deletions). Working tree clean after commit; report/ledger are intentionally ignored SDD artifacts. Initial sandboxed staging/commit could not create Git index.lock; authorized elevated git add/commit succeeded. No approval rejection occurred.

## Fix round 1 — review Important I1

FIX_BASE: 593ff83. Status: DONE; controller re-review pending.

Verified the finding: valid custom utility IDs could equal reserved `agent`/`methodology` keys. A successful utility probe overwrote genuine component versions; utility probe/metadata errors similarly overwrote the GSD unconfirmed error. Controller's progress.md ruling reserves component keys and uses `utility:<id>` for every explicit utility confirmation/error and its persisted ledger.

Changed only explicit version observation logic and covering tests. All utility-loop branches now derive one namespaced key before parsing metadata and pass it to confirm/fail, so Result.Versions, VersionErrors, returned component errors and persisted VersionCheck share the namespace. Custom utility IDs remain supported. Catalog validation, configured image-lock InitialVersions and the legacy updater schema are unchanged. The existing warning remains before guest execution.

Behavioral regression `TestVersionsUtilityIDsCannotOverwriteComponentProvenance` covers utilities named agent (RTK) and methodology (Headroom): successful probes, external probe errors, invalid metadata and unsupported installer errors. It independently asserts actual Codex confirmation, no confirmed GSD methodology, retained GSD unconfirmed error, correctly scoped utility keys/errors, pre-exec warning, genuine confirmation time and a fresh Service.Snapshot reading the persisted ledger. Existing RTK observation assertion was updated for the approved utility namespace.

### RED

```
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/operations -run TestVersionsUtilityIDsCannotOverwriteComponentProvenance -count=1
```

Exit 1, before the production fix:

```
--- FAIL: TestVersionsUtilityIDsCannotOverwriteComponentProvenance
    --- FAIL: .../success
        utility ID overwrote component confirmation: got map[agent:rtk 0.99.0 methodology:headroom 3.0.0] want map[agent:codex-cli 2.0.0 utility:agent:rtk 0.99.0 utility:methodology:headroom 3.0.0]
    --- FAIL: .../probe-errors
        utility ID overwrote component error: map[agent:exit status 1: external unavailable methodology:exit status 1: external unavailable]
    --- FAIL: .../metadata-errors
        utility ID overwrote component error: map[agent:json: cannot unmarshal array ... methodology:unsupported utility version probe unsupported]
FAIL paseo-sbx/internal/operations 0.109s
```

### GREEN and whole-suite verification

```
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./internal/operations -run 'TestVersions|TestVersionCheck|TestSnapshot|TestProviderRegistration' -count=1
ok paseo-sbx/internal/operations 2.108s
```

Full suite run once after the fix with require_escalated for local httptest listeners:

```
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache make test
```

Exit 0:

```
go test -race ./...
? paseo-sbx/cmd/agent-wrapper [no test files]
? paseo-sbx/cmd/install-runtime [no test files]
? paseo-sbx/cmd/paseo-sbx [no test files]
ok paseo-sbx/internal/cli 4.072s
ok paseo-sbx/internal/config (cached)
ok paseo-sbx/internal/gateway (cached)
ok paseo-sbx/internal/launcher (cached)
ok paseo-sbx/internal/operationlock (cached)
ok paseo-sbx/internal/operations 2.946s
ok paseo-sbx/internal/process (cached)
ok paseo-sbx/internal/runtimeinstall (cached)
ok paseo-sbx/internal/sandboxupdate (cached)
ok paseo-sbx/internal/tui (cached)
```

`rtk git diff --check` exited 0 with no output. Self-review read the two-file diff; confirmed every utility confirm/fail branch uses the namespace, reserved component/category keys and warning semantics remain intact, and persistence stores the same maps without a second conversion. No unresolved concerns.

Files: internal/operations/versions.go, internal/operations/versions_test.go; ignored report appended.

Fix commit: `714968f fix: namespace explicit utility version observations`. Working tree clean. Important I1 addressed; controller re-review pending.
