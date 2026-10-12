# codex-userns-v1

The embedded JSON is byte-identical to the reviewed `/private/tmp/paseo-sbx-minimal-seccomp.json` artifact, SHA256 `de699bf56093ed501d6cf021db43b48f6bd5bd843a715404e7d0714ff0ed0af8`.

Derived from Moby profiles, Apache License 2.0:
https://github.com/moby/profiles/blob/2ceae35d351c156cb5a8efc0fdc4a08cf94569d8/seccomp/default.json
Baseline SHA256 `6416b47770785a41ac59073cdc77d9fe98517df2799dc83ef207e622de3053f6`.
Copyright Moby Authors. The upstream license is included in LICENSE.

Changes retain baseline metadata and conditional rules, remove socketcall, and add narrowly constrained clone (8), unshare (1), mount (72), pivot_root (1), and umount2 MNT_DETACH (1) rules. The default action remains deny; clone3 remains ENOSYS. SYS_ADMIN must be dropped to prevent the baseline conditional administrative rules from activation.

This policy expands kernel syscall access for every process in the opted-in container. It was validated on the image/platform and Codex versions recorded in docs/superpowers/reviews/2026-10-06-runtime-categories/namespace-compatibility-proposal.md; it is not claimed to equal any installed Docker daemon default policy.
