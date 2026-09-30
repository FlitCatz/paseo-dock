# Execution ledger — plan: docs/superpowers/plans/2026-09-30-selection-tui.md

Ruling: The user approved the displayed pair-per-sandbox design and explicitly instructed work to start. Execute inline without another approval handoff. Host CLI remains Go; optional Headroom runtime is sandbox-only, as disclosed in the approved design.

Ruling: There is no Git repository or CodeGraph index in this workspace. Edit the supplied workspace directly; do not create a repository or fake commit ranges.

Pre-flight: Catalog.Normalize produces the shared selection used by CLI and wizard. CLI retains registry entries for preserved sandboxes; launcher consumes the registry's active list and captured catalog.

Task 1: complete — catalog selection tests failed for missing implementation, then passed. Pair compatibility, unknown IDs, duplicates and image invalidation covered.

Task 2: complete — selected bake/create/register behavior and legacy lifecycle suite pass. Explicit CLI selection or completed saved selection is required for non-TTY operations.

Task 3: complete — keyboard tests cover distinct per-agent choices, utility choices, confirmation and cancellation. Real PTY rendered the wizard and accepted selection; Ctrl+C cancelled without creating the workspace.

Task 4: complete — installer tests failed for missing implementation, then passed. Actual linux/arm64 Codex and Claude Superpowers images with RTK 0.50.0 and Headroom 0.39.1 built successfully. Headroom uses uv 0.12.21 and isolated Python 3.13.7 inside selected images.

Final review: fresh reviewer found three substantive issues. Each reproducer failed before its fix and passed afterward:

- Failed wrapper finalization replaced the active selection. Activation now occurs only after finalization and commits active profiles plus selection in one registry write; failed create preserves the old selection.
- Catalog removal stranded preserved sandboxes. Shell/stop now resolve the actual registry rather than rejecting the current catalog's missing ID.
- An incompatible selected utility could not be cleared. Deselection is allowed even when adding another agent makes that utility incompatible.

Ruling: Store the authoritative completed selection in registry.json alongside active profiles, with selection.json as an importable copy. This avoids disagreement between separate active-state and saved-selection commits after an interrupted finalization.

Verification: go test -race ./..., go vet ./..., make build, shell syntax checks and workflow YAML parsing passed. Final host CLI cross-builds for linux/amd64 and linux/arm64 passed.

Actual sbx verification: Loaded two selected utility templates, created exactly two sandboxes in a unique temporary workspace, and successfully initialized Headroom MCP and listed tools in both. Reducing the selection reused Codex, retained the Claude sandbox, generated only the Codex provider, and rejected the inactive Claude launcher. Test sandboxes, runtime and workspace were removed; all original 12 sandbox IDs were preserved. Production Paseo config was not changed.

RTK smoke: Claude and Codex native hook processors rewrote git status to rtk git status. The first Codex probe omitted the required hook_event_name and permission_mode fields; the pinned release source confirmed that incomplete payloads intentionally pass through. Retesting the complete official payload succeeded without a product code change.
