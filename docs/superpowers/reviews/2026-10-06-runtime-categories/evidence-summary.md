# Historical evidence summary and transcript inventory

Raw transcripts were removed from the reviewable checkout on 2026-10-12.
The controller retained byte-identical copies locally under ignored
`.artifacts/review-logs/`, preserving each original repository-relative path.
Previously committed originals remain in Git history; no history rewrite occurred.
This inventory identifies historical runs and is not a fresh acceptance result.
Links formerly pointing to raw logs now deliberately lead to this summary.

Category reports distinguish failed initial runs from corrected continuations.
Fake-boundary regressions cover foreign target rejection and JSON-RPC initialize
responses; actual native transport and Codex confinement checks were exercised.
SYS_ADMIN canonicalization initially broke userns creation/cleanup; the correction
and same-CID continuation succeeded. Final isolated guest/daemon stops were
recorded. These runs do not establish authenticated model inference or
model-driven MCP calls, amd64 live confinement, or AppArmor-host compatibility.
See [category report](task-7-report.md), [userns report](userns-task3-report.md),
and [policy proposal](namespace-compatibility-proposal.md). The final make logs
record historical test/vet/build success; fresh maintenance checks are recorded
in [closure status](branch-closure.md).

| Former transcript | Bytes | SHA256 |
| --- | ---: | --- |
| `task-7-codex-before-path-fix.log` | 21408 | `117d87c626e7c6b021aba930274c8830c29f4e43001dfe50408c88abede0a9cf` |
| `task-7-codex-final-diagnostics.log` | 21894 | `98eb042d31e68c444396f5cd3e7bc18e8d7793e0569d853134725cd26bd97a74` |
| `task-7-continuation-final-make-build.log` | 231 | `fb5d2f8bd941996ad06e8e8e4d3b746a950d6fd9d975e50b2353698de128ba4c` |
| `task-7-continuation-final-make-test.log` | 586 | `bf28554c15ddfa49778fd0061cc1eeb82250d4fcff589388b1319e60f6cd4b86` |
| `task-7-continuation-final-make-vet.log` | 13 | `685d158f951e3eb11749d2e519a64eba4f57dcc93cd562eaee2cc9457f72846e` |
| `task-7-continuation-make-build.log` | 231 | `fb5d2f8bd941996ad06e8e8e4d3b746a950d6fd9d975e50b2353698de128ba4c` |
| `task-7-continuation-make-test.log` | 586 | `93b7c6fa4bec2f06026ecbf43415e257df4ff365c4a4226a5a13b5e60ff2f0ba` |
| `task-7-continuation-make-vet.log` | 13 | `685d158f951e3eb11749d2e519a64eba4f57dcc93cd562eaee2cc9457f72846e` |
| `task-7-final-make-build.log` | 231 | `fb5d2f8bd941996ad06e8e8e4d3b746a950d6fd9d975e50b2353698de128ba4c` |
| `task-7-final-make-test.log` | 580 | `d9743286dc8ab533ba781ffba970010f2fa8fc907d55463aec58ee9d6e36bdd9` |
| `task-7-final-make-vet.log` | 13 | `685d158f951e3eb11749d2e519a64eba4f57dcc93cd562eaee2cc9457f72846e` |
| `task-7-fix-green.log` | 35 | `a926cddbc9c2093c21706431f827ac9546be6f47d7268efc426c866a44ec7f03` |
| `task-7-fix-red.log` | 3709 | `528f2afed04e3a44dd30a9e84c4de89cf77da94616af03cf99a8e038707b8fcf` |
| `task-7-initialize-red.log` | 791 | `33294d22fc3168a585f7c3dd3ea5dbb904a6f4de77418873ad638bc91c8ece7d` |
| `task-7-namespace-green.log` | 4992 | `c59fb8393707e10cb8fd263a91390485723b49c73bc5fde479c293b0ab33ba0d` |
| `task-7-namespace-minimal-live.log` | 5167 | `9dfc72f6b8913023d64819ff7e595c3c37319bfc01adf824fd3cd5e4d865249d` |
| `task-7-native-archive-green.log` | 1880 | `96c921da8a8961e5ef153ba98983cf7f2ab51a09c51b97c24d5e0ffe595e61e1` |
| `task-7-native-concurrent-lock.log` | 736 | `afc4d5a8c68f333f3bcfcc68458e718a235a0bff0fd838fbfe6678531a205c93` |
| `task-7-native-green.log` | 1237 | `74a3ee9406b91d7daa77dd50dff79b7b69997603bf24c12b0929c159b9cff149` |
| `task-7-native-path-green.log` | 1820 | `d15fd46c3c01ab14b5d40e703b229df5be3da5ea2c074352107fd5267cb6a6f0` |
| `task-7-native-path-red.log` | 659 | `aaffcd1a97c28cdeff0617ae7b023aa46034d7ad99e1b60f7cd37da9b4c0c038` |
| `task-7-path-regression.log` | 40 | `450213a9d0eb2e9adb3f0c580b0eb9dab7685f86993fd8d4d28a0e83bd807a02` |
| `task-7-provider-final-live.log` | 5673 | `0dd0e9fca669376d80deafed06962d248834a750bd16a882f25e9e52357cea2e` |
| `task-7-provider-fixed-live.log` | 5048 | `1461b572ffcec575dd4d59a413050b0d1bb23e87a9fc49d4ec74a3588e864f81` |
| `task-7-resumed-live.log` | 5277 | `f396eadb591aa4d16756cddf0f676b9011a2a8cff6206e157abb571d829fa9b9` |
| `userns-final-make-build.log` | 231 | `fb5d2f8bd941996ad06e8e8e4d3b746a950d6fd9d975e50b2353698de128ba4c` |
| `userns-final-make-test.log` | 621 | `fa08343dfed328669aac77722e2ab871755a8bf2e9405084114d5fddf3baf1de` |
| `userns-final-make-vet.log` | 13 | `685d158f951e3eb11749d2e519a64eba4f57dcc93cd562eaee2cc9457f72846e` |
| `userns-task3-cleanup.log` | 1347 | `5e73b5db0e74c3f2e4bdb1036e3b567d5aeeb9392ebc91aa059795cc39f13e84` |
| `userns-task3-daemon-cleanup.log` | 1224 | `ce7ca91de44a884647e7d46870f00825db1c7146d93fe522f603dc521c106e02` |
| `userns-task3-daemon-helper-cleanup.log` | 2156 | `204302c33c301243d5a68ffc9a1a74f743df1e3ee8c997feac524b04a7f5a986` |
| `userns-task3-live-resume.log` | 12335 | `6a812d73fcbcf18a5428daaeecaed568fc25de0166120ac3499cc6ac9ac81a16` |
| `userns-task3-live.log` | 1344 | `3181b4e559986a26de975f74147aaaaff11f3e5f0061f7efd28e70e9352fd316` |
