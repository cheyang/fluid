# pr-6300-en-data-toleration — verification

Reproducible evidence for the review of
https://github.com/fluid-cloudnative/fluid/pull/6300
("docs: add the English dataset toleration sample and fix its StatefulSet apiVersion").

Layers run against the **code under review** (head `81f467b58d15796a5dc87b2641f8fe4899df81bd`,
base `b8ff60e309c341b58a9e26b085b76e6067e6bb60`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | doc content contracts (code-block parity en/zh, en TOC entry + placement, embedded YAML vs real API types, no removed k8s APIs) | `go test ./docs/verification/pr-6300-en-data-toleration/ -run 'Test(CodeBlocksMatchChineseSource\|EnglishTOCListsPage\|DocManifestsParseAgainstRealAPITypes\|NoRemovedKubernetesAPIs)' -v` |
| 2. Integration | the real docs build tooling (`docs/scripts/mergeByTOC.py`, used by `docs/build.sh` / `docs/Makefile`) on a copied docs tree | `go test ./docs/verification/pr-6300-en-data-toleration/ -run 'TestDocsMergeIncludesPage' -v` |
| 3. Live | not applicable (docs-only PR) | — |

> Test polarity: all five tests are CONTRACT tests — they assert the intended behavior of
> this PR, so they FAIL on the merge-base and PASS on the PR head.

## Problem premise (P0)

This is a documentation PR (translation + doc fix), so the premise check is the factual one
the PR body itself states; it was verified against the **base** branch.

| | |
|---|---|
| Claimed symptom | "`docs/zh/samples/data_toleration.md` (数据容忍污点调度) shows how `Dataset.spec.tolerations` lets the cache run on tainted nodes. It has no English version, and English readers can't find the feature." Plus: the sample StatefulSet uses `apps/v1beta1`, "Kubernetes removed that API in 1.16, so `kubectl create -f app.yaml` fails"; and a stray `kubectl taint nodes node1 key=value:NoSchedule` line sits outside any code block. |
| Linked issue | none (`NONE` per PR body) |
| Reported component | docs: `docs/zh/samples/data_toleration.md`, missing `docs/en/` counterpart |
| Patched component | docs: adds `docs/en/samples/data_toleration.md`, one en TOC line, two zh fixes |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | At base `b8ff60e3`: `docs/en/samples/data_toleration.md` does not exist; `docs/en/TOC.md` has 0 `data_toleration` links while `docs/zh/TOC.md:24` has it; zh page line 120 = `apiVersion: apps/v1beta1`, line 29 = the stray taint line. The documented feature is real: `api/v1alpha1/dataset_types.go:146` `Tolerations []v1.Toleration`, propagated by `pkg/ddc/alluxio/transform_resources.go` (`transformTolerations`) into chart values consumed by `charts/alluxio/templates/{master,worker}/statefulset.yaml`. |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| A1 | The English page's code blocks match the (fixed) Chinese page exactly (the PR body's own verification claim) | 1 | Confirmed (pass on head) | 13/13 fenced blocks identical after CRLF normalization; results/unit-head.out |
| A2 | `docs/en/TOC.md` lists the page under Dataset → Creation, after Cache Co-locality (mirroring zh TOC), and the target exists | 1 | Confirmed | results/unit-head.out; zh TOC line 24 vs en TOC line 20 |
| A3 | Embedded manifests parse against real API types: `Dataset` (tolerations preserved), `AlluxioRuntime` (name pairs with dataset), `apps/v1` StatefulSet (selector == template labels, serviceName set) | 1 | Confirmed | results/unit-head.out |
| A4 | No removed Kubernetes APIs (`apps/v1beta1`/`apps/v1beta2`/`extensions/v1beta1`) remain in either toleration page | 1 | Confirmed | results/unit-head.out |
| A5 | `mergeByTOC.py en/` (the real docs build step) merges cleanly and the merged `doc.md` contains the new page | 2 | Confirmed | results/unit-head.out |

Harness-bites check: grafted onto the merge-base, all five tests FAIL (en page absent,
TOC entry absent, merged doc lacks the page) — see `results/unit-base.out`.

## Per-finding detail

No defects were found in this PR; A1–A5 are acceptance contracts, not bug reports.
Mechanism notes:

- **A1**: blocks extracted with `(?s)```[a-zA-Z]*\n(.*?)``` ` from both pages, CRLF-normalized
  (zh uses CRLF, en uses LF), compared pairwise. 13 blocks, all identical.
- **A3**: heredocs (`cat<<EOF >file ... EOF`) extracted and unmarshalled into
  `datav1alpha1.Dataset`, `datav1alpha1.AlluxioRuntime`, `appsv1.StatefulSet`. The
  StatefulSet check proves the PR body's claim "the manifest already has the `selector`
  that `apps/v1` requires".
- **A5**: runs the shipped `docs/scripts/mergeByTOC.py` on a `t.TempDir()` copy of
  `docs/en` + `docs/scripts`; asserts exit 0 and page presence in the merged output.

## Live run notes

None — docs-only change, no cluster behavior involved.

## Proposed fixes (NOT applied to production here)

None required.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/pr-6300-en-data-toleration-codex`
(fork remote `https://github.com/cheyang/fluid.git`); production code is untouched —
the branch adds only `docs/verification/pr-6300-en-data-toleration/**`.

1. Get it onto updated code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/pr-6300-en-data-toleration-codex
   git checkout <updated-ref>
   git checkout FETCH_HEAD -- docs/verification/pr-6300-en-data-toleration
   ```
   or, from a checkout of the verify branch, simply:
   ```bash
   bash docs/verification/pr-6300-en-data-toleration/scripts/re-verify.sh
   ```
   (no argument: it fetches the current PR head from the manifest's `pr` URL).
2. Prereqs: Go toolchain (module is vendored); `python3` for the integration layer
   (the test skips with a clear message if absent).
3. Re-run the two layer commands above; all five are contract tests and should stay GREEN.
   If a future revision changes the docs structure (renamed files, different TOC layout)
   a red result is a Harness-update: adjust the test, then re-run.
4. Harness-bites: `bash docs/verification/pr-6300-en-data-toleration/scripts/re-verify.sh b8ff60e309c341b58a9e26b085b76e6067e6bb60`
   must report STILL-BROKEN (all five fail on the merge-base).

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/pr-6300-en-data-toleration-codex
(remote https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6300 (docs: English translation of the
dataset-toleration sample + apps/v1beta1 -> apps/v1 fix in the zh page) produced no
defects; five acceptance contracts (A1-A5) were encoded as Go tests under
docs/verification/pr-6300-en-data-toleration/. Read that directory's README.md and
re-verify.sh; run `bash docs/verification/pr-6300-en-data-toleration/scripts/re-verify.sh`
(no args — it fetches the current PR head) and report an observed-vs-expected table.
All tests are contract polarity (must PASS on the PR head, FAIL on merge-base).
```
