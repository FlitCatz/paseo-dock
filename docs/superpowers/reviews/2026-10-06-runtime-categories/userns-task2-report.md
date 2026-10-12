# Codex user namespace opt-in: CLI/TUI implementation

2026-10-12, Task 2 on `feat/management-tui`, based on Task 1 commit `a1b5f9c`.

`create --codex-userns` opts new Codex containers into the fixed `codex-userns-v1` policy.
Omission preserves saved/file choices; explicit `--codex-userns=false` clears saved true.
The flag is creation-only. Standalone CLI wizard seeds honor it; users can change that seed.
Both standalone and embedded wizard confirmations offer `u` only for container selections
containing Codex. The choice survives normalized/frozen results and ports propagation, and
category changes or removal of Codex clear it. Mixed selections apply the policy only to Codex.

Confirmation/diagnostics explain workspace-write isolation and added kernel syscall access
for **all guest processes** in each opted-in Codex container, without privileged mode or
added capabilities. README records unchanged off defaults, no existing-container conversion,
supported Linux arm64/amd64 images, and the narrower tested OrbStack/arm64/version boundary.

The controller authorized one resolver bridge: `SelectionOptions.CodexUserNS *bool` applies
explicit overrides before normalization, nil preserves behavior, and explicit creation choices
promote historical pairs to schema-2/container as existing CLI creation already did.
JSON/Registry format validation remains authoritative; false cannot bypass invalid decoding.
No backend, configuration model, or seccomp policy implementation changed in Task 2.

Focused CLI/TUI/resolver tests passed after observed RED failures. PTY tests cover saved true,
explicit false, explicit-profile and fresh seeds, plus manual disable. CLI and TUI package race
tests, scoped resolver race tests, vet for the three affected packages, and diff checks passed.
The first sandboxed CLI race attempt was blocked by an existing local `httptest` listener;
the approved rerun with loopback listening enabled passed. No global GORACE override was used.

Full repository verification, independent spec/quality review and product-created guest
acceptance remain controller follow-up. No live Docker, model authentication or inference
was performed during this task.
