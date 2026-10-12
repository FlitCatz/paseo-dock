# PR 1: Add state-driven sandbox management TUI

Base: `main`. Head: `review/management-tui`.

Users can view registered workspaces and sandbox state, run management jobs, and
review scoped confirmations from the terminal dashboard. Shared operations retain
locks, context cancellation and exact target validation while keeping CLI access.
This PR contains the TUI work through source revision `c7fe51a` and its applicable
maintenance. Runtime categories and userns policy belong to the following PRs.

Raw verification logs are replaced with reports and hash inventory. CI uses the
same Make test/vet commands as local verification. The launcher cwd fixture is
isolated from inherited Paseo session environment.

Validation: `make test` (race), `make vet`, `make build` and diff check pass at
`d1cbd99`; see [stack status](README.md).
The prior live creation lifecycle hit manifest HTTP 403; full live lifecycle
acceptance is still unverified. No authenticated model/MCP acceptance is claimed.
