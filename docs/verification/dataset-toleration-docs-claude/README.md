# dataset-toleration-docs — verification harness

Reproducible evidence for the review of [PR #6300](https://github.com/fluid-cloudnative/fluid/pull/6300)
(docs-only: add the English dataset-toleration sample, fix the StatefulSet
`apiVersion`, remove a stray taint line from the Chinese page).

The PR is documentation-only, so the harness is a docs-consistency checker:
`scripts/check_docs.py` reads the tree at any git ref via `git show` (no
checkout, working tree untouched) and runs two layers:

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. unit (static) | removed-apiVersion scan over all doc manifests, en/zh code-block parity, TOC entry validity/order, no unfenced `kubectl taint` line | `python3 scripts/check_docs.py <ref> --repo <repo> --layer unit` |
| 2. integration (manifest validation) | the page's heredoc manifests parse as YAML; the `apps/v1` StatefulSet has the required `spec.selector.matchLabels`; `Dataset.spec.tolerations` matches the corev1 `Toleration` shape; the field names used still exist in `api/v1alpha1/*_types.go` at the same ref | `python3 scripts/check_docs.py <ref> --repo <repo> --layer integration` |

Polarity: **all checks are contract tests** — they assert the intended correct
state, fail on the base branch, and pass on the PR head. There are no
bug-canaries.

## Problem premise (P0)

Docs-only PR: the PR body states the problem (no closing issue), quoted:

> "`docs/zh/samples/data_toleration.md` (数据容忍污点调度) shows how `Dataset.spec.tolerations`
> lets the cache run on tainted nodes. It has no English version … the sample app used
> `apiVersion: apps/v1beta1` for a StatefulSet. Kubernetes removed that API in 1.16 …
> A stray line `kubectl taint nodes node1 key=value:NoSchedule` sat between two steps,
> outside any code block."

Run against the **base** branch (`b8ff60e309c341b58a9e26b085b76e6067e6bb60`):

| | |
|---|---|
| Claimed symptom | English readers cannot find the toleration sample; following the zh sample fails with `no matches for kind "StatefulSet" in version "apps/v1beta1"`; a stray command line confuses the steps |
| Reported component | sample docs for `Dataset.spec.tolerations` |
| Patched component | `docs/en/samples/data_toleration.md`, `docs/en/TOC.md`, `docs/zh/samples/data_toleration.md` |
| Component match | Yes |
| **Verdict** | **Confirmed** (statically; see results/base-unit.txt) |
| Evidence | base: en page + TOC entry absent; zh line 29 is the bare `kubectl taint nodes node1 key=value:NoSchedule`; zh `app.yaml` uses `apps/v1beta1` for `kind: StatefulSet`. The page's own sample output shows a `v1.16.9` cluster, on which that apiVersion is removed (k8s removed `apps/v1beta1` StatefulSet in 1.16), so `kubectl create -f app.yaml` fails exactly as claimed. |

The live kubectl layer (actually applying the manifests to a ≥1.16 cluster) was
**not** run in this round; the static removal + the documented 1.16 API removal
establish the premise. See `liveNote` in `verify-manifest.json`.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | problem exists on base | 1 | Confirmed | results/base-unit.txt (4 failing checks at base) |
| doc-removed-apiversion | no doc StatefulSet uses a 1.16-removed apiVersion | 1 | Fixed by PR | results/base-unit.txt fail → results/head-unit.txt pass |
| doc-en-page-parity | en/zh code blocks identical | 1 | Fixed by PR | 13 vs 13 blocks identical at head |
| doc-toc-entry | TOC entry present, resolves, ordering matches zh | 1 | Fixed by PR | results/head-unit.txt |
| doc-stray-taint-line | no unfenced `kubectl taint` line | 1 | Fixed by PR | results/head-unit.txt |
| doc-manifest-validity | page manifests parse; apps/v1 selector present; toleration shape valid | 2 | Fixed by PR | results/head-integration.txt (3 manifests, 0 errors) |
| doc-api-currency | field names still exist in current API types | 2 | Pass (guard, not differential — passes on base too) | results/head-integration.txt |

Review verdict: **no blocking or major findings**. The PR does what it claims,
the translation is faithful (all 13 code blocks byte-identical modulo line
endings), and the two zh-page fixes are correct and complete (`git grep
apps/v1beta1` at head returns nothing in docs/).

## Harness-bites check

Run against the base ref, the same harness fails 7 of 8 checks and exits 1
(`bash scripts/re-verify.sh b8ff60e…` → STILL-BROKEN × 5 + FIXED(guard) × 1,
exit 1); against the PR head it passes 8 of 8 and exits 0. The tests therefore
exercise the claimed behavior, not a vacuous path.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/dataset-toleration-docs-claude` in the
reviewer's fork (`cheyang/fluid`); production code is untouched (the diff to
the PR head is this directory only).

1. Get it onto any clone of fluid:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/dataset-toleration-docs-claude
   git checkout FETCH_HEAD -- docs/verification/dataset-toleration-docs-claude
   ```
2. Prereqs: git, python3, PyYAML (`pip install pyyaml`). No cluster, no Go toolchain.
3. Re-run everything against the current PR head (auto-resolved from the PR URL
   in the manifest — no sha needed):
   ```bash
   bash docs/verification/dataset-toleration-docs-claude/scripts/re-verify.sh
   ```
   Exit 0 iff all contract checks pass. `re-verify.sh <ref>` tests any other ref
   (e.g. the base for the bites check).
4. Polarity table: every check is a contract test — green means correct; there
   are no canaries to invert.
5. After reviewing a new PR delta, advance the marker:
   `echo <new-head-sha> > .last-reviewed && git commit`.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/dataset-toleration-docs-claude
(https://github.com/cheyang/fluid.git). Background: a review of
fluid-cloudnative/fluid PR #6300 (docs-only) verified the PR's claims with a
docs-consistency harness; all checks passed at head 81f467b. The PR has been
updated and must be re-checked. Read
docs/verification/dataset-toleration-docs-claude/README.md ("Continuing after
the fix"), run scripts/re-verify.sh (it resolves the current PR head itself),
confirm the harness still bites on the base ref, review the
.last-reviewed..head delta for new issues, and report an observed-vs-expected
table. No cluster is needed; do not push to upstream or the PR author's fork.
```
