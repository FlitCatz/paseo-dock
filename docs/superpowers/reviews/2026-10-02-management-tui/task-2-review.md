# Task 2 review — 4e0e0d3..593ff83

## Spec Compliance

- ❌ Issues found: component confirmation provenance can be overwritten by utility IDs in `internal/operations/versions.go:136-151`, including a false confirmed methodology value when GSD has no current-version probe. See Important I1.
- ✅ The typed Snapshot/Query contracts and approved refinements are present (`internal/operations/snapshot.go:20-65`, `internal/operations/requests.go:38-45`). Production config changes are unnecessary under the approved private-discovery refinement.
- ✅ Core polling uses `sbx ls --json` and local reads, while optional subprocess connections require flags (`internal/operations/snapshot.go:175-179`, `257-300`); malformed or unrecognized sbx wrappers leave managed states unknown (`116-139`, `181-198`, `230-235`).
- ✅ Update and explicit-check records are separately matched by both sandbox and image (`internal/operations/snapshot.go:238-244`); GSD's ordinary path records an explanatory error instead of a lock version (`internal/operations/versions.go:104-113`).
- ⚠️ Cannot verify from diff: live adapter/plugin/utility command behavior and disposable sandbox start/cancellation behavior. The controller should retain the planned later live verification; fake subprocess tests establish argv boundaries rather than installed-binary behavior (`internal/operations/snapshot_test.go:26-47`, `internal/operations/versions_test.go:22-29`).

## Strengths

- `internal/operations/snapshot.go:87-106`: bounded stdout/stderr capture and per-command timeouts use the existing process-group cancellation helper without changing shared Service streams.
- `internal/operations/snapshot.go:204-218`, `304-331`, `337-386`: malformed local registries retain a visible stable runtime identity; provider registration checks enabled state, adapter, launcher and profile separately from activation.
- `internal/operations/versions.go:20-38`, `49-59`, `161-168`: targets are revalidated under their existing sandbox lock, warnings precede guest execution, and actual partial observations persist independently of updater attempts.
- `internal/operations/snapshot_test.go:88-107`, `111-178`, `183-217`; `internal/operations/versions_test.go:17-67`, `69-118`: tests exercise real filesystem parsing, joins, partial provenance and restart persistence while strictly constraining external commands.

## Issues

### Critical (Must Fix)

- None.

### Important (Should Fix)

- **I1 — `internal/operations/versions.go:136-151` (confirmation provenance / malformed boundary):** Utility manifest IDs become keys in the same maps used for reserved components (`agent`, `methodology`, and error categories). For example, `{"agent":"codex","utilities":{"agent":{"installer":"rtk"}}}` runs the supported RTK probe and overwrites the previously confirmed Codex version at line 58. A utility named `methodology` similarly supplies an RTK value after GSD was deliberately left unconfirmed at line 112; `Versions["methodology"]` and an unrelated methodology error then coexist. This is possible for malformed guest metadata and also for custom utility IDs because catalog validation only checks ID syntax/uniqueness within each category (`internal/config/catalog.go:58-68`, `120-135`). Give utility results a collision-free namespace or explicitly reject reserved IDs before recording any utility result; add regression coverage that reserved IDs cannot overwrite agent/methodology confirmations or errors.

### Minor (Nice to Have)

- None.

## Focused unchanged-code checks

- Named risk: child timeout/cancellation might leave subprocess descendants alive. Read `internal/process/process.go:11-17`; the existing helper creates a process group, kills that group on cancellation, and bounds post-cancellation waiting.
- Named risk: target revalidation or version-ledger persistence might bypass existing execution contracts. Read `internal/operations/service.go:86-92`, `140-182`; `writeJSON` preserves existing change tracking and `resolveTarget` checks workspace/entry identity. No changed production file was reread; these focused reads inspect unchanged functions outside the supplied hunks.
- Named risk: probe paths/custom identifiers might diverge from installed manifest contracts. Read `internal/sandboxupdate/update.go:170-245` and `internal/config/catalog.go:58-68`, `104-135`; plugin/utility shapes are consistent, and custom utility IDs expose I1. The existing updater's similar map-key behavior does not validate this newly added explicit-observation path.
- Named risk: snapshot per-profile image hashes might disagree with image creation for multiple selected pairs. Read `internal/operations/images.go:11-19` and `internal/operations/workspaces.go:158-163`; image creation also hashes one pair at a time, so the new comparison follows the existing contract.
- Supplied diff read in passes because the first tool output truncated the snapshot-test section. No suite, test, git command, source edit, index change, or branch change was performed. The implementer's reported final GREEN/vet/build evidence is present in `task-2-report.md`; it was not independently rerun.

## Assessment

**Task quality:** Needs fixes.

**Reasoning:** Snapshot boundaries and read-only polling are well separated and well covered, but explicit version observations currently allow utility identifiers to corrupt reserved component provenance. Resolve I1 before this API is trusted by later dashboard tasks.
