# Codex user namespace opt-in Implementation Plan

**Current integration:** Implemented and locally merged into main through the
reviewed stack. See [latest integration status](../reviews/2026-10-12-pr-stack/README.md).
The closure checkpoint and step lists below remain historical; authenticated
model/tool and additional-platform acceptance are still incomplete.

**Closure status (2026-10-12):** Policy, CLI/TUI and isolated Service confinement
implementation are committed through `69652ee`. The resumed scoped review and
helper maintenance are recorded in [closure status](../reviews/2026-10-06-runtime-categories/branch-closure.md).
The original step checklist below is retained as the historical plan; it does not
assert that every broader acceptance gate is closed. Feature work is frozen.
Authenticated inference/model-driven MCP, amd64 live confinement and AppArmor
host acceptance remain unverified. Do not interpret implementation as full acceptance.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to explicitly enable the verified limited seccomp policy on new Docker Codex containers, with persisted identity and fail-closed execution checks.

**Architecture:** Bundle the exact reviewed profile into a small Go package. A schema-2 selection flag maps only Codex entries to a fixed policy ID/digest; Docker creation applies it and subsequent inspect verifies the observed policy. CLI/TUI expose this creation choice without modifying any existing resource.

**Tech Stack:** Go, standard library embed/JSON, existing Docker CLI and Bubble Tea.

**Spec:** [approved design and live evidence](../reviews/2026-10-06-runtime-categories/namespace-compatibility-proposal.md).

## Global Constraints

- User explicitly selected opt-in implementation. No repeated design/execution approval.
- New runtime logic and tests use Go; no new runtime dependency.
- Existing containers, default creation settings and Docker daemon are not changed.
- Exact profile SHA256 is `de699bf56093ed501d6cf021db43b48f6bd5bd843a715404e7d0714ff0ed0af8`; ID is `codex-userns-v1`.
- Default-deny profile, no added capabilities/privileged mode, no-new-privileges enabled. Explicitly drop SYS_ADMIN on opted-in resources so daemon defaults cannot activate the baseline's SYS_ADMIN rule group.
- Opt-in requires schema2/container with at least one `codex` pair; apply only to entries whose Agent is `codex`. Other selected agents retain their default policy.
- No authenticated model test: user cannot login now. Record that gate as deferred.
- Remain in the current management-tui worktree; do not merge/push/delete it.

## Review Focus

- Saved/interactive selections retain explicit true/false correctly; switching away from Codex clears inapplicable choice (Task2 tests).
- Policy mismatch, extra capability/privileged/security option and forged digest block mutation (Task1 tests).
- Existing default-policy resources cannot be silently adopted as opt-in, and opt-in cannot silently downgrade (Task1 tests).
- Runtime opt-in does not change image tags/build contents (Task1 image-tag tests).
- Failed/canceled creation retains exact CID and policy metadata; restarted guest retains policy (Task1 regression and Task3 live check).

### Task 1: Policy model and Docker transport

**Files:** Create `internal/containerpolicy/policy.go`, `codex-userns-v1.json`, provenance/license and tests; modify `internal/config/catalog.go`, `config.go`, `category.go` and focused tests; modify `internal/operations/docker_backend.go`, `workspaces.go` and focused policy tests.

**Interfaces:**

- `config.Selection.CodexUserNS bool` with JSON `codex_userns,omitempty`.
- `config.Entry.SecurityPolicy string` / `SecurityPolicyDigest string`, JSON `security_policy,omitempty` / `security_policy_digest,omitempty`.
- `containerpolicy.ID` / `SHA256` string constants. `Profile() ([]byte,error)` verifies embedded digest and returns a copy. `MatchesJSON([]byte) bool` compares the observed JSON semantics against the fixed profile. `WriteTemp() (string,func(),error)` creates a private temporary policy and cleanup.

- [ ] Write RED regressions for normalization, policy-aware identity, unchanged image tag, create options/metadata, inspection rejection, unknown policy, default compatibility and existing resource mismatch.
- [ ] Observe RED before implementation with focused Go tests; assert actual behavior rather than copied predicates.
- [ ] Embed exactly `/private/tmp/paseo-sbx-minimal-seccomp.json` with SHA/provenance and upstream Apache-2.0 attribution. No caller-supplied profile/path.
- [ ] Validate option format and preserve JSON backward compatibility; omit the runtime-only boolean when hashing an image.
- [ ] Map opted-in Codex entries to fixed policy metadata before prior-entry equality checks. Other entries stay unchanged.
- [ ] For new opted-in Create, add policy/digest labels, seccomp temp file, no-new-privileges and SYS_ADMIN drop; no capability additions. Support image architectures arm64/amd64 only, rejecting unsupported opted-in image architectures before creation.
- [ ] Inspect opted-in resources checks exact policy/digest labels, matching profile JSON, no-new-privileges, SYS_ADMIN drop, no CapAdd/Privileged and no unrelated security options. Existing policyless inspect remains compatible.
- [ ] Pin all root bootstrap utilities to root-owned absolute image paths. Keep host UID/GID alignment and long-lived agent process.
- [ ] Run `go test -race ./internal/containerpolicy ./internal/config ./internal/operations`, focused vet, and diff check. Report commands/evidence and commit only task files.
- [ ] Independent spec/quality review; resolve important findings before Task2.

### Task 2: CLI and TUI choice

**Files:** Modify `internal/cli/cli.go`, selection helpers/help tests and new `userns_test.go`; narrowly extend `internal/operations/selection.go` and its selection tests; modify `internal/tui/wizard.go`, relevant create confirmation and new `userns_test.go`; update relevant README usage.

**Interfaces:** Consume Task1's Selection boolean and validation. CLI flag is `--codex-userns` (explicit false supported). `operations.SelectionOptions.CodexUserNS *bool` applies an explicit override before Normalize (nil preserves saved/file value). TUI model keeps `CodexUserNS bool`, selection/result propagation and an eligible confirmation toggle `u`.

- [ ] Write RED CLI tests for create propagation, explicit false override of saved true, invalid command/category/agent and interactive seed handling.
- [ ] Write RED TUI tests for default off, saved true, confirmation toggle/result propagation and category/agent switch clearing invalid state.
- [ ] Add creation-only flag. Apply explicit flag values to saved/file/interactive selection before normalization; no visited flag means preserve the saved selection.
- [ ] Expose toggle only for container selections containing Codex; indicate workspace-write isolation and expanded kernel syscall access for all guest processes in confirmation. Retain final selection and ports behavior.
- [ ] Show selected policy in noninteractive create confirmation/diagnostics where the existing flow displays options. Avoid silently enabling it.
- [ ] Document example `create --category container --profile codex-superpowers --codex-userns`, unchanged defaults, no existing-container conversion and supported/tested platforms boundary.
- [ ] Run CLI/TUI focused RED→GREEN and package race tests, vet, diff check; report and commit task files.
- [ ] Independent spec/quality review; resolve important findings before Task3.

### Task 3: Service live acceptance and final review

**Files:** Create a Go opt-in helper/test under `internal/operations/testdata/userns-acceptance`; update Task7 report/ledger and policy proposal with concrete final evidence.

**Interfaces:** Consume public `operations.Service.Execute`, normalized Selection and actual registry Entry. Use exact backend inspect/exec/stop with operation locks.

- [ ] Create a fresh unique disposable root/home/workspace and trusted recipe copy, with no auth/home/socket mounts. Use the already built exact image through normal Service creation and the new true Selection flag.
- [ ] Verify actual saved metadata/selection and raw Docker policy, then actual Codex workspace-write probe from the built Go namespace-probe. Confirm outside/symlink/protected/network denial and zero CapEff/NoNewPrivs1/Seccomp2.
- [ ] Confirm forbidden administrative syscalls/socket families; install exact guest Codex0.161.0 if necessary, without copying credentials, and repeat its sandbox probe.
- [ ] Stop/recreate-through-Service the same exact entry to prove restart, policy and home/workspace persistence. Attempt mismatched policy/digest through the operation layer and require rejection without target mutation.
- [ ] Stop exact guest with independent cleanup deadline, fail on cleanup errors, preserve identity/logs and verify running=false. No authenticated inference claim.
- [ ] Run final `make test`, `make vet`, `make build`, explicit helper build/vet and diff check once final code settles.
- [ ] Independent whole-change review including category integration and deferred gates, then commit evidence. Retain worktree and report remaining auth/model acceptance clearly.

## Authorization and execution

The user approved the opt-in design and instructed implementation by selecting the implementation option. Execute continuously using fresh task workers and independent review. No extra approval is required for the normal isolated acceptance guest needed to verify this authorized implementation.
