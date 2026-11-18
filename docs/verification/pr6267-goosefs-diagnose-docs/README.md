# Verification — PR #6267: docs: drop the removed GooseFS diagnose script from troubleshooting

- PR: https://github.com/fluid-cloudnative/fluid/pull/6267
- Base: `dcb76167b0143a239addc6bed83ed50b45267631` — Head: `f95486bd313aca075f6d6bb1426e951ff3085340`
- Harness: additive only (this `docs/verification/` tree). No production code touched.

## Premise (P0) — CONFIRMED

Claim (quoted from the PR body): "The troubleshooting guide (English and Chinese) still tells
users to download `tools/diagnose-fluid-goosefs.sh`, but that script was removed when the
GooseFS runtime was deprecated in #5703, so the `wget` command returns 404."

Verified against the **base** branch (`dcb76167`):

- `git ls-tree dcb76167 tools/` — no `diagnose-fluid-goosefs.sh`; the script was removed by
  commit `6dff0aaba` ("ddc: deprecate goosefs runtime (#5703)"). The Alluxio, JuiceFS and
  JindoFS scripts named in the same docs all still exist.
- `TestTroubleshootingDocsReferenceExistingScripts` FAILS on base (see
  `results/base-reverify.out`): both troubleshooting docs reference the missing script, i.e.
  the documented `wget` would 404 exactly as claimed.

Component match: the report names the troubleshooting docs and `tools/`; the patch touches
exactly those two docs. Match.

## Findings

No defects found in the PR itself. The diff removes every `diagnose-fluid-goosefs` reference
from both docs (`git grep -i diagnose-fluid-goosefs <head>` is empty repo-wide) and leaves the
markdown structure intact in both languages.

The contract tests below double as this PR's regression suite for future rounds:

| ID | Test | Polarity | Layer | Base | Head |
|----|------|----------|-------|------|------|
| D1 | TestTroubleshootingDocsReferenceExistingScripts | contract | unit | FAIL (premise reproduced) | PASS |
| D2 | TestNoStaleGooseFSDiagnoseReference | contract | unit | FAIL | PASS |

Observed vs expected:

- D1: expected FAIL on base / PASS on head — observed exactly that (`results/base-reverify.out`,
  `results/head-unit.out`). Harness bites confirmed.
- D2: same polarity; PASS on head, FAIL on base (the goosefs reference is present there).

## How to run

```bash
# from the repo root of this branch
go test ./docs/verification/pr6267-goosefs-diagnose-docs/ -v

# full re-verification against any ref (defaults to the current PR head via manifest.pr)
bash docs/verification/pr6267-goosefs-diagnose-docs/scripts/re-verify.sh            # vs PR head -> FIXED
bash docs/verification/pr6267-goosefs-diagnose-docs/scripts/re-verify.sh dcb76167b0143a239addc6bed83ed50b45267631  # vs base -> STILL-BROKEN
```

## Handoff / continuing after changes

Everything durable is on this branch (`verify/pr6267-goosefs-diagnose-docs-codex`):
the test, `verify-manifest.json` (with `pr` set for machine-independent head discovery),
`.last-reviewed`, and `results/`. On a fresh machine: fetch the branch, run
`re-verify.sh` with no arguments, review the printed `last-reviewed..head` delta, then advance
`.last-reviewed` as the script instructs. Both tests are contract tests — a future fix keeps
them green; if either goes red, the docs again reference a script that does not ship.

Out-of-scope observations (pre-existing on base, NOT introduced by this PR, recorded so a
future round is not surprised; none are findings against this PR):

- `tools/diagnose-fluid-curvine.sh` ships but is not listed in either troubleshooting doc.
- The English doc's step-1 code block omits the JindoFS `wget` line that the top-of-page list
  and the Chinese doc both include (pre-existing inconsistency).
- Other docs (`core-concepts/concepts.md`, `dashboard/overview.md`, `userguide/install.md`)
  still mention GooseFS the runtime; this PR scopes itself to the removed diagnose script only.
