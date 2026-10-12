# Branch closure and integration decision — 2026-10-12

Feature development is frozen. Implementation and offline checks are distinct
from complete product acceptance; authenticated inference and model-driven MCP
remain unverified. No merge, push, or resource/policy changes are authorized by
this maintenance decision.

## Integration decision

Use three dependency-ordered PRs rather than one merge. Proposed logical ranges:

| Review unit | Existing source range | Scope and dependency |
| --- | --- | --- |
| 1. Management TUI | `main..c7fe51a` | Shared Service, jobs, state dashboard and final TUI fixes |
| 2. Runtime categories | `c7fe51a..99a2b4e` | Schema, Docker backend, category CLI/TUI, secure relay and native transport; requires 1 |
| 3. Codex userns opt-in | `99a2b4e..final closure` | Frozen policy identity, CLI/TUI choice, confinement helper and closure corrections; requires 2 |

These are proposed review boundaries, not newly created/published PRs. Native and
categories share identity/backend/transport changes, so splitting them further
would obscure their contracts. Userns is a separate security decision that can
be reverted without removing TUI/category management. Existing history includes
two category planning commits before the final TUI fixes; organize those docs
with review unit 2 when constructing review snapshots. Preserve source history.

Before publishing, apply the relevant final maintenance to each review snapshot:
remove its raw logs, keep its summary, and ensure its helper targets reference
only helpers available in that snapshot. Run test/vet/build on each actual PR
head. Historical boundary commits have not been freshly tested by this closure;
passing the final combined head does not establish that every intermediate PR
head passes. Do not merge all three under a full acceptance claim.

Main's two uncommitted plan documents remain intact. They can be compared with
the integrated version and removed only after integration, without losing edits.

## Review disposition

Independent scoped review of `737f398..69652ee` confirmed foreign-CID/policy
rejection before cleanup enrollment but found a P2 cancellation regression:
verification captured the canceled creation context, skipping enrollment and
independent cleanup after Service returned a recovered CID. The fix uses an
independent bounded read-only context and keeps all CID/policy checks.
`TestCanceledCreationStillEnrollsVerifiedOwnedGuest` reproduced `context canceled`
before the fix and verifies enrollment, deadline release, foreign-CID rejection
and policy failure after it. This review does not approve the entire branch.

The final suite also exposed `TestLaunchStdioAndExit` inheriting Paseo's
`PASEO_AGENT_CWD`, overriding its temporary process cwd. A focused run reproduced
the failure. The test now clears that environment variable for its cwd fallback
fixture; production behavior is unchanged.

## Verification and evidence policy

`make test` runs `go test -race ./...` plus `make test-helpers`; `make vet`
includes the same offline helpers. Explicit category-verify source files avoid
the directory's unrelated standalone main programs. Native live acceptance stays
explicitly opt-in and separate. No default test starts real guests or a model.
CI now calls these Make targets too. The live userns helper is macOS/OrbStack-specific:
two `/private/tmp` daemon identity fixtures explicitly skip on other OSes without
weakening the helper's path guard. Ownership/cancellation and unrelated-home
rejection remain cross-platform. The category parser keeps only two actual
initialize frames in an embedded fixture, so a clean checkout needs no archived
logs. Its nested race-instrumented provider/guard/fixture subprocesses each have
Go's default exit delay; the bounded timeout is 10 seconds rather than 3 seconds.

36 previously committed raw logs and three pending final logs were preserved
byte-for-byte under ignored `.artifacts/review-logs/` and removed from docs.
Per-directory inventories record filename, bytes and SHA256. Reports retain
results, failed attempts and limits; raw-log links now lead to the summary.
`.gitignore` prevents new docs logs from entering future commits. This reduces
checkout noise, not history size; no history rewrite is requested or performed.
One-time resource identities in historical reports remain provenance records,
not configuration or instructions to operate those resources.

Fresh final checks on the combined closure tree:

| Command | Result |
| --- | --- |
| `make test` with local socket access | Exit 0; all package race checks plus userns helper (1.025s) and category helper (21.521s) pass; unchanged packages use Go's cache |
| `make vet` | Exit 0, including both helpers |
| `make build` | Exit 0, three product binaries |
| `go build ./internal/operations/testdata/userns-acceptance` with temporary output | Exit 0 |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c` for userns helper | Exit 0; compile only, no Linux execution claim |
| Archive inventory verification | All 39 copies match byte counts/SHA256, no raw docs logs remain |
| `git diff --check` | Exit 0 |

The first full run failed the inherited launcher environment, and the next run
exposed the deleted-log dependency plus the nested race subprocess timeout.
Those failures were addressed as described above; they are not counted as passes.
The final independent maintenance review found no remaining Critical/Important
issues. Its scope covers these corrections, Make/CI paths, evidence cleanup and
status documents; it does not replace acceptance or checks on future PR heads.

## Pending acceptance gates

- Authenticated model inference inside the selected runtime, including normal
  provider routing and observable response, remains deferred until login is available.
- A model must actually invoke an MCP tool through the relay and return its
  result. Initialize/EOF, relay reachability and confinement probes do not pass
  that gate. Native authenticated model/tool execution remains unverified too.
- Actual confinement was exercised on OrbStack Docker Engine 29.4.0 with arm64
  Linux guests and Codex 0.159.2/0.161.0. amd64 has unit coverage only; an amd64
  guest run is still required before claiming live support there.
- An AppArmor-enabled Linux host has not been tested. Seccomp does not override
  AppArmor; mount/user namespace restrictions may independently block bubblewrap.
  This is a compatibility risk, not an observed failure on such a host. Future
  acceptance should record host/kernel/Engine, loaded AppArmor profile and denied
  audit events, and run the same confinement checks without disabling host policy.
  [Docker's AppArmor documentation](https://docs.docker.com/engine/security/apparmor/)
  describes default profile application and `dmesg`/`aa-status` diagnostics.

The scoped reviewer declined to judge whole-branch security/correctness,
authenticated inference/MCP, historical transcript authenticity, untested
platforms and intermediate PR checks. Those remain gates or historical evidence
limits; they are not implicitly approved by this maintenance.
