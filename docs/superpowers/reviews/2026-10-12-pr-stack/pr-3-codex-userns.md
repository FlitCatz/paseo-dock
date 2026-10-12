# PR 3: Add explicit Codex user namespace policy opt-in

Base: `review/runtime-categories`. Head: `review/codex-userns`.

New Codex Docker containers can select `--codex-userns` or the TUI confirmation
toggle. The embedded limited seccomp policy has a fixed ID/digest, is validated
before use, and applies no-new-privileges with SYS_ADMIN dropped. Saved choices
and explicit false are respected; existing containers are not converted.

This depends on PR 2's category identities and Docker backend. Changes correspond
to source `99a2b4e..7efadae`, including helper ownership/cancellation corrections,
regular helper tests and CI, and evidence cleanup. Product source matches the
original combined closure snapshot plus the isolated native dry-run fixture
correction carried from PR 2; original development history is preserved.

Validation: `make test` (race plus both helpers), `make vet`, `make build`
and diff check pass at `bede316`; see [stack status](README.md).
Historical Service-created confinement/restart checks passed on OrbStack Docker
29.4.0, Linux arm64 guests and Codex 0.159.2/0.161.0. amd64 live confinement and
AppArmor-enabled Linux hosts are untested. Authenticated inference and actual
model-triggered MCP calls remain deferred, so full product acceptance is incomplete.
