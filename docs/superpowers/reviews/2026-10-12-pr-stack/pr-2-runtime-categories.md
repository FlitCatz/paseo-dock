# PR 2: Separate sandbox and Docker container runtime categories

Base: `review/management-tui`. Head: `review/runtime-categories`.

Selections and persisted entries distinguish agent-only sandbox runtimes from
Docker development containers. Operations, CLI/TUI and provider routing carry
category-aware frozen identities, enforce ownership and preserve partial failure
state. Native assets cross into the guest through constrained transport; relay
credentials and workspace boundaries remain checked.

This depends on PR 1's shared Service and dashboard. Category and native transport
stay together because they share identity/backend contracts. Changes correspond
to source `c7fe51a..99a2b4e`, plus applicable closure maintenance. The offline
category guard/parser helper is part of `make test`, `make vet` and CI, with an
embedded minimal protocol fixture instead of historical docs-log dependencies.

Validation: `make test` (race plus category helper), `make vet`, `make build`
and diff check pass at `9fe399f`; see [stack status](README.md).
Authenticated inference and model-driven MCP/native tool acceptance remain
unverified. Default Docker Codex workspace-write failed on the tested OrbStack
host; PR 3 adds an explicit opt-in rather than changing this PR's default policy.
