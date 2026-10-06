# Task 1 implementation report

Status: DONE
Base: 3143747c872d4a546a4f7e4b1fb297fd561769e1
Worktree: /Users/dev-r/devsbx/paseo-sbx/.worktrees/management-tui

## Implementation

- Introduced operations.Service, New, Execute, ShellCommand, ResolveSelection,
  SavedSelection, and the specified Kind/Target/Request/Event/Result types.
- Moved existing image build/load, workspace creation, selection validation,
  provider union/backup/merge, unregister and prune execution bodies into the
  planned operations files. CLI keeps flag parsing, standalone wizard, run-plugin
  entry, and small legacy forwarding wrappers. No operations import of cli/tui.
- Each Execute owns its selection, cloned target slice, context, change tracking,
  and output writers. Shared Service fields are never reassigned. With an emit
  callback, stages plus stdout/stderr are delivered as timestamped Event values;
  request output does not write terminal streams. Callback delivery is serialized
  per request. With emit=nil, existing CLI output behavior is preserved.
- Frozen registry Entry identities are checked before mutation and again under
  the sandbox lock for stop/update. Explicit register/unregister targets are
  similarly validated under their workspace/provider locks. Workspace and runtime
  identities are canonicalized and checked against the trusted runtime location.
- Added nonblocking process-wide flock acquisition, sorted/deduplicated keys,
  conflict errors, partial-acquisition unwind, canceled-context rejection, and
  idempotent release. Sandbox locks retain the updater's exact existing
  updates/<sha256 sandbox prefix>.lock path. Root outputs lock under the shared
  root; provider-home locks live in that home, so different runtime bases and
  symlink path aliases cannot bypass those shared locks.
- Build/load use a root lock; create/unregister use workspace locks; launcher and
  provider changes use runtime locks; provider merge also locks home and root
  generated output. Automatic updater contention still skips the update; explicit
  update/stop conflict promptly. The agent session itself is not held under the
  updater lock.
- Background subprocesses run in their own process group, kill the group on
  cancellation, and bound pipe waiting. Interactive ShellCommand retains the
  terminal-owned process group and performs no hidden update. CLI shell retains
  its existing automatic-update policy before opening the interactive command.
- UpdateContext accepts the caller context and emits component stages while
  retaining the existing three-minute limit, daily/hourly interval, disable flag,
  and progressive update record. Update remains its compatibility wrapper.

Controller-approved API refinement: sandboxupdate.UpdateLockedContext is an
exported narrow helper whose documented precondition is the matching sandbox
lock already held by its caller. Execute uses it to revalidate the target under
the same lock without nested re-acquisition. Update/UpdateContext acquire that
lock themselves. This was explicitly approved in the implementation exchange.

## Partial results and cancellation

- Changed tracks successful atomic JSON writes, registry removals, and externally
  started mutation commands. A failed command may have changed external state;
  its launch is conservatively reported as a change rather than claiming rollback.
- Create returns persisted registry resources in Remaining, including successful
  earlier creations when a later create or initialization fails. Existing previous
  activation/selection is retained until finalization, as the existing CLI tests
  require. A canceled in-flight external creation can still require verification;
  unrecorded existence is not invented in Remaining.
- Update/stop failures return the affected/unprocessed targets in Remaining.
  Canceled installers retain Attempt and component Errors in the updater record,
  do not claim Success, and return NeedsVerification. If writing that record also
  fails, the cancellation still returns NeedsVerification.
- Cancellation kills host process groups only. No sbx stop or automatic rollback
  is used to cancel an installer; guest termination is not claimed.
- Prune dry-run exposes canonical missing registry/profile identities in Missing,
  reports Changed=false, and preserves the registry. Successful prune retains the
  existing sandbox-preservation behavior. Frozen prune execution belongs to Task 5.
- KindVersions and RestoreSelection are reserved; snapshots, versions probing,
  restoration and UI are not implemented in this task.

## TDD evidence

All commands ran with explicit workdir at the worktree above and RTK wrappers.
Minimal compilable compatibility baselines were used for the newly named APIs;
the RED evidence below is behavioral assertion failure, not missing symbols.

Initial RED command:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/operations ./internal/process ./internal/operationlock ./internal/sandboxupdate
```

Observed exit 1:

```text
TestExecuteRejectsChangedTarget/stop: changed confirmed target accepted; executed stop two
TestExecuteRejectsChangedTarget/update: executed unconfirmed target (exec ... two ...)
TestUpdateContextCancellation: installer ignored caller cancellation
TestSharedUpdateLock: stop overlapped installer
TestProcessCancellationKillsGroup: child held stdout after cancellation: 3.009705875s
TestAcquireConflictAndUnwind: concurrent mutation accepted
FAIL operations; FAIL process; FAIL operationlock; ok sandboxupdate
```

These reproduce identity replacement, ignored external cancellation, missing stop
coordination, surviving child stdout, and absent conflict/unwind protection.

Additional RED→GREEN iterations from integration/self-review:

```text
go test ./internal/operations -run 'TestExecute(Logs|RejectsInvalid)'
RED: subprocess logs missing from request events; background request wrote terminal streams;
     invalid selection wrote build artifact
GREEN after request-local event writers and schema validation.

go test ./internal/operationlock -run TestAcquireSharedPathsAcrossRuntimeBases
RED root/home: same shared path acquired through a different runtime base
GREEN after locks were located at the shared root/home.

go test ./internal/operations -run TestPruneReturnsMissingTargets
RED: prune preview lost missing target (Missing=[])
GREEN after typed missing targets were populated. The test fixture was corrected
to canonicalize /var versus /private/var before comparing target identities.

go test ./internal/operationlock ./internal/operations -run 'Test(AcquireSharedPathAliases|StopCancellationNeedsVerification)'
RED: symlink alias bypassed shared output lock; interrupted stop lost uncertain result
GREEN after stable lock filenames and preserved context cancellation/verification.

go test ./internal/operations -run TestExecuteRejectsChangedTarget
RED register: accepted changed target, wrote provider configuration
RED unregister: accepted changed target, modified activation, wrote provider configuration
GREEN after frozen entry validation under workspace/provider locks.

go test ./internal/operations -run 'Test(ExecuteFreezesTargetsBeforeEvents|CanceledUpdateRecordFailureStillNeedsVerification|ExecuteReportsPartialCreation)'
RED: event callback changed confirmed request (stop two);
     state-write failure hid uncertain cancellation (Changed=true, NeedsVerification=false)
GREEN after cloning Targets before callback delivery and preserving verification
when canceled state persistence fails. Partial-create Remaining test characterized
the preserved progressive-write behavior and passed throughout.
```

Additional passing boundary tests cover UpdateContext directly with an external
cancellation and component callback, and ShellCommand executing only the terminal
shell command with no update/install call. External fake commands and process-tree
helpers are Go test executables; no Python or new runtime dependency was added.

Focused GREEN command:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./internal/operations ./internal/process ./internal/operationlock ./internal/sandboxupdate
ok operations 1.363s
ok process (cached)
ok operationlock 1.041s
ok sandboxupdate 1.040s
```

The unelevated focused CLI run reached the existing TestPluginAgentCreation
httptest listener restriction: bind operation not permitted. This was an
environment restriction, not a product regression. The authorized elevated
CLI/launcher run passed, followed by the full elevated race suite.

Final GREEN command (authorized elevated execution for local httptest listeners):

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./...
? cmd/agent-wrapper [no test files]
? cmd/install-runtime [no test files]
? cmd/paseo-sbx [no test files]
ok internal/cli 3.855s
ok internal/config (cached)
ok internal/gateway (cached)
ok internal/launcher (cached)
ok internal/operationlock (cached)
ok internal/operations 1.594s
ok internal/process (cached)
ok internal/runtimeinstall (cached)
ok internal/sandboxupdate (cached)
ok internal/tui (cached)
```

Exit 0. All ten tested packages passed, including existing CLI lifecycle,
selection/source-boundary/provider-union/updater and launcher tests. The three
command packages have no test files. git diff --check also passed.

## Changed files

- internal/operations/service.go
- internal/operations/requests.go
- internal/operations/selection.go
- internal/operations/images.go
- internal/operations/workspaces.go
- internal/operations/providers.go
- internal/operations/service_test.go
- internal/process/process.go
- internal/process/process_test.go
- internal/operationlock/lock.go
- internal/operationlock/lock_test.go
- internal/cli/cli.go
- internal/cli/selection.go
- internal/sandboxupdate/update.go
- this report

Existing internal/cli/paseo.go and internal/launcher/launcher.go needed no source
changes: the plugin's installLauncher forwards to the service, and launcher Update
now reaches the same operationlock implementation through its preserved API.

## Self-review and concerns

Reviewed extraction and new service/locking/process/updater code, existing
compatibility tests, and the final diff. Fixed the broader register/unregister
identity contract, shared-path lock aliasing, stream event routing, selection
schema validation, missing prune result, target freezing before callbacks, and
canceled state-write failure verification. No known unresolved implementation
issue remains.

Event observers must return promptly; Task 4 is responsible for a bounded
nonblocking log observer so view rendering does not backpressure subprocesses.
Host group termination does not prove guest installer termination. Real sandbox
lifecycle validation is deliberately left to the controller's later task; no live
sandbox or default Paseo home was used as a test target.

## Review fix round 1/5

Fix base: 4dff4c4. Addressed both Important findings from task-1-review.md.

### Prune / sandbox lock lifetime

Test: TestPruneDuringUpdatePreservesSandboxLock, operations/service_test.go.
The test starts a real UpdateContext against the Go sbx helper, waits for the
installer, removes its workspace, and invokes prune concurrently. It verifies
that prune reports the actual lock conflict, retains registry metadata and the
same lock inode, and rejects another sandbox acquisition while the updater holds
it. After cancellation/release, prune succeeds, forgets the registry, preserves
both registered and historical lock-file inodes, and permits a later acquisition.

RED command:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/operations -run TestPruneDuringUpdatePreservesSandboxLock
--- FAIL: TestPruneDuringUpdatePreservesSandboxLock (0.07s)
prune overlapped active installer: <nil>
prune replaced the live lock inode: no such file or directory
prune allowed reacquisition while installer holds lock
conflicting prune forgot registry: no such file or directory
completed prune replaced sandbox lock inode
FAIL operations 0.079s
```

Fix: prune acquires all registered sandbox keys in each affected missing runtime
before changing provider or registry metadata. Prior acquisitions unwind on a
conflict; all releases run in reverse order. Successful prune removes only
registry.json, selection.json and the legacy per-runtime agent-wrapper. It
retains the updates directory, update history, and every legacy lock inode;
historical automatic callers therefore continue acquiring the same file. Keeping
the history matches the policy that the sandbox itself is preserved, while
config.Registries and CLI list no longer see the forgotten workspace.

GREEN covering command:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/operations -run TestPruneDuringUpdatePreservesSandboxLock
ok operations 0.081s
```

### Internal deadline / failed result persistence

Test: TestInternalDeadlineAndRecordWriteFailure, sandboxupdate/update_test.go.
The test creates an expired child deadline with a still-live parent, forces the
atomic state rename to fail by placing a directory at the record destination,
and exercises the updater's actual persistence/completion boundary. It requires
both errors.Is(DeadlineExceeded) and errors.As(*os.LinkError) to survive. This is
the error classification consumed by Execute for NeedsVerification even when its
parent context remains live.

The existing completion block was extracted without behavior changes into a
private finishUpdate helper so the test reaches the exact production boundary
without waiting three minutes or adding a public timeout/test knob.

RED command:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test ./internal/sandboxupdate -run TestInternalDeadlineAndRecordWriteFailure
--- FAIL: TestInternalDeadlineAndRecordWriteFailure (0.01s)
internal deadline hidden by record-write failure: rename ... box.json: file exists
FAIL sandboxupdate 0.012s
```

Fix: finishUpdate now returns errors.Join(persistenceError, childContext.Err())
when record writing fails, preserving both the filesystem cause and the internal
timeout/cancellation classification. Successful persistence and normal component
failure behavior are unchanged.

GREEN covering command:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./internal/operations ./internal/sandboxupdate
ok operations 1.582s
ok sandboxupdate 1.059s
```

Final GREEN command, elevated for existing local httptest listeners:

```text
rtk proxy env GOCACHE=/private/tmp/paseo-sbx-management-go-cache go test -race ./...
? cmd/agent-wrapper [no test files]
? cmd/install-runtime [no test files]
? cmd/paseo-sbx [no test files]
ok internal/cli 4.033s
ok internal/config (cached)
ok internal/gateway (cached)
ok internal/launcher 1.138s
ok internal/operationlock (cached)
ok internal/operations (cached)
ok internal/process (cached)
ok internal/runtimeinstall (cached)
ok internal/sandboxupdate (cached)
ok internal/tui (cached)
```

Self-review: checked the lock acquisition/release order, no metadata changes on
lock conflict, inode retention after completed prune, the existing CLI prune
contract, and the preserved child-context error classification. git diff --check
passed. Changed files in this fix: operations/workspaces.go,
operations/service_test.go, sandboxupdate/update.go,
sandboxupdate/update_test.go, and this report. No public API or dependency added.

Remaining concern: the reviewer Minor finding about process-helper readiness is
explicitly deferred by the controller to final review and was not changed in
this round. Both assigned Important findings are resolved.
