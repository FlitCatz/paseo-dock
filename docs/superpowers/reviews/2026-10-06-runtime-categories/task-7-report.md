# Runtime category verification — Task 7

Updated: 2026-10-12. Initial implementation baseline: `8c91652` on `feat/management-tui`.
The approved new-container Codex user namespace opt-in is implemented in the product and
passes actual Service-created guest confinement and exact-ID restart checks. CLI/TUI default
creation behavior and existing containers are unchanged. **Authenticated acceptance is not
complete:** the user has deferred guest login; actual model inference and model-driven MCP
tool execution remain unverified. The policyless Docker runtime still has its documented
bubblewrap namespace limitation; opt-in provides the explicitly selected compatibility path.
The discovered SBX activation-path defect has been repaired and verified on the same guest.
Independent whole-branch review and its four code fixes are recorded in
[whole-branch review](whole-branch-review.md). Current opt-in evidence is recorded below.

## Scope and identities

Initial live mutations used the authorized disposable namespace
`paseo-sbx-category-live-20261007` and existing root
`/private/tmp/paseo-sbx-category-live-1638524322`.
Workspace: that root's `workspace`; trusted runtime: `trusted`; isolated Paseo home: `paseo`;
registry: `home/.local/share/paseo-sbx/58213f234a/registry.json`.
The Go helper guards resource mutations using the actual isolated registry and exact pending CID.
It fails closed when a resume root is missing. No fabricated registry, host-home mount,
host Claude settings edit, default Paseo configuration edit or broad runtime policy change occurred.

| Resource | Exact identity / image |
|---|---|
| Docker Codex + Superpowers + RTK | CID `2ed04124ee17f7fd840f00695c0a84ba79438fa3b92a3fe16f8c2767bc418748` |
| Docker image | `sha256:7760a63ba6a62611c4434c1208c1c87236cd37d36b691da206ebd0d7e77d6521`; tag `container/codex-superpowers:2026.09.30-8e8751383862` under disposable namespace |
| SBX Codex | `paseo-sandbox-codex-58213f234a`; UUID `4448dc0f-a21f-4de1-959d-215a51953bff`; created `2026-10-08T00:36:27Z` |
| SBX Codex image | `sandbox/codex:2026.09.30-3b32788af33b` under disposable namespace; original Docker ID `sha256:b447638b02086a84a16a634bf9e10fda3ac8fcd923aecfd4d2f4f99510f227f6` |
| SBX Claude | `paseo-sandbox-claude-58213f234a`; tag `sandbox/claude:2026.09.30-f52fe6d46160` under disposable namespace |
| Fixed Claude image | `sha256:4d104746c41b18b406fb44867fbf8969cfcc4a31f3117059289ccd7d241e1be0` |

The older root `3716506909` vanished across interruption. Its `7b80a06c9b` resources were
not reconstructed, stopped or deleted during this continuation. Initial older-root success
did not prove Docker relay: its log showed an unknown container and no container provider.

## Fresh verification

### Approved product opt-in implementation — 2026-10-12

CLI `create --codex-userns` and the eligible TUI confirmation toggle enable the fixed
`codex-userns-v1` policy only for new Codex container entries. Default remains off; saved true
is preserved, explicit false overrides it, and inapplicable categories/selections are rejected
or cleared by the appropriate flow. Runtime choice does not change image tags/build contents.
Registry policy ID/digest are part of frozen identity. Docker inspect verifies policy labels,
full JSON, no-new-privileges, SYS_ADMIN drop, no CapAdd/Privileged and no unrelated options.
Existing resources cannot silently enable/downgrade the policy. Root bootstrap utilities are
absolute image paths. Backend and CLI/TUI independent reviews passed; the subsequent scoped
helper review and remaining acceptance gates are recorded in [closure status](branch-closure.md).

Actual Service-created new CID
`f7a8eceb0f7700c2a75e7487c403fcc7dbcb7245b0f27ff6d8e10d7c521ff116`, root
`/private/tmp/paseo-sbx-userns-live-117101514`, reuses the normally computed pinned image tag
for `sha256:7760a63ba6a62611c4434c1208c1c87236cd37d36b691da206ebd0d7e77d6521`.
Trusted recipe/current binaries, routing, HOME, PASEO_HOME and Docker config are staged privately.
No old vanished temporary registry was reconstructed, and no default project routing changed.

First create revealed Docker29.4 canonicalizes SYS_ADMIN to CAP_SYS_ADMIN. This caused both
creation and cleanup inspection to reject the equivalent capability name; exact CID/metadata
were retained. Regression RED reproduced; the fix accepts only the two exact names and rejects
near matches. Default-GORACE focused race tests pass and scoped independent review approves.
The exact owned guest was then stopped through corrected Service and resumed, not replaced.
[Initial failure](evidence-summary.md), [corrected cleanup](evidence-summary.md).

Actual Codex0.159.2 and exact guest-installed0.161.0 both pass workspace write and outside
home/symlink/protected/network denial, with CapEff0/NoNewPrivs1/Seccomp2. Baseline outside write
and network success establish meaningful negative probes. Unneeded administrative syscalls and
socket families remain denied. Forged policy/digest and existing-policy change requests reject
without registry/raw-state changes. Exact Service stop/resume preserves CID, policy and both
home/workspace markers, then0.161.0 confinement repeats successfully. Final guest running=false.
[Service live continuation](evidence-summary.md). No login or model request occurred.

The new isolated Paseo daemon was stopped via its verified private home, and its PID vanished.
The final helper cleanup function was separately exercised against a restarted same-home
daemon, with identity-rejection regressions; no guest probe was repeated for that check.
[Daemon cleanup verification](evidence-summary.md).

Settled `make test` with default global GORACE exits0 across all packages (CLI15.967s,
launcher41.354s, operations144.720s, TUI13.589s). `make vet`, `make build`, explicit helper vet
and diff check pass. Logs: [race](evidence-summary.md), [vet](evidence-summary.md),
[build](evidence-summary.md). Final independent review found no important product-path
defect, but requested a helper cleanup-enrollment correction on preserved CID mismatch; that
correction was committed at `69652ee`. The resumed review's cancellation correction is recorded
in [closure status](branch-closure.md). The matching-CID live evidence remains historical.

The actual platform is arm64/Linux Docker29.4.0. amd64 architecture acceptance has unit
coverage, but no amd64 live confinement claim.

### Continuation after whole-branch review

The latest production changes fix native asset ownership and native CLI category routing.
The launcher now sends a Go tar stream and extracts it with fixed `/usr/bin/tar` as the
guest agent, retaining private 0700/0600 permissions without root execution or Docker cp.
Anchored `os.OpenRoot` host reads, nofollow/inode checks, regular-file/directory-only entries,
128MiB size bounds and context cancellation preserve the source/transport boundary.
The pipeline propagates both extraction and writer errors; legacy SBX copy is retained.
The exact native resource, lock, registry and backend identity checks remain in place.
The CLI now resolves active native profiles in their category and rejects ambiguous choices.
The final independent read-only re-review found no further important issue.

An earlier ownership repair used root chown. Review identified PATH substitution (real UID0
RED, absolute-path GREEN), followed by intermediate staging-directory link handling that
`chown -hR` does not protect. The final agent-only extraction removes that root boundary.
Private modes, pinned-root rename/link replacement, cancellation, size and writer errors
have regressions; actual native schema and directory reads pass. See
[final archive verification](evidence-summary.md). Earlier PATH logs record the
review/correction history, not the final production command.

Latest `make test` (`go test -race ./...`) exits 0: CLI 9.344s and launcher 41.350s;
the unchanged package results are cached. `make vet`, `make build` and `git diff --check`
also exit 0. Logs: [race](evidence-summary.md),
[vet](evidence-summary.md), [build](evidence-summary.md).

Native acceptance uses only `/private/tmp/paseo-sbx-category-native-20261008`, its separate
Paseo home, and CID `10f4af99d91d6c1a4a28750a2f1f053e3caeb2242f8dcf033f8b2db986f765be`.
Actual plugin 0.9.2 / Paseo 0.11.1 registration and an idle agent session were created
(agent `599c4f27-f9fd-4d45-b202-d7d212d0ff5b`); this did not spawn a model turn.
Actual official native CLI 1.3.1 reads private generated file/directory assets and emits a
structured authentication-required result: zero turns and zero tokens. DNS/HTTPS and
nonroot workspace writes pass. No host authentication was copied. The exact container was
stopped and preserved. Evidence: [native runtime](evidence-summary.md),
[root PATH RED](evidence-summary.md), [root PATH GREEN](evidence-summary.md),
[final agent-only archive](evidence-summary.md).

`TestLiveCodexProviderMCPDiscovery` created its own Service-managed disposable root
`/private/tmp/paseo-sbx-mcp-live-2382036687` and CID
`c06840713ef58cd237370b0df74491bd690369601a9ac131335a267023052d8a` (image `7760a63ba6a6…`).
Actual image agent 0.159.2, with auto-update disabled, traversed Service → LaunchIO → guest
app-server → host HTTP relay and discovered synthetic `list_providers` after upstream
initialize and tools/list (one each). The opt-in live test exited 0 in 3.52s; exact guest
stop was verified. This is authenticated synthetic relay discovery, not a model call or
execution of a real Paseo MCP tool. Kit-generated ignored headers/type warnings remain
external to this injected http_headers configuration.

The user separately approved one new disposable container for a limited seccomp spike.
CID `f4e7b755a2f9b8ecb8f7423b1f5004f2e02b905cedfe3c76fec1ffac6edf5156` under
`/private/tmp/paseo-sbx-userns-live-3660932136` has no added capability/privileged mode,
no-new-privileges, bridge networking, no published ports and only its new workspace bind.
The agent has effective capability 0. The pinned Moby baseline and actual candidate SHA
are recorded in [experiment log](evidence-summary.md). Bubblewrap runs successfully.
Actual Codex 0.159.2 on the first pass and 0.161.0 after an exact guest-only installation
passed workspace writes, outside agent-home/symlink/protected-path denial and network denial.
The retained final log is a 0.161.0 rerun, since the persisted update is selected by codex.
Baseline outside-home write and network success make the denial probes meaningful.
AF_ALG/AF_VSOCK, bpf, perf_event_open, setns and unrelated namespace/mount requests remain
blocked. The sole container is stopped and preserved; it was reused, never replaced.

A build permission failure initially left the earlier experiment executable in place;
that revision-one executable created the sole container and used an obsolete Codex command.
The helper was corrected, rebuilt successfully with a writable cache, then rerun only with
the exact existing CID/profile. Actual policy retains two unneeded timestamp remount flag
families; the narrower source revision has not been run. This limits the result to the
recorded experimental policy. No product seccomp/default/capability/daemon/sysctl changes
were made. See [proposal and outcome](namespace-compatibility-proposal.md).

The user then approved one additional guest for audited minimal policy SHA256
`de699bf56093ed501d6cf021db43b48f6bd5bd843a715404e7d0714ff0ed0af8`. Exact CID
`b09beb001008c739bcc3e55ad5fdce95194e63cc7eab16328b88f64e94847c4c` under
`/private/tmp/paseo-sbx-userns-live-1919452979` passed the complete probe set on actual
Codex0.159.2 and exact-installed0.161.0, then stopped with an explicit running=false
assertion. This closes the minimal-policy spike gap without modifying product defaults or
any existing resource. [Minimal-policy live log](evidence-summary.md).
The user subsequently selected opt-in product integration for new Codex containers;
that implementation is now committed separately from the completed spike, as recorded above.

Guest direct login was requested by the user. Both device flows ended without
authentication; the user then said login is not currently possible. The exact original
Codex guest is confirmed stopped. Secrets are neither printed nor copied. A no-tool
actual-model probe is prepared, but has not been run; authentication-dependent gates are
deferred rather than counted as green.

### Earlier Task 7 verification (preserved evidence)

Commands used `GOCACHE=/private/tmp/paseo-sbx-go-cache GOTMPDIR=/private/tmp` with
local socket access. Final runs followed completed live activity.

| Check | Result |
|---|---|
| `make test` (`go test -race ./...`) | final after PATH fix exit 0; CLI 9.398s, launcher 41.254s, operations 57.440s, TUI 13.258s |
| `make vet` | exit 0 |
| `make build` | exit 0; all three Go binaries |
| Go acceptance-helper build | exit 0 after final diagnostics changes |
| PTY shell restoration | race suite passes for legacy and new agent-only category; q and Ctrl+C cases |
| Claude nested-kit mount regression | exact old image RED; exact rebuilt image GREEN |
| SBX activated-agent regression | focused race exit 0; provider/direct exec select update first and image fallback |

An earlier full suite failed only `TestShellExecProcessCallback`'s existing 3-second deadline
during concurrent live activity. Its focused race run repeated five times passed; the final
settled suite passed without timeout inflation or a production change. The opt-in Docker
regression skips in the normal suite and was also run explicitly as described below.

Logs: [race suite](evidence-summary.md), [vet](evidence-summary.md),
[build](evidence-summary.md), [actual Claude lifecycle](evidence-summary.md),
[provider proof before PATH fix](evidence-summary.md),
[provider proof after PATH fix](evidence-summary.md),
[final Codex diagnostics](evidence-summary.md).

## Observed live gates

The prior implementer's verified results below were preserved through the restart handoff
and checked against the surviving registry/update records. The earlier current-root full
transcript was not retained on disk; these are historical observations, not fresh reruns.

| Historical gate in root 1638524322 | Observed evidence |
|---|---|
| Pinned Docker stack | Codex 0.159.2; Superpowers `8ca22dba9a94f28898bbce59f2537ff4d87c747d`; RTK 0.50.0 |
| Network | DNS lookup of registry.npmjs.org; HTTPS HTTP 200 |
| Workspace write / ownership | agent UID:GID 501:20 matched host; nonroot guest created workspace file successfully |
| Explicit publication | Go Linux HTTP probe returned `CATEGORY_PORT_OK` at host `127.0.0.1:49816` → guest 18765 |
| Restart persistence | exact same CID restarted; guest home and workspace markers survived |
| Docker shell / update | PTY marker; full stack update produced Codex 0.161.0 and RTK 0.51.0; methodology revision unchanged |
| Codex agent-only contents | image binary 0.160.1 and official source manifest; no RTK/Headroom/OpenSpec or managed methodology paths |
| Codex shell / update | PTY marker and update stage success; activated binary 0.161.0 (provider PATH discrepancy below) |
| Same workspace | independent category rows; Docker and Codex running and active together |
| Relay / stop | real Docker reachability challenge and mixed registration; exact stop preserved both, known=true/running=false |

Fresh continuation proves Claude create, contents, versions, PTY shell, agent-only update,
provider `--version` route and exact stop. Actual guest version is **2.1.293**; `agent.json`
and `installed.json` record `https://registry.npmjs.org/@anthropic-ai%2Fclaude-code/2.1.293`.
Managed utility binaries and methodology paths were absent. `/home/agent/.claude` is
1000:1000 and kit-created `settings.json` is writable by agent. Update completes at 2.1.293.

Fresh mixed registration succeeds only after explicitly starting and inspecting the preserved
Docker CID. The real bridge challenge passes and the isolated config contains
`container-codex-superpowers`. Docker provider `--version` returns 0.161.0; SBX Codex returns
0.160.1 before the fix and 0.161.0 after the fix. Each actual `app-server` receives initialize request ID 1, returns a result identifying
Linux and `/home/agent/.codex`, then exits 0 after the helper closes stdin on that response.
This proves runtime routing, protocol transport and EOF termination, not login/model inference
or a completed MCP tool request through the model.

The first resumed registration intentionally encountered a stopped Docker resource and refused
registration, preserving prior config. The harness was corrected to establish running state
before registration. Claude creation replaces active sandbox selection, leaving Codex inactive;
the helper reactivates the exact existing Codex via normal service creation before dual-provider
verification. No registry was edited by hand. Final Docker, Claude and Codex guests are stopped
with preserved identities; final active registrations are Docker + SBX Codex. The isolated
Paseo daemon and temporary root remain preserved for inspection, not globally installed.

## Claude kit ownership fix

Scoped SBX daemon evidence localized the original failure to the Claude kit installer writing
`/home/agent/.claude/settings.json`: PermissionDenied. Nested kit plugin mounts created the
missing parent directory as root. SBX auto-deleted the failed target; teardown HTTP 404 was
secondary, not the root cause.

`Dockerfile.sandbox` now precreates `.claude` only for Claude before the existing home chown.
This adds no methodology or utility and changes no host settings. Opt-in Go test
`TestClaudeSandboxConfigSurvivesKitMount` runs an exact, labeled Claude image as agent with
a readonly mount under `.claude/plugins/kit-probe` and verifies parent UID and settings write.
Against old image `sha256:b248a3aa71d40ac8cd021d581668a9a1ac6b5b2e7434060258b2444c621930c8`
(2.1.292), it fails exit 1. Against fixed image `4d104746…` (2.1.293), it passes exit 0.
Actual SBX create/kit write also passes, so the image probe is not the sole compatibility proof.

## Load-bearing limits and unresolved gates

**Docker Codex workspace-write execution is blocked.** In exact inspected CID above,
installed Codex 0.161.0's supported command syntax is `codex sandbox [OPTIONS] [COMMAND]`.
`codex sandbox -c 'sandbox_mode="workspace-write"' -- bash -lc 'printf CATEGORY_CODEX_SANDBOX_OK > category-codex-sandbox; cat category-codex-sandbox'`
exits **1** before executing the command: `bwrap: No permissions to create a new namespace`.
The initial probe used older `sandbox linux` syntax; the final diagnostic corrected it and
reproduced the same restriction. Provider initialize itself still exits 0 and emits a warning.

Installed features list marks `use_legacy_landlock` deprecated. An invocation-only attempt
with `--enable use_legacy_landlock` and the same workspace-write policy exits **101**:
`filesystem-restricted execution requires bubblewrap to isolate app-server sockets`.
The workspace write and outside-workspace denial markers therefore never execute. This matches
the [official pinned 0.161.0 Linux sandbox source documentation](https://github.com/openai/codex/blob/rust-v0.161.0/codex-rs/linux-sandbox/README.md).
No safe compatibility configuration was verified. No privileged/unconfined Docker mode,
capability/AppArmor/seccomp change, persistent feature change or disabled-sandbox fallback
was applied. Runtime user-namespace support or a separately reviewed compatible agent release
would need fresh confinement verification before this gate can pass.

Read-only inspection of that CID found kernel `7.0.14-orbstack-00380-ga7e0a2dc9535`,
`/proc/sys/user/max_user_namespaces=48184`, `Seccomp: 2`, `Seccomp_filters: 1`, and system
`/usr/bin/bwrap` package `0.11.1-1ubuntu0.1`. These establish a configured namespace limit and
installed helper, but do not identify which seccomp/AppArmor/kernel rule rejects namespace
creation. The [official sandbox prerequisites](https://learn.chatgpt.com/docs/sandboxing)
also require user-namespace access. No global sysctl or daemon policy was changed.

**SBX activation path defect — repaired.** The Codex sandbox UUID and creation time
remain unchanged. The update ledger at `2026-10-08T09:36:48.50306+09:00` reports 0.161.0;
`/home/agent/.local/bin/codex` still links to the installed 0.161.0 staging tree. `bash -lc`
prepends `.local/bin` and returns 0.161.0. Direct `sbx exec ... printenv PATH` starts with
`/opt/paseo-sbx/runtime/bin` and excludes `.local/bin`; direct `codex --version` and launcher
both return image version 0.160.1. This is a reproducible execution-path mismatch, not a
recreate, rollback or lost update file. The image manifest describes build source, not the
activated update.

Authorized narrow fix: `internal/config/category.go` defines a fixed guest-only PATH with
`/home/agent/.local/bin` first. `internal/operations/sbx_backend.go`, both the version shortcut
and main session in `internal/launcher/launcher.go`, and the SBX updater transport in
`internal/sandboxupdate/update.go` pass this PATH for the new sandbox category. It is supplied
as an argv environment option, not interpolated into a shell or derived from host PATH.
Legacy and Docker image paths remain as previously implemented.

`TestSandboxProviderSelectsActivatedAgentBeforeImage` in `internal/launcher/category_test.go`
observed RED separately for the original provider and direct backend (selected image-0.160.1
instead of updated-0.161.0). GREEN covers direct exec, provider version shortcut, normal
provider session path, image fallback when no update exists, and no host fixture path in argv.
Actual same-UUID SBX provider now returns 0.161.0 and its app-server initialize result also
identifies 0.161.0; initialize/EOF exits 0, followed by exact stop. The persisted update symlink
and image binary remain 0.161.0 and 0.160.1 respectively. No image rebuild or recreate was
used to fix this installed resource.

SBX app-server additionally warns that kit-generated MCP gateway `headers` and `type`
settings are ignored. The warning did not prevent initialize/EOF, but end-to-end MCP tool use
was not tested. Actual model calls/login and native Antigravity runtime inheritance remain
unverified. No new authentication setup or host credential modification was attempted.

Pre-build sandbox confirmation shows official source and an unresolved version until build;
extremely short terminal heights may clip final confirmation. These previously reviewed UX
limits remain disclosed. No all-agent/all-methodology live build matrix was performed.

Tasks 1–6 were independently reviewed, including their correction rounds. This report records
Task 7 self-review and evidence; it does not substitute for the final independent branch review
or resolve the Docker sandbox execution gap above.

## Task 7 review correction round 1

Independent review found two Important helper defects: an owned identity anywhere in argv
could authorize a foreign mutation, and an initialize ID substring could authorize an error
or echoed request. Both are corrected in `category-verify.go`; no production file changed in
this correction round.

The proxy now maintains separate SBX and Docker ownership sets. Exec parsing accepts only
the supported options with their required values and validates the actual resource position
before the guest command. Unsupported/malformed options fail closed. Docker start/stop
require every positional target to be owned; SBX stop accepts only one owned target. SBX
policy accepts only the exact supported allow/remove network forms with the owned sandbox
at `--sandbox` and a valid localhost port. An owned ID in an environment or guest-command
argument cannot authorize a different target.

Initialize processing uses `encoding/json` on complete newline frames, including frames
split across writes. A response must have numeric ID 1, a nonnull result and no error field.
Matching errors terminate the helper with failure. Partial lines, echoed requests, nested IDs,
other/string IDs, null results, error+result and no response cannot pass. Both the provider
session child and its parent transcript gate use the same structured predicate.

New Go tests in `internal/tui/testdata/management-tui-live/category-verify_test.go` invoke
actual helper/proxy subprocesses with trusted temporary registries and fake Go runtime
executables. They assert that rejected foreign/mixed/malformed requests never reach the fake
external executable, while exact owned requests do. Provider-session subprocesses reproduce
JSON-RPC error/no-response exit-0 false positives and exercise valid/whitespace responses;
chunk tests cover partial frames and all result predicates.

RED: the original guard reached the fake executable for foreign Docker/SBX targets,
mixed start/stop, unknown/missing option values and cross-runtime IDs. The original session
returned success for error and absent responses, and did not terminate on valid JSON spacing.
GREEN: the focused command below exits 0 (`ok command-line-arguments 0.180s`). Helper compile
also exits 0. Both retained real Docker and SBX initialize result frames pass the new parser,
so a new external-resource launch was unnecessary; no real runtime mutation occurred during
this correction. The previous complete race/vet/build checks remain unchanged.

```text
GOCACHE=/private/tmp/paseo-sbx-go-cache GOTMPDIR=/private/tmp go test \
  internal/tui/testdata/management-tui-live/category-verify.go \
  internal/tui/testdata/management-tui-live/category-verify_test.go -count=1
GOCACHE=/private/tmp/paseo-sbx-go-cache GOTMPDIR=/private/tmp go build \
  -o /private/tmp/category-verify internal/tui/testdata/management-tui-live/category-verify.go
```

Use explicit Go files because this testdata directory also contains separate standalone
programs. Evidence: [guard/parser RED](evidence-summary.md),
[provider-session RED](evidence-summary.md), [focused GREEN](evidence-summary.md).
The subsequent category whole-branch review and corrections are recorded in
[whole-branch review](whole-branch-review.md). The resumed userns helper review,
normal helper test targets and remaining product acceptance gates are recorded
in [closure status](branch-closure.md). Authenticated inference/model-driven MCP,
amd64 live confinement and AppArmor-host acceptance remain incomplete.
