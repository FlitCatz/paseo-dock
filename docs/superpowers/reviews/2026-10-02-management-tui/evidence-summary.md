# Historical evidence summary and transcript inventory

Raw transcripts were removed from the reviewable checkout on 2026-10-12.
The controller retained byte-identical copies locally under ignored
`.artifacts/review-logs/`, preserving each original repository-relative path.
Previously committed originals remain in Git history; no history rewrite occurred.
This inventory identifies historical runs and is not a fresh acceptance result.
Links formerly pointing to raw logs now deliberately lead to this summary.

The management TUI reports record race/vet/build, help/no-argument behavior
and PTY checks. The live lifecycle encountered a manifest HTTP 403; full live
lifecycle is not established. See [Task 6 report](task-6-report.md) and
[review](task-6-review.md) for results and limitations.

| Former transcript | Bytes | SHA256 |
| --- | ---: | --- |
| `task-6-help.log` | 154 | `77ef12406861b36fc9d6c6107013fe46d127f8eb47c93f3c4c050c2300bddf8b` |
| `task-6-make-build.log` | 231 | `fb5d2f8bd941996ad06e8e8e4d3b746a950d6fd9d975e50b2353698de128ba4c` |
| `task-6-make-test.log` | 585 | `b09080e3ce2d6df9cb57722d35304316ef462f2b0aea5555286021f819900e1b` |
| `task-6-make-vet.log` | 13 | `685d158f951e3eb11749d2e519a64eba4f57dcc93cd562eaee2cc9457f72846e` |
| `task-6-noargs.log` | 100 | `d7bef0287d51a01ab8995b2737887e796ccd7f8b1ac8802e3b217b28f7064dd2` |
| `task-6-pty.log` | 4919 | `ae9b0b64480b534712551267c98a5417fbd1b1c85c7d601b6ba06a44a285ef4b` |
