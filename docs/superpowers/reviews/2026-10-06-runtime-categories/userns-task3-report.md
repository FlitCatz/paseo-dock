# Task 3 isolated Service acceptance

2026-10-12. Live acceptance passes for the exact product policy, image and tested
versions. Initial creation exposed a Docker capability spelling compatibility bug;
the independently reviewed correction was applied before continuation. Final guest
and isolated provider daemon are stopped. Authenticated model acceptance stays deferred.

## Exact resource and isolation

One new container was created through real `operations.Service.Execute(KindCreate)`
with normalized container/Codex/Superpowers/RTK selection and `CodexUserNS=true`.
Subsequent attempts reused this same exact CID; no replacement guest was created:

- CID: `f7a8eceb0f7700c2a75e7487c403fcc7dbcb7245b0f27ff6d8e10d7c521ff116`.
- Root: `/private/tmp/paseo-sbx-userns-live-117101514`.
- Normal computed image ref: `paseo-sbx-category-live-20261007/container/codex-superpowers:2026.09.30-8e8751383862`.
- Image: `sha256:7760a63ba6a62611c4434c1208c1c87236cd37d36b691da206ebd0d7e77d6521`, arm64.
- Policy: `codex-userns-v1`, digest `de699bf56093ed501d6cf021db43b48f6bd5bd843a715404e7d0714ff0ed0af8`.
- Source during passing acceptance: `ddcd4fdc5f26abc4656fdb08ce76813289020a66`.

The helper copies trusted lock/Dockerfile and freshly built host binaries into the
new root's `recipe`, then uses normal `Service.New(recipe)`. HOME, PASEO_HOME and
DOCKER_CONFIG are fresh isolated directories. Only the read-only discovered local
Docker endpoint is retained; credentials/context files are never copied. The sole
guest mount is the disposable workspace. No original registry/resource, source
`.generated` routing/provider file, daemon configuration, existing image/tag or
guest was modified. The probe is compiled from existing Go source as Linux arm64
before creation. The normal ImageSpec result matched the existing image, so no
image build/tag or fabricated registry was needed.

## Failure and correction

[Initial log](evidence-summary.md) records actual creation followed by creation
and cleanup guard failures. Docker 29.4.0 normalized requested `SYS_ADMIN` into
raw `CapDrop=["CAP_SYS_ADMIN"]`; the product validator originally accepted only
the short spelling. Do not interpret that first attempt as passing cleanup.

Reviewed correction `ddcd4fd` accepts precisely the two equivalent capability
spellings while retaining all other privilege/profile checks. The corrected helper
first performed a cleanup-only normal Service stop and verified exact
`running=false`: [cleanup recovery log](evidence-summary.md). The guest then
remained stopped until scoped review passed. Continuation required the original
CID file, untouched real registry, matching trusted lock/Dockerfile and full actual
Docker policy validation; it did not reconstruct a registry or bypass a guard.

Host binaries were rebuilt after the correction and refreshed in the private
trusted root. Initial wrapper SHA256 was
`2f5806506310d8d2e0ae069e8cfb1453f3674c6aef6c810dc8b7fe97d1a468c5`;
passing wrapper SHA256 was
`87425e6b422e65a027d2db14a426074777efffb70d91cabb298fa34386c357c8`.
Both are recorded in their corresponding logs. Normal provider registration was
performed inside isolated PASEO_HOME and its installed launcher was byte-compared
with the latest host build.

## Passing evidence

[Passing continuation log](evidence-summary.md), helper exit 0, records:

| Check | Actual outcome |
| --- | --- |
| Saved registry and selection | Both persist true opt-in and exact policy ID/digest; exact active target/image identity |
| Raw Docker policy | Matching loaded JSON profile, no-new-privileges, CapDrop CAP_SYS_ADMIN, no CapAdd/privileged mode, one workspace bind, zero ports |
| Baseline for both versions | Agent-owned outside-home write and TCP connection succeed before sandbox |
| Actual Codex 0.159.2 sandbox | Workspace write succeeds; outside-home/symlink/protected writes and TCP connection denied |
| Exact guest-installed 0.161.0 | Explicit version equality asserted; the same baseline and sandbox probes pass |
| Sandbox process state | CapEff=0, NoNewPrivs=1, Seccomp=2 asserted on every sandbox run |
| Negative kernel probes | AF_ALG/AF_VSOCK, bpf/perf_event_open/setns, unrelated unshare and mount requests denied |
| Forged operation requests | Forged policy and digest stop requests reject; false-opt-in create rejects on existing target; registry bytes and inspected state unchanged |
| Service stop/create resume | Exact same CID/entry/policy reused; guest home and workspace sentinel contents persist |
| Post-resume sandbox | Exact 0.161.0 confinement probe passes again |
| Final cleanup | Independent deadline Service stop succeeds; backend inspect verifies exact running=false |

Registration started a new isolated Paseo daemon (PID 27932). Its generated PID
record was checked, then supported `paseo daemon stop --home <isolated-home>`
returned exit 0. Status reports `localDaemon: stopped`, PID null, and the exact PID
is gone: [daemon cleanup transcript](evidence-summary.md). The helper
now also includes independent-deadline isolated-home daemon cleanup and clears
PASEO_HOST. After the full live run, the same isolated home was started again
(PID 34324) and the actual Go `cleanupDaemon` function was exercised directly by
the dedicated `-daemon-cleanup-only` mode. It returned exit 0; localDaemon is
stopped, PID null, exact PID gone, and the guest remains stopped. Focused race
tests prove wrong UID, missing server ID and invalid PID reject before CLI
dispatch without changing PID records, plus rejection of unrelated homes and
successful already-stopped handling. [Focused cleanup evidence](evidence-summary.md).
No guest execution or creation was repeated for this cleanup check. The original
full run did not log initial PASEO_HOST presence; the same session environment
checked before the focused run reports it unset. No variable value was printed.

## Local checks and limits

Fresh `make build` before registration, explicit host helper `go build` and
`go vet`, helper focused `go test -race`, and Linux arm64 probe `go vet` all exit 0. `git diff --check` is performed
before the evidence commit. The controller owns final full test/vet/build and
whole-change review; this report does not claim those checks.

No login, copied host authentication or authenticated model inference was
performed. Guest auth.json absence and empty API credential environment are
checked. This verifies actual Codex sandbox execution rather than model-triggered
tool execution. It does not establish general kernel security, other images or
versions. Expanded kernel access remains per-container for every process, as the
approved opt-in design explains. Preserve the disposable root and stopped exact
guest for review; do not silently recreate lost prior artifacts.

## Final-review correction: cleanup ownership

The whole-change review identified a P2 helper defect: assigning a resumed
registry candidate before checking the independently preserved CID could arm
deferred stop for a different valid managed guest. The corrected helper keeps
the candidate local and routes every cleanup enrollment through `ownedGuest`.
Preserved CID equality and full read-only policy inspection complete before the
cleanup target is published. The CID freezes at the first verified Service
creation result and cannot change through resumed `Remaining` entries or later
registry confirmation. Rejected later candidates retain the already verified
owned target; they never replace it with a foreign entry.

Focused fake-boundary regression reproduced the original assignment order as
RED: mismatched valid CID rejection recorded a foreign stop, and policy failure
also incorrectly armed stop. The corrected order is GREEN and records no
stop/start/exec/create for either rejection. Additional tests verify later
Service-result/registry mismatches and failed policy checks cannot change frozen
cleanup ownership, including failed first-time policy verification.

Fresh helper `go test -race`, `go vet`, build and diff check pass for this
correction. It changes only helper/test/report code. No real Docker, guest or
daemon action and no full live probe repetition were performed; the preserved
matching-CID live evidence above remains the historical acceptance record.

## Resumed review and regular verification

Independent scoped re-review confirmed the foreign-CID rejection but found that
post-cancellation enrollment used the expired operation context. Verification
now has its own bounded read-only deadline before cleanup enrollment. The new
cancellation regression failed with `context canceled` before the fix and passed
after it, including foreign-CID and policy rejection checks. `make test` and
`make vet` now explicitly include this helper and category-verify tests, which
Go's `./...` excludes. Fresh final results and unresolved acceptance are tracked
in [closure status](branch-closure.md). This does not repeat the live run or
establish authenticated model/tool acceptance.
