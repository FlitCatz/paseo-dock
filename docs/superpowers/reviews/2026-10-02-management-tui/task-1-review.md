## Spec Compliance

- ❌ Issues found: the shared-lock contract is incomplete when prune removes runtime directories containing active sandbox locks (`internal/operations/workspaces.go:317`, `internal/operationlock/lock.go:51`). The cancellation-result contract also loses the updater's own timeout when persisting its result fails (`internal/sandboxupdate/update.go:97`, `internal/operations/service.go:292`).
- ✅ The requested service and typed request/result interfaces are present (`internal/operations/service.go:22`, `internal/operations/service.go:238`, `internal/operations/requests.go:8`, `internal/operations/selection.go:16`). CLI execution routes through the service, preserving small compatibility wrappers (`internal/cli/cli.go:26`, `internal/cli/cli.go:38`, `internal/cli/selection.go:8`).
- ✅ The additional UpdateLockedContext API matches the controller's explicit ruling (`.superpowers/sdd/2026-10-02-management-tui/progress.md:59`, `internal/sandboxupdate/update.go:58`). Unchanged launcher/paseo files are justified by the forwarded installLauncher and preserved Update entry point, rather than missing execution-layer work (`internal/cli/cli.go:32`, `internal/launcher/launcher.go:458`).
- ⚠️ Cannot verify from this diff: actual guest termination and live sandbox lifecycle behavior. The report explicitly leaves these for later controller validation (`.superpowers/sdd/2026-10-02-management-tui/task-1-report.md:208`). KindVersions and restoration remain reserved for later tasks, as reported (`internal/operations/service.go:340`, `.superpowers/sdd/2026-10-02-management-tui/task-1-report.md:68`).

## Strengths

- Request-local service copies, cloned selections/targets, and serialized event delivery isolate concurrent execution state and terminal output (`internal/operations/service.go:53`, `internal/operations/service.go:242`). The event-routing and target-freezing tests exercise the actual process and registry boundaries (`internal/operations/service_test.go:40`, `internal/operations/service_test.go:180`).
- Confirmed entries are checked again while the sandbox lock is held; automatic lock conflicts retain their skip policy, while explicit operations report conflicts (`internal/operations/service.go:280`, `internal/sandboxupdate/update.go:41`). Shared root/home locks remain effective across runtime bases and symlink aliases (`internal/operationlock/lock.go:43`, `internal/operationlock/lock_test.go:10`).
- Progressive create writes preserve the prior activation until finalization and return persisted resources after partial failure (`internal/operations/workspaces.go:185`, `internal/operations/workspaces.go:228`, `internal/operations/service_test.go:152`). Cancellation tests verify an incomplete updater record and absence of an automatic sandbox stop (`internal/operations/service_test.go:399`).

## Issues

### Critical

- None found.

### Important

- **Prune can unlink a held sandbox lock and defeat mutual exclusion.** `internal/operations/workspaces.go:317` removes the entire runtime, including `updates/*.lock`. Prune acquires only runtime/home/root locks (`internal/operations/service.go:339`, `internal/operations/service.go:351`), whereas update/stop and automatic updates acquire locks inside the runtime (`internal/operations/service.go:280`, `internal/sandboxupdate/update.go:41`, `internal/operationlock/lock.go:51`). If a workspace disappears during an update, prune can delete that lock while the installer still runs. A later acquisition recreates a different inode, so the original flock no longer protects that sandbox; update records may also be lost. Preserve the legacy sandbox lock files during registry pruning, or introduce a lifecycle protocol that every sandbox-lock acquirer participates in. Merely holding a lock while unlinking it does not prevent a new caller from creating a replacement inode. Add a focused regression covering prune during a held update lock and subsequent acquisition.

- **An internal updater timeout can lose NeedsVerification when the state write fails.** `internal/sandboxupdate/update.go:82` creates a child context with its own three-minute deadline, but `internal/sandboxupdate/update.go:97` returns the filesystem error alone if record persistence fails. In that case the caller's context can still be live, and the error no longer wraps DeadlineExceeded, so `internal/operations/service.go:292` leaves NeedsVerification false despite a canceled installer. Return an error preserving both the persistence failure and the child context error, such as errors.Join(err, ctx.Err()), and cover the internal-deadline plus record-write-failure path. The existing test at `internal/operations/service_test.go:202` cancels the parent context and therefore does not cover this case.

### Minor

- **The process-group test can pass without starting its descendant.** `internal/process/process_test.go:35` starts a 150 ms timeout before launching the helper, and `internal/process/process_test.go:44` accepts an empty/unparseable child PID. Under slow startup, cancellation may happen before the child exists, satisfying the error/elapsed assertions without testing inherited stdout or descendant termination. Synchronize on the helper's child-ready signal, require a valid PID, and then cancel.

## Checks and Limits

- Read the supplied diff in bounded chunks; one aggregate tool response truncated part of the CLI extraction and that diff segment was reread. No git commands, source/index/branch mutations, or test reruns were performed. The final passing race-suite evidence is recorded at `.superpowers/sdd/2026-10-02-management-tui/task-1-report.md:152`.
- Named cross-cutting risk: automatic updater lock scope. Checked the unchanged launcher call at `internal/launcher/launcher.go:458`; its Update call ends before the agent exec is assembled, so the agent session is not held under the updater lock.
- Named cross-cutting risk: prune eligibility and lock-directory lifecycle. Checked `internal/config/config.go:179` and `internal/config/config.go:185`; a missing workspace can retain a registry eligible for prune while its updater is active.
- The updater diff cuts UpdateLockedContext into several partial hunks; read `internal/sandboxupdate/update.go:58` through its refresh prelude once to verify the complete timeout/persistence error path.

## Assessment

**Task quality: Needs fixes.**

The extraction and request isolation are sound, and the behavioral tests cover the principal service boundaries. The prune/lock-file lifetime conflict and lost internal-timeout classification must be fixed before relying on the shared execution contract.
