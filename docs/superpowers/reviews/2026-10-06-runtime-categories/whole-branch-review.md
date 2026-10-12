# Whole-branch review and recovery fixes

Date: 2026-10-08. Reviewed baseline: `e0f4263..2012474`, branch `feat/management-tui`.
The interrupted review was recovered from the prior session and completed by an independent
read-only reviewer. Four findings were confirmed; each was reproduced in a Go test before
its production fix. Independent scoped re-review found no new Important defect.

| Finding | Reproduction and resulting behavior |
|---|---|
| P1: SBX failure rewrote healthy Docker state | `TestSBXFailurePreservesDockerShell` failed with Docker `unknown`. An SBX connection failure now invalidates only SBX rows, preserving a healthy Docker shell action. A fatal snapshot error still invalidates all retained rows. |
| P1: CLI shell forced updates | `TestCLIShellRespectsAutomaticUpdatePolicy` failed for opt-out, recent success and recent failure in legacy SBX, agent-only SBX and Docker. CLI shell now requests automatic policy; explicit CLI/TUI updates still force refresh. Automatic lock conflicts retain the previous skip policy. |
| P1: signals orphaned an active CLI shell | `TestActiveCLIShellSignalCancelsChild` failed for SIGINT and SIGTERM sent only to the CLI after shell launch. The signal context now covers both update and the interactive child; child termination is awaited before releasing the target lock. |
| P2: partial Docker failures hid CID | `TestCLIPartialCreateReportsDockerIdentity` reproduces actual registry persistence failure after backend creation; `TestJobLogReportsUnrecordedDockerIdentity` checks the TUI recovery log. Both surfaces now show the retained CID and registry path. CLI diagnostics use stderr, preserving stdout. |

Focused regression command passed (exit 0):

```sh
go test -race ./internal/cli ./internal/tui -run 'Test(ActiveCLIShellSignalCancelsChild|CLI(ShellRespectsAutomaticUpdatePolicy|PartialCreateReportsDockerIdentity)|SBXFailurePreservesDockerShell|JobLogReportsUnrecordedDockerIdentity)$' -count=1
```

The policy regression checks Docker updater dispatch; its shell-launch assertion is limited
to SBX. Existing Docker shell lifecycle tests continue to cover locked execution and identity
validation. No runtime policy or live resource was changed by this fix round.

The first full `make test` was restricted by the execution sandbox: local httptest servers
and MCP relays could not bind ports (`operation not permitted`). A rerun with local socket
access passed (exit 0): CLI 9.521s, launcher 41.204s, operations 57.046s; remaining
packages also passed, including TUI. `make vet`, `make build` and `git diff --check`
also exited 0. Commands used `GOCACHE=/private/tmp/paseo-sbx-go-cache` and
`GOTMPDIR=/private/tmp`. The environment-blocked first run is not counted as green.

## Acceptance remains incomplete

The independently reviewed [Task 7 evidence](task-7-report.md) still records Codex 0.161.0
workspace-write execution failing in Docker before the command starts, with bubblewrap's
namespace permission error. App-server initialize success proves transport, not usable
development execution. A compatible confined runtime configuration requires separate review
and fresh live proof. No sandbox bypass, capability addition, unconfined profile or host
daemon/sysctl change was authorized or applied. Login/model inference, end-to-end MCP tool
use and live native Antigravity runtime inheritance remain unverified.

This report closes the four code defects; it does not mark Task 7 or the whole feature accepted.
