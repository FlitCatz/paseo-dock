# Task 1 — Codex user namespace policy model and Docker transport

Status: implemented and verified; independent controller review pending. No live Docker resource was created, started, stopped, or modified. All Docker transport checks used the Go test binary as a fake Docker CLI. No authentication/model request was attempted.

## Implementation

- Selection has `CodexUserNS bool` (`codex_userns,omitempty`); true requires schema 2, container category, and at least one codex pair. Mixed Codex/Claude selections remain valid. False preserves legacy JSON behavior.
- Entry has fixed policy ID/digest fields and both participate in EqualIdentity and registry confirmation. Service maps only Codex entries before prior-resource comparison. Enabling or disabling the policy on an existing saved resource is rejected before backend mutation; existing resources are preserved.
- `internal/containerpolicy` embeds the exact reviewed JSON. ID is `codex-userns-v1`; SHA256 is `de699bf56093ed501d6cf021db43b48f6bd5bd843a715404e7d0714ff0ed0af8`. Profile verifies integrity and returns a copy; MatchesJSON checks the complete decoded JSON with insignificant object ordering/whitespace differences accepted; WriteTemp creates a 0600 file and idempotent cleanup.
- Upstream pinned Moby commit `2ceae35d351c156cb5a8efc0fdc4a08cf94569d8`, baseline SHA `6416b47770785a41ac59073cdc77d9fe98517df2799dc83ef207e622de3053f6`, derivation, attribution and unmodified upstream Apache-2.0 LICENSE are included. The profile requires no external runtime path/download.
- New opted-in Docker creation validates image architecture arm64/amd64 before run and adds exact policy/digest labels, seccomp temp path, no-new-privileges, and SYS_ADMIN drop. No added capabilities or privileged mode. Unknown policy, forged digest, digest-only identity or non-Codex policy use is rejected.
- Inspect compares exact labels and complete profile JSON, requires no-new-privileges and explicit SYS_ADMIN drop, disallows CapAdd/Privileged and unrelated/duplicate security options. All existing exec/stop/start/readiness paths use this inspection before mutation. Policyless resources keep historical daemon-option compatibility; they cannot silently adopt resources with policy labels.
- Root bootstrap utilities and shell are absolute trusted image paths. Host UID/GID alignment and persistent agent sleep process remain. New policy metadata survives failed/canceled creation and recovery, Service registry failure bookkeeping, and exact-ID restarts.
- Runtime flag is cleared only in ImageTag hashing. Existing schema-2 BakeConfig/ImageSpec already uses installation inputs without serializing Selection; byte-for-byte bake comparison verifies equal manifests, arguments, labels and tags with opt-in toggled. Saved creation selections retain the true flag.

## Test evidence

Behavioral RED before production integration:

1. `go test ./internal/config ./internal/operations -run UserNS -count=1`: invalid opt-in formats accepted; policy-aware identities compared equal; mismatched observed policy reached stop. All intended failures were observed.
2. Docker RED: missing policy, label/digest/body mismatch, missing/duplicate no-new-privileges, unrelated option, CapAdd, privileged, missing SYS_ADMIN drop, unknown/forged policy and policyless adoption all allowed mutation; create lacked policy/security/drop/absolute bootstrap arguments; unsupported image architecture reached run. Failures were observed before Docker implementation.
3. Service RED: Codex policy metadata missing on successful/partial creation; existing-resource policy enable and disable both accepted. Failures were observed before reinstating Service mapping.
4. New package API test initially failed compilation because Profile/SHA256/MatchesJSON/WriteTemp did not exist. This is interface RED, not a behavioral failure. Package GREEN validates fixed digest, defensive copy, formatted JSON equivalence, invalid/trailing JSON rejection, private temp content/mode and cleanup.

One initial image regression fixture incorrectly used unsupported native pairs; corrected to existing superpowers pairs before GREEN. Image equality and preservation tests confirm existing behavior rather than requiring a new image implementation.

Focused GREEN covers policy mismatch denial with no subsequent stop/exec, exact create args/labels and actual temp file bytes read by the fake CLI, temp cleanup, unknown/non-Codex/digest-only policy rejection, arm64/amd64 acceptance and riscv64 rejection, legacy daemon-option compatibility, policy restart, failed/canceled run and canceled readiness retaining CID+policy metadata, mixed selection persistence and unchanged image build contents.

Commands (environment: `GOCACHE=/private/tmp/paseo-sbx-go-cache GOTMPDIR=/private/tmp`; race suite also uses `GORACE=atexit_sleep_ms=0`, removing only the fake Go CLI subprocess exit delay; race instrumentation remains enabled):

- `go test ./internal/config ./internal/containerpolicy ./internal/operations -run "UserNS|TestProfile" -count=1`: passed after integration.
- `go test -race ./internal/containerpolicy ./internal/config ./internal/operations`: passed.
- `make test` (`go test -race ./...`): all packages passed, including final added transport coverage.
- `go vet ./internal/containerpolicy ./internal/config ./internal/operations`: passed, no output.
- `git diff --check`: passed, no output.
- `shasum -a 256 internal/containerpolicy/codex-userns-v1.json`: exact expected digest above.

The first sandboxed focused race run failed existing localhost listener tests: `TestRestoreCategorySelection/container`, `TestDockerRelayUnreachableRefusesRegistration`, `TestDockerRelayCreateFailureKeepsCIDAndPriorActivation`, each due to `listen tcp4 127.0.0.1:0: bind: operation not permitted`. An escalated run allowed local listeners; focused and full suites passed. No auto-review rejection occurred.

## Remaining boundaries

CLI/TUI controls and documentation are Task 2. Fresh Service-created live guest acceptance is Task 3. This task claims fake-boundary transport/config verification only; architecture acceptance is not an amd64 live confinement claim. Authenticated inference and model-triggered tool execution remain deferred. The fixed policy expands kernel syscall access for all guest processes, as stated in the approved design/provenance.

## Task 3 compatibility fix — round 1

Task 3 live inspection found Docker Engine 29.4.0 normalizes the requested `--cap-drop SYS_ADMIN` to `HostConfig.CapDrop=["CAP_SYS_ADMIN"]`. Task 1 initially accepted only the CLI spelling, causing valid creation verification and subsequent cleanup verification to reject the product guest. The controller/live worker supplied the raw observation; this fix worker did not inspect or mutate the actual guest.

Changed only capability-drop recognition to accept exact `SYS_ADMIN` or exact `CAP_SYS_ADMIN`. No case folding, trimming or general prefix normalization. CapAdd/Privileged denial and policy ID/digest/profile/labels, architecture, mount and security-option checks are unchanged. Embedded profile SHA256 remains `de699bf56093ed501d6cf021db43b48f6bd5bd843a715404e7d0714ff0ed0af8`.

`TestUserNSDockerCapabilityDropAliases` exercises the Go fake CLI process boundary: both exact spellings pass Inspect, new Create verification/readiness, and Stop cleanup verification. Lowercase spellings, trailing/leading whitespace, duplicate CAP_ prefixes, extra suffixes and unrelated CAP_NET_ADMIN fail before Stop or Exec mutation. Existing creation options continue to request explicit SYS_ADMIN drop.

RED: `go test ./internal/operations -run TestUserNSDockerCapabilityDropAliases -count=1` failed only `/CAP_SYS_ADMIN` with `Docker security policy capabilities mismatch` before production modification. GREEN: same command passed after the two-alias change.

Fresh verification with `GOCACHE=/private/tmp/paseo-sbx-go-cache GOTMPDIR=/private/tmp`, normal GORACE defaults (no override):

- `go test -race ./internal/config ./internal/operations -run UserNS -count=1`: passed (config 1.017s, operations 88.223s).
- `go vet ./internal/config ./internal/operations`: passed, no output.
- `git diff --check`: passed, no output.
- Profile SHA256 check: unchanged exact expected digest.

No full-suite rerun was requested for this narrow compatibility fix; the original task evidence above records the prior full-suite run. The live worker must resume the same exact guest and verify actual Service flow/cleanup; this fix round claims fake-boundary verification only.
