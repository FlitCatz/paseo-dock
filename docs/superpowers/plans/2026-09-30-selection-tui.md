# Selection TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Create and register only selected agent/methodology pairs, with optional sandbox utilities.

**Architecture:** A catalog validates a workspace selection. The CLI and Bubble Tea wizard share that selection, and bake/create/register use the same plan. Sandbox installers apply pinned utilities and agent integrations.

**Tech Stack:** Go 1.26, Charmbracelet Bubble Tea v2, Docker, sbx, Bash and Node for existing sandbox setup.

**Spec:** `docs/superpowers/specs/2026-09-30-selection-tui-design.md`

## Global Constraints

- One sandbox per selected agent/methodology pair.
- No implicit all-six creation; noninteractive execution needs explicit or saved selection.
- Utilities apply to all selected pairs; default none.
- Host management and launcher remain Go. Headroom's isolated runtime is sandbox-only.
- Preserve unrelated providers and existing sandboxes; reject incompatible image reuse.
- No Git repository exists here; edit the provided workspace without creating a repository or committing.
- User explicitly instructed implementation to start; execute inline without another approval handoff.

## Review Focus

- A partially failed create must preserve prior registry entries without making incomplete selections active.
- Deselecting a pair must remove only providers owned by this runtime, and deny stale delegation.
- Saved selection and explicit flags must never silently disagree about installed utilities.
- Non-TTY input and cancellation must never create all six sandboxes.
- Utility version changes must invalidate images, even when display profile names stay the same.

## Task 1: Catalog and selection

**Files:** `catalog.json`, `internal/config/catalog.go`, `internal/config/catalog_test.go`, `internal/config/config.go`, `versions.lock.json`.

**Interfaces:** `LoadCatalog(root string) (Catalog,error)`, `Catalog.Normalize(Selection) (Selection,error)`, `Selection.Profiles() []string`, `config.ImageTag(Lock,Selection) string`; registry stores optional active profile list and catalog, preserving legacy entries.

- [x] Write tests for two chosen pairs, unknown/unsupported pairs and utilities, duplicate normalization, changed utility version producing a different tag.
- [x] Run `go test ./internal/config` and observe missing implementation failure.
- [x] Implement catalog loading/validation, explicit adapters, deterministic selection and image fingerprints. Remove fixed base-image count requirement.
- [x] Run `go test ./internal/config`; expected PASS.

## Task 2: Selected CLI lifecycle

**Files:** `internal/cli/selection.go`, `internal/cli/cli.go`, `internal/cli/selection_test.go`, `internal/cli/cli_test.go`, `internal/launcher/launcher.go`.

**Interfaces:** App holds `Catalog` and `Selection`; CLI accepts `--profiles`, `--utilities`, `--selection`, `--workspace` for planning commands; `--profile` remains a single-pair shorthand. Workspace selection lives in trusted runtime, outside agent-writable workspace.

- [x] Write behavior tests for selected bake/create/register, omitted non-TTY selection, inactive provider pruning, unknown options and stale sandbox rejection.
- [x] Run `go test ./internal/cli`; expected FAIL on selected behavior.
- [x] Use one resolved selection for image build/load/create and active mapping; preserve old registry entries during partial writes. Restrict launcher allowlist to active entries and catalog-supported adapters.
- [x] Run `go test ./internal/cli ./internal/launcher`; expected PASS.

## Task 3: TUI

**Files:** `internal/tui/wizard.go`, `internal/tui/wizard_test.go`, `internal/cli/selection.go`, `go.mod`, `go.sum`.

**Interfaces:** `tui.Run(Catalog,Selection,workspace,memory string,in io.Reader,out io.Writer) (Result,error)`; Result contains selection and apply preference. Wizard is invoked by TTY create with no explicit flags or by `--interactive`.

- [x] Add failing tests for per-agent choice, utility toggles, validation, back, confirm and Ctrl+C cancellation.
- [x] Run `go test ./internal/tui`; expected FAIL.
- [x] Install pinned Bubble Tea v2, implement accessible keyboard steps and confirmation with actual pairs, optional utility dependency note, then CLI build/load/create workflow with progress messages.
- [x] Run `go test ./internal/tui ./internal/cli`; expected PASS. Exercise real PTY rendering and cancellation.

## Task 4: Utility installation and delivery

**Files:** `scripts/install-utilities.sh`, `scripts/install-tools.sh`, `Dockerfile`, `README.md`, `.github/workflows/templates.yml`, `docker-bake.json` as needed.

- [x] Add failing installer contract tests using fake commands, covering agent-specific RTK initialization and Headroom MCP registration, unsupported utility rejection and installer failures.
- [x] Run installer tests; expected FAIL.
- [x] Pin RTK release URLs/checksums, uv release/checksums, Headroom package version/runtime; install only selected utilities inside images. Configure official agent MCP integrations without proxy routing.
- [x] Run tests, shell syntax checks, `go test -race ./...`, `go vet ./...`, `make build`, then cross-build linux/amd64 and linux/arm64. Expected PASS.
- [x] Build selected utility image targets and smoke-check binary versions, utility hooks and MCP server initialization. If external downloads are blocked, preserve implementation and report exact unverified integration.
- [x] Document the wizard, saved selection, explicit examples, extension adapters and sandbox-only Headroom dependency. Obtain fresh code review and fix substantive findings with regression tests.
