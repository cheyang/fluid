# Verification — PR #6267 — troubleshooting GooseFS diagnose script removal (Reviewer A, Claude)

## Premise (P0) — CONFIRMED

The PR claims the en/zh troubleshooting guides tell users to `wget` a GooseFS
diagnose script that no longer exists, so the command 404s.

Evidence (against **base** `dcb76167b0143a239addc6bed83ed50b45267631`):

- `tools/diagnose-fluid-goosefs.sh` is absent from the base tree; it was deleted
  by commit `6dff0aaba` ("ddc: deprecate goosefs runtime (#5703)").
- `https://raw.githubusercontent.com/fluid-cloudnative/fluid/master/tools/diagnose-fluid-goosefs.sh`
  → HTTP 404 (checked 2026-11-18); the three sibling URLs (alluxio, juicefs,
  jindo) → 200.
- Contract check C1 fails on base (script referenced, missing from tree) and
  passes on the PR head — the harness bites.

## Observed vs expected

| ID | Claim | Layer | Polarity | Base | Head | Verdict |
| --- | --- | --- | --- | --- | --- | --- |
| P0 | Guides reference a removed script (404 wget) | unit | contract | FAIL | PASS | Fixed / Confirmed premise |
| C1 | Every `diagnose-fluid-*.sh` URL in troubleshooting.md resolves in-tree | unit | contract | FAIL | PASS | Pass |
| C2 | No `diagnose-fluid-goosefs` references remain in docs/ | unit | contract | FAIL | PASS | Pass |

Raw output: `results/head-run.txt`, `results/base-run.txt`.

## Findings status

No blockers or majors. Two pre-existing nits (not introduced by this PR, do not
gate the verdict):

- `N1-curvine-undocumented` — `tools/diagnose-fluid-curvine.sh` exists but is
  not documented in the troubleshooting guides (en or zh).
- `N2-stale-goosefs-mentions` — other GooseFS mentions remain in
  `docs/{en,zh}/core-concepts/concepts.md`, `docs/{en,zh}/dashboard/overview.md`,
  and `docs/{en,zh}/userguide/install.md` (goosefsruntimes CRD listing).
  The runtime is gone from `pkg/ddc/`, so those are stale, but they are outside
  this PR's stated scope.

## How to run

From a checkout of this branch (repo root):

```bash
python3 docs/verification/troubleshooting-goosefs-claude/scripts/check_troubleshooting_links.py          # deterministic
python3 docs/verification/troubleshooting-goosefs-claude/scripts/check_troubleshooting_links.py --http   # + HEAD-check raw URLs
bash docs/verification/troubleshooting-goosefs-claude/scripts/re-verify.sh [ref]                          # graft onto any ref
```

`re-verify.sh` with no argument fetches the current PR head from
`https://github.com/fluid-cloudnative/fluid/pull/6267`, grafts the harness onto
it via a temp worktree, and exits 0 iff C1/C2 pass.

## Continuing after the fix / next round

All tests here are **contract** polarity (no canaries): a red result *is* the
reproduction. After any further doc edits, re-run `scripts/re-verify.sh`
(no argument) — everything should stay green. If new `diagnose-fluid-*.sh`
links are added to the guides, C1 automatically covers them. Advance
`.last-reviewed` to the reviewed sha after each round:

```bash
echo <reviewed-sha> > docs/verification/troubleshooting-goosefs-claude/.last-reviewed
```

Kickoff prompt for a fresh agent: "Checkout branch
verify/troubleshooting-goosefs-claude from the reviewer fork cheyang/fluid,
run docs/verification/troubleshooting-goosefs-claude/scripts/re-verify.sh for
PR https://github.com/fluid-cloudnative/fluid/pull/6267, and report the
per-check table."

Production code is untouched by this harness (diff is additive under
`docs/verification/`).
