# One container per `create` (multi-agent container)

Status: design rev 2, awaiting approval. Category `container` only; `sandbox` (agent-only SBX)
and legacy schema-1 SBX are unchanged.

Rev 2 (captain review):

1. The Paseo connection method is now a per-agent option for every agent in the container.
2. Oh My Pi (`omp`) and Gajae Code (`gjc`) are added to the container catalog.

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
  - `connections`: a map from agent to connection mode. See "Paseo connection per agent".
- **One pair** with default connections keeps today's path exactly: the same entry shape, key
  `container/<agent>-<tool>`, image tag, bake target and provider ID. Existing single-agent
  containers and their registrations still match and are reused.
- **Two or more pairs**, or any non-default connection, create one *bundle* entry:
  - The key is `container/<name>`. The default name is the sorted distinct agents followed by
    the sorted methodologies, for example `codex-claude-superpowers-ponytail`.
  - `config.Entry` gains `Agents []string`, `Tools []string` (normalized and sorted) and
    `Connections map[string]string`. `Agent` and `Tool` stay empty.
  - `EqualIdentity` covers the new fields. Re-running the same selection reuses the container.
    A different selection or different connections under the same name are refused, and the
    existing container is kept, as today.
  - Running `create` again with a different selection or name makes another container. That is
    how the captain gets separate containers.
- **Image**:
  - One image per bundle: `namespace/container-<name>:<tag>`.
  - `ImageSpec` hashes the category, sorted pairs, utilities, connections and the lock with
    recipe `bundle-v1`. Connections are included because an ACP adapter can be an installed
    package. The single-pair recipe is unchanged.
- **Dockerfile**:
  - `AGENT`/`TOOL`/`METHOD_INSTALLER` become comma lists: `AGENTS`, `PAIRS`
    (`agent:tool:installer`) and `ACP_ADAPTERS`.
  - The agent install step loops over `AGENTS`, using npm for codex and claude and
    `install-runtime` (checksum-verified binaries) for antigravity-cli, omp and gjc.
  - Single-pair builds pass one-element lists, so they produce the same image.
  - `BASE_IMAGE` is the `base_images` entry of the first selected agent in catalog order
    (codex, claude, antigravity-cli, omp, gjc). Claude is installed through npm in either case.
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

## Paseo connection per agent

Every agent in the container has a connection mode: `native`, `acp` or `none`.

- It is chosen in the wizard (one row per selected agent), with `--connection agent=mode` (repeatable),
  or with `connections` in the create config.
- Paseo 0.11.2 accepts only `claude, codex, copilot, opencode, pi, omp` and `acp` as `extends`
  bases, so `native` is offered only for codex, claude and omp.

| agent | modes | default | `native` command | `acp` command in the container |
| --- | --- | --- | --- | --- |
| codex | native, acp, none | native | `codex …` (Paseo args passed through) | `codex-acp` (Zed adapter `@zed-industries/codex-acp`) |
| claude | native, acp, none | native | `claude …` | Claude ACP adapter. **Package not identified offline**, so `acp` is unavailable until it is pinned. |
| omp | native, acp, none | native | `omp …` (Paseo's built-in omp runtime replaces `command`) | `omp acp` |
| gjc | acp, none | acp | n/a (no built-in) | `gjc acp`, env `GJC_ACP_PERMISSION_MODE=prompt` (same as the host's `gjc` provider) |
| antigravity-cli | none (acp blocked) | none | n/a (the plugin provider cannot be extended) | **none found in the pinned `agy` 1.2.14.** See below. |

**Registration**

- One provider per agent whose mode is not `none`:
  - ID `container-<name>-<agent>`; single-pair entries keep `container-<agent>-<tool>`.
  - Label `SBX <name> · <Agent label>`.
  - `native` → `extends: <codex|claude|omp>`, command `[launcher, id]`.
  - `acp` → `extends: "acp"`, command `[launcher, id, "--acp"]`.
- The launcher maps `--acp` to that agent's ACP argv from a fixed Go table. It no longer appends
  `--acp` to the agent CLI as it does today for `agy`.
- IDs never collide with single-pair IDs, because agent IDs and methodology IDs are disjoint.
  The launcher's `providerEntry` maps an ID to its `(key, entry, agent)` by exact match.
- PR #1's guarantees stay:
  - Every base goes through `filterProviderBases` (an unsupported base is skipped with a warning).
  - The full config is validated, with rollback on failure.
  - `MergeManagedConfig` ownership stays the same. Its "managed command" test accepts
    `[launcher, id, "--acp"]` for any agent, not only Antigravity IDs, so it can remove its own
    stale entries.
- Existing single-pair Antigravity entries count as `none`. They stop emitting the
  always-skipped `extends: "antigravity"`, which removes a warning and nothing else.

**ACP safety check**

- For each `acp` agent, at create (before activation) and at register:
  - The launcher path runs the agent's ACP argv in the container.
  - It sends ACP `initialize` over stdio and requires a valid `protocolVersion` response
    within 10 s.
- On failure, only that agent's provider stays inactive, with a clear error. The other agents
  in the same container still register.

**What I could and could not verify offline** (no network installs; read-only inspection only)

| agent | verified | not verified |
| --- | --- | --- |
| omp | Host omp 18.10.3: `omp acp` exists ("Run omp as an ACP server over stdio"). Paseo's omp built-in runs the `omp` binary through a replaceable `command` (seen in Paseo's bundled server code). | Whatever omp version we pin; no ACP handshake was run. |
| gjc | Host gjc 0.18.11: `gjc acp` exists. The host's Paseo `gjc` provider is `extends: "acp"`, `command: ["gjc","acp"]`, env `GJC_ACP_PERMISSION_MODE=prompt`. | Whatever gjc version we pin; no ACP handshake was run. |
| codex | Host codex 0.162.1 has no ACP subcommand (only `app-server`). The ACP route is the Zed adapter. `@zed-industries/codex-acp@0.11.1` is in the local npx cache with npm sha512 integrity for `linux-arm64` and `linux-x64`. It is a standalone binary and reads `~/.codex`. | Its compatibility with our pinned codex 0.159.2 config and auth; no ACP handshake was run. |
| claude | Host claude 2.1.293 `--help` shows no ACP mode. | The current ACP adapter's package name, version and integrity (not cached locally). |
| antigravity-cli | I read the pinned `agy` 1.2.14 from the local image `paseo-sbx/container/antigravity-cli-gsd-core` (arm64, read-only `docker save`, no container run). Its strings have no ACP methods (`session/new`, `session/prompt` absent), no `acp` flag or subcommand, only an `acp_version` protobuf field. Host `agy` 1.3.5 `--help` lists no ACP flag or subcommand; `agy help acp` gives "unknown subcommand". So the old "does not install an ACP server" note is still accurate. | Whether Google's separate ACP server (used and removed on 2026-10-01) still exists. Its URL and checksum were never committed, so they are not in git history. |

So "every agent has ACP" holds today for omp and gjc (built into the CLI) and for codex (through a
pinned adapter). Claude needs an adapter I could not identify offline. The pinned Antigravity
CLI has no ACP mode.

## Added agents: Oh My Pi (`omp`) and Gajae Code (`gjc`)

| | omp | gjc |
| --- | --- | --- |
| catalog id / adapter | `omp` / `omp` | `gjc` / `gjc` |
| what it is | Bun-compiled single binary | Bun-compiled single binary (fork of omp) |
| install | Go `install-runtime --agent omp` → `/opt/paseo-sbx/runtime/bin/omp`. Pinned linux arm64/x64 artifact, verified by sha512 like `agy` (`agent_artifacts.omp`). | Same pattern → `/opt/paseo-sbx/runtime/bin/gjc` (`agent_artifacts.gjc`). |
| source (from binary strings) | GitHub `can1357/oh-my-pi`. npm `@oh-my-pi/pi-coding-agent` ships `src/cli.ts`, which needs Bun, so it is not used. | GitHub `Yeachan-Heo/gajae-code` |
| auth/config in container | `~/.omp` (`agent/`: `config.yml`, `agent.db`, `models.db`) | `~/.gjc` (`agent/`: same layout, plus `credential-auto-import-state.json`) |
| Paseo provider | `native` → `extends: "omp"` (default), or `acp` → `omp acp` | `acp` → `gjc acp` (only mode) |
| base image | codex sandbox-templates base (like antigravity-cli) | same |
| methodologies | `native` only for now | `native` only for now |
| utilities | none (rtk/headroom agent lists unchanged) | none |
| self-update | Disabled in the container. The launcher's update path skips omp; the version stays pinned. | same |

- **Methodologies**: both CLIs support skills (`--skills`, `omp skill`) and plugins (`omp plugin`).
  But the install targets for Superpowers, GSD Core, Ponytail and OpenSpec are not verified
  offline. So only the `native` methodology lists `omp` and `gjc`. The others can be added once
  verified.
- **gjc credential auto-import**: gjc has a credential auto-import state file. In a shared
  container it may import Claude or Codex logins from `~/.claude` and `~/.codex`. That stays
  inside the container and never touches host credentials, but it should be noted in the README.
- **CI and bake**:
  - The catalog gains pairs `omp-native` and `gjc-native`: two more matrix jobs and two more
    `docker-bake.json` targets.
  - The `ci-all` bundle includes them.
  - Each build downloads the pinned artifacts and verifies their sha512.

**Missing, so I will not guess.** Adding omp and gjc is blocked until someone supplies these, or
authorizes a network lookup to fetch them:

1. The exact release tag to pin for each. Host candidates are omp 18.10.3 and gjc 0.18.11.
2. Linux `arm64` and `x64` release asset URLs for that tag. The binaries' strings only show the
   names `omp-darwin-arm64`, `gjc-darwin-arm64`, `gjc-darwin-x64` and `gjc-linux-x64`, so an arm64
   linux gjc asset is unconfirmed.
3. A sha512 (or sha256) for each asset.
4. Whether those Linux binaries run on the sandbox-templates base image (glibc version,
   baseline vs modern CPU builds).
5. Whether gjc requires a separate omp install or any other runtime.
6. The license and redistribution terms for baking the binaries into published GHCR images.
7. For codex ACP: confirmation that `codex-acp` 0.11.1 matches codex 0.159.2, or a different
   pin.
8. For claude ACP: the adapter's package name, version and integrity.

## Auth and config inside one container

Everything stays in container-local `/home/agent`, and host auth is never shared:

| agent | auth and config |
| --- | --- |
| Codex (and codex-acp) | `~/.codex` |
| Claude | `~/.claude`, `~/.claude.json` |
| Antigravity | `~/.gemini` |
| omp | `~/.omp` |
| gjc | `~/.gjc` |

These paths are disjoint, so the logins coexist. You log in once per agent through the
dashboard shell (`s`).

## Two agents at once

- Each launch is its own `docker exec` in the running container.
- The launcher's startup lock (`operationlock` key `container:<id>`) is non-blocking. Today a
  second launch starting during the first one's short startup window fails with "operation
  already running". It will wait instead: retry for up to 30 s, still honoring cancellation.
- Auto-update stays opportunistic: it skips on conflict, and only updates the agent being
  launched (never omp or gjc).
- `codex_userns` applies to the whole container. The existing warning already says "all guest
  processes".

## Existing containers

There is no migration. Single-pair entries, images, provider IDs and Docker names are
unchanged. Bundle entries are new keys next to them. Remove, prune and stop act on one entry,
which is one container, as today.

## Smallest change

1. `config`:
   - Add `Entry.Agents`, `Tools` and `Connections`, and update identity.
   - Add `Selection.Name` and `Connections` with validation and defaults.
   - Add catalog entries for omp and gjc, plus a per-agent connection table (allowed modes, default,
     native base, ACP argv).
2. `operations`:
   - `createCategory` makes one bundle entry when there are two or more pairs or non-default
     connections.
   - Update `ImageSpec`/`bakeConfig` and `ProviderID`/`ActiveAdapters`/`ProviderConfig`.
   - Add the ACP probe.
3. `launcher`:
   - Resolve the agent from the provider ID, and map `--acp` to the agent's ACP argv.
   - Make the startup lock wait.
   - Skip updates for omp and gjc.
4. `runtimeinstall`: generalize the `agy` installer to omp and gjc. Dockerfile,
   `install-tools.sh` and `install-utilities.sh` loop over agents and pairs, and install
   `codex-acp` when codex uses `acp`.
5. Wizard, CLI and create config: add name and connections. Update the README and add the one CI
   bundle job.

Items 1–5 can ship without omp and gjc. Their catalog entries and lock artifacts land when the
missing data above is supplied.

## Decisions needed

1. **omp and gjc pins**: supply the missing items 1–6, or authorize a network lookup (GitHub
   releases, read-only) so I can fill them in and report them for approval. Until then, phase 2
   ships without omp and gjc.
2. **Codex ACP adapter**: pin `@zed-industries/codex-acp@0.11.1` (integrity known offline), or
   name another version.
3. **Claude ACP**: name the adapter to pin. Without one, Claude offers only `native` and `none`.
4. **Antigravity ACP**: the pinned `agy` has no ACP mode. Choose one:
   - (a) Antigravity offers only `none` for now. This is my recommendation.
   - (b) Look up Google's separate ACP server again (network; it needed QEMU on arm64 in October).
