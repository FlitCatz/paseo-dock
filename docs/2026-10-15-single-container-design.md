# One container per `create` (multi-agent container)

Status: design, awaiting approval. Category `container` only; `sandbox` (agent-only SBX) and
legacy schema-1 SBX are unchanged.

## Today

`create` normalizes the selection to `pairs` (agent × methodology). `createCategory` makes one
registry entry, one image (`namespace/container/<agent>-<tool>`) and one Docker container per
pair. Each entry has one provider, `container-<agent>-<tool>`. Antigravity entries use
`extends: "antigravity"`. PR #1's base probe always skips that base, so container Antigravity
never gets a Paseo provider.

## Selection → one image, one container

- The selection format stays the same: `pairs` from the wizard, `--agents`/`--methodologies`,
  `--profiles`, or `pairs` in the create config. Two optional fields are added:
  - `name`: the container name. It must match `^[a-z][a-z0-9-]{0,47}$`.
  - `antigravity_connection`: `none` or `acp`.
- **One pair** keeps today's path exactly: the same entry shape, key `container/<agent>-<tool>`,
  image tag, bake target and provider ID. Existing single-agent containers and their
  registrations still match and are reused.
- **Two or more pairs** create one *bundle* entry:
  - The key is `container/<name>`. The default name is the sorted distinct agents followed by
    the sorted methodologies, for example `codex-claude-superpowers-ponytail`.
  - `config.Entry` gains `Agents []string` and `Tools []string` (normalized and sorted).
    `Agent` and `Tool` stay empty.
  - `EqualIdentity` covers the new fields. Re-running the same selection reuses the container.
    A different selection with the same name is refused, and the existing container is kept,
    as today.
  - Running `create` again with a different selection or name makes another container. That is
    how the captain gets separate containers.
- **Image**:
  - One image per bundle: `namespace/container-<name>:<tag>`.
  - `ImageSpec` hashes the category, the sorted pairs, the utilities and the lock with recipe
    `bundle-v1`. The single-pair recipe is unchanged.
- **Dockerfile**:
  - `AGENT`/`TOOL`/`METHOD_INSTALLER` become comma lists: `AGENTS` and `PAIRS` (`agent:tool:installer`).
  - The agent install step loops over `AGENTS`. Single-pair builds pass one-element lists, so
    they produce the same image.
  - `BASE_IMAGE` is the `base_images` entry of the first selected agent in catalog order
    (codex, claude, antigravity-cli). Claude is installed through npm in either case.
  - `agent.json` becomes `agents/<agent>.json` (one manifest per agent). The update path reads
    the manifest of the agent being launched.
- **`install-tools.sh`** changes so it can run once per pair:
  - The marketplace JSON appends plugins instead of overwriting them.
  - Agent instructions are appended once per agent.
  - The GSD `~/.gemini/antigravity` symlink is created only if missing.
  - `installed.json` becomes a list.
- **`install-utilities.sh`** runs once per agent.
- **OpenSpec**: one `openspec init --tools <all selected>` in the container.
- **CI and bake**:
  - The matrix keeps every catalog pair as a single-pair target: `container-<agent>-<tool>`,
    unchanged.
  - One more job builds an "all agents × all supported methodologies" bundle
    (`build --profiles <all> --name ci-all` → target `container-ci-all`) to catch install
    conflicts.
  - The checked-in `docker-bake.json` keeps its single-pair targets.

## Paseo provider registration

- A bundle registers **one provider per agent**. The ID is `container-<name>-<agent>`, for
  example `container-codex-claude-superpowers-codex`, and the label is `SBX <name> · <Agent label>`.
- The command is `[launcher, id]`, or `[launcher, id, "--acp"]` for Antigravity over ACP.
- These IDs never collide with single-pair IDs (`container-<agent>-<tool>`) because agent IDs
  and methodology IDs are disjoint. The launcher's `providerEntry` maps an ID to its
  `(key, entry, agent)` by exact match, as it does now.
- `extends`:
  - Codex → `codex`.
  - Claude → `claude`.
  - Antigravity → `acp` only when `antigravity_connection=acp`. When it is `none`, no
    provider is emitted.
  - Single-pair Antigravity entries are treated as `none`. They stop emitting the
    always-skipped `extends: "antigravity"`, which removes a warning and nothing else.
- PR #1's guarantees stay:
  - Every base still goes through `filterProviderBases` (an unsupported base is skipped with a
    warning).
  - The full config is still validated, with rollback on failure.
  - `MergeManagedConfig` ownership stays the same. Its native-ID test gains the
    `-antigravity-cli` suffix form, so it can remove its own stale `--acp` entries.

## Antigravity connection option

`antigravity_connection` is accepted only when the selection includes `antigravity-cli`. It is
offered in the wizard, as `--antigravity-connection`, and in the create config. It is stored on
the entry and is part of its identity, so changing it means a new `create` with another name.

| value | effect |
| --- | --- |
| `none` (default) | `agy` is installed. There is no Paseo provider. It is usable from the shell (`s`). |
| `acp` | Registers `extends: "acp"`, command `agent-wrapper <id> --acp`. The launcher runs `agy --acp` in the container (it already appends `--acp`). |

Risk: whether `agy` 1.2.14 speaks ACP is unverified, and the evidence points to no:

- `internal/runtimeinstall` says it "does not install an ACP server".
- The 2026-10-01 implementation log used a *separate* official ACP server artifact (1.2.1) that
  aborted on arm64 because of 16 KB pages and needed QEMU. That artifact was removed.

Checking this needs a network download of the pinned `agy` artifact (or a built image), which I
did not do. To keep `acp` safe, create checks that the container's `agy` answers an ACP
`initialize` (stdio, 10 s timeout) before it activates the Antigravity provider. If it does not,
the Antigravity provider stays inactive with a clear error. The Codex and Claude providers in the
same bundle still register.

## Auth and config inside one container

Everything stays in container-local `/home/agent`, and host auth is never shared:

| agent | auth and config |
| --- | --- |
| Codex | `~/.codex` |
| Claude | `~/.claude`, `~/.claude.json` |
| Antigravity | `~/.gemini` (`antigravity-cli`) |

These paths are disjoint, so the logins coexist. You log in once per agent through the
dashboard shell (`s`).

## Two agents at once

- Each launch is its own `docker exec` in the running container.
- The launcher's startup lock (`operationlock` key `container:<id>`) is non-blocking. Today a
  second launch starting during the first one's short startup window fails with "operation
  already running". It will wait instead: retry for up to 30 s, still honoring cancellation.
- Auto-update stays opportunistic: it skips on conflict, and only updates the agent being
  launched.
- `codex_userns` applies to the whole container. The existing warning already says "all guest
  processes".

## Existing containers

There is no migration. Single-pair entries, images, provider IDs and Docker names are
unchanged. Bundle entries are new keys next to them. Remove, prune and stop act on one entry,
which is one container, as today.

## Smallest change

1. `config`: add `Entry.Agents`/`Tools`/`AntigravityConnection` and update identity; add
   `Selection.Name`/`AntigravityConnection` with validation.
2. `operations`: `createCategory` makes one bundle entry when there are two or more pairs;
   update `ImageSpec`/`bakeConfig`, `ProviderID`/`ActiveAdapters`/`ProviderConfig` and the
   ACP probe.
3. `launcher`: resolve the agent through the provider ID; make the startup lock wait.
4. Dockerfile, `install-tools.sh`/`install-utilities.sh`: loop over agents and pairs.
5. Wizard, CLI and create config: add name and connection; README; one CI bundle job.

## Decision needed

If `agy` has no ACP mode, the `acp` option can never activate. The choices are:

- **(a)** Keep the option and fail it clearly. This is my recommendation.
- **(b)** Bring back the separate official ACP server artifact. That means a large install, an
  arm64 QEMU workaround, and network installs.
- **(c)** Ship `none` only for now.
