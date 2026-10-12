# Namespace compatibility experiment proposal

Status: both authorized spikes executed; minimal policy passes; product integration not applied. Date: 2026-10-08.

Question: can the current Codex Linux sandbox run inside an unprivileged Docker container
using an explicit, constrained seccomp profile while retaining workspace-write confinement?

Observed: Docker Engine 29.4.0, commit `daa0cb7f`, OrbStack; built-in seccomp; no AppArmor
profile or extra capabilities; guest UID/GID 501:20, effective capabilities zero.
`unshare --user --map-root-user true` returns EPERM. System bubblewrap 0.11.1 also fails before
the requested command. Existing original acceptance container was restored to stopped state.

The proposed experiment creates one new disposable container with only its disposable
workspace mounted. No existing container, daemon, sysctl or host credential is changed.
Codex workspace-write remains enabled, and no privileged mode or extra capability is used.

Candidate profile derives from the pinned official Moby profile revision
`2ceae35d351c156cb5a8efc0fdc4a08cf94569d8`, retaining default-deny and existing conditional
rules. This is a known explicit baseline, **not claimed to be the exact installed daemon
profile**: public source retrieval for `daa0cb7f` and `v29.4.0` returned 404. Record the
baseline digest and differences. Remove legacy `socketcall` permission, keeping AF_ALG and
AF_VSOCK denied, rather than using compatibility workarounds for 32-bit software.

Only source-derived namespace requirements may be added:

- `clone`: exact bubblewrap flags including a new user and mount namespace, with the
  IPC/PID/network combinations needed by Codex; preserve clone3 ENOSYS.
- `unshare`: only CLONE_NEWUSER; any other requirement needs evidence and renewed review.
- `mount`: only specific propagation, bind/remount and tmpfs/proc/devpts flag combinations.
- `pivot_root`: required by bubblewrap root transitions; pathname pointers cannot be
  inspected by ordinary seccomp.
- `umount2`: only MNT_DETACH.

This expands kernel access for **all** processes in that one container. Seccomp cannot
limit the added rules to the bubblewrap executable or determine whether the process is
already in a child user namespace. Linux capability checks still apply. A successful
startup alone does not establish sufficient confinement.

Validation: actual Codex command writes inside the workspace; writes to a separate
agent-owned home path and protected/symlink paths are denied; expected network restriction
holds; agent capabilities stay zero; unrelated administrative syscalls stay blocked.
Repeat against the current agent version before any product integration. Stop the exact
new container and preserve its identity/logs. Stop on an unexplained failure instead of
adding a broad permission group. Product support remains a separate reviewed change.

Sources:

- [Docker seccomp](https://docs.docker.com/engine/security/seccomp/)
- [Pinned Moby profile](https://github.com/moby/profiles/blob/2ceae35d351c156cb5a8efc0fdc4a08cf94569d8/seccomp/default.json)
- [Codex 0.161.0 bwrap arguments](https://github.com/openai/codex/blob/rust-v0.161.0/codex-rs/linux-sandbox/src/bwrap.rs)
- [bubblewrap 0.11.1](https://github.com/containers/bubblewrap/blob/v0.11.1/bubblewrap.c)

Independent source review considers this technically plausible, with the kernel-surface
limitation above. The user explicitly approved the limited experiment on 2026-10-08.

## Recorded outcome

Exactly one new container was created and reused: CID
`f4e7b755a2f9b8ecb8f7423b1f5004f2e02b905cedfe3c76fec1ffac6edf5156`, root
`/private/tmp/paseo-sbx-userns-live-3660932136`. Image identity is the previously built
`sha256:7760a63ba6a62611c4434c1208c1c87236cd37d36b691da206ebd0d7e77d6521`.
Baseline SHA256: `6416b47770785a41ac59073cdc77d9fe98517df2799dc83ef207e622de3053f6`.
Actual candidate SHA256: `cb1ec6ebd5e65e1852a77a4de8e8911c3a6f50827eaeaf0df6fd9d683a371d06`.
No added capabilities or privileged mode; no-new-privileges enabled; sole new workspace bind;
no ports/home/socket mounts. Image/label/mount/security-policy checks precede exec and stop.

Bubblewrap works. Actual Codex 0.159.2 first pass and exact-installed 0.161.0 pass workspace
writes, agent-home outside path/symlink/protected-path denial, network denial, and zero agent
capabilities. Baseline outside-home write and TCP connection work before the sandbox.
Final preserved log uses 0.161.0, selected by its persisted update. AF_ALG, AF_VSOCK, bpf,
perf_event_open, setns and unrelated unshare/mount requests remain denied.
The exact container was stopped after every attempt and preserved.
[Final runtime log](evidence-summary.md).

The first build failed on the host cache, but the earlier executable was inadvertently run.
Its initial obsolete `sandbox linux` command failed after successful bubblewrap startup.
The executable was rebuilt with a writable cache and corrected command/guard/assertions;
only the existing exact container was reused. The actual revision-one profile includes
unneeded strict/lazy timestamp remount combinations. Source removes those combinations,
but the minimal revision is untested because replacing the single approved container was
not part of this experiment. Success applies only to the digest recorded above.

This answers feasibility for the recorded profile and tested probes, not general kernel
security or product acceptance. No model authentication was installed in the experiment
guest, and no model call or model-triggered tool execution was attempted. Product integration
requires a reviewed minimal profile, new confinement evidence for that exact profile,
and an explicit decision about the expanded per-container kernel surface.

The reviewed minimal artifact is `/private/tmp/paseo-sbx-minimal-seccomp.json`, SHA256
`de699bf56093ed501d6cf021db43b48f6bd5bd843a715404e7d0714ff0ed0af8`.
Independent audit confirms unchanged baseline metadata/rules except socketcall removal,
with added clone8/unshare1/mount72/pivot_root1/umount2(DETACH)1 rules. The helper now stages
a prebuilt Go ELF arm64 probe before container creation, returns cleanup failures and checks
the exact final stopped state. The probe explicitly asserts CapEff0/NoNewPrivs1/Seccomp2.
Host/Linux builds and vet pass. This artifact is ready for a separate fresh-container
validation; it has not been applied to any guest. The earlier approval covered one new
container, and Docker fixes seccomp policy at creation, so validating this different policy
required authorization for another disposable container. That preparation is preserved above;
the subsequent approval and validation are recorded below. Existing guests remain untouched.

## Additional authorized minimal-policy validation

The user explicitly approved one additional disposable container for the minimal policy.
Root `/private/tmp/paseo-sbx-userns-live-1919452979`, exact CID
`b09beb001008c739bcc3e55ad5fdce95194e63cc7eab16328b88f64e94847c4c`, candidate SHA256
`de699bf56093ed501d6cf021db43b48f6bd5bd843a715404e7d0714ff0ed0af8`.
No original container or daemon policy was changed; only its new workspace is mounted.
Capability additions/privileged mode are absent and no-new-privileges is enabled.

Host helper exit0. Actual Codex0.159.2 and exact guest-installed0.161.0 both pass all
workspace/outside/symlink/protected/network probes with explicit CapEff0/NoNewPrivs1/Seccomp2
assertions. Baseline outside write/network succeed; AF_ALG/AF_VSOCK/bpf/perf_event_open/setns
and unneeded namespace/mount calls are denied. Bubblewrap succeeds. Cleanup stop and exact
final running=false assertion pass. [Actual minimal-policy log](evidence-summary.md).

This closes the earlier minimal-profile verification limitation for the recorded image,
versions and probes. It does not establish authenticated inference or a model-triggered
tool call; user login is currently deferred. Product creation still uses its existing policy.

## Product integration candidate — decision required

Recommended scope: an explicit opt-in at creation of new Docker Codex containers.
The CLI/TUI expose the choice and its expanded per-container kernel surface in confirmation.
Only the reviewed minimal policy and no-new-privileges are used; no added capabilities.
Persist the fixed policy identifier/digest with the exact resource; inspect/revalidate it
before subsequent execution. Default/legacy existing-resource behavior is retained, and
existing containers and the daemon are never modified. Unsupported categories are rejected.

Alternative decision: make this the default for new Codex containers to enable workspace-write
without an extra choice; this explicitly expands kernel access for all processes in each new
container. The experiment authorization does not itself choose a product default.
Implementation will need policy persistence/validation, CLI/TUI create flow, mismatch and
legacy compatibility regressions, independent review, and a fresh Service-created guest
validation against the exact product policy. Actual authenticated model calls remain deferred.
The user explicitly selected opt-in implementation for new Codex containers. Implementation
is now authorized with the constraints above; no equivalent approval will be requested again.
No product integration code or new product-policy guest has been created at this checkpoint.
