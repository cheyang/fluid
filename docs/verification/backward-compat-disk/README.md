# Verification — backward-compat-disk — PR fluid-cloudnative/fluid#6002

Reviewer: **Claude (Reviewer A)** · Round: **initial (2026-11-06)** · Head: `27581121` · Base: `da69206`

## Premise verdict (P0): CONFIRMED — at step level

**Claimed symptom (from PR body):** "The `backward-compat-test` CI job was failing with
`no space left on device` when loading Docker images to the kind cluster."

**Evidence (GitHub Actions API, no cluster needed):**

| run | code | result |
| --- | --- | --- |
| 27287867871 (branch `policy`, 2026-06-10, pre-PR code) | base-era | **all 5 matrix legs FAIL at step `Build current fluid docker images`**; e2e step skipped |
| 27496321566 (PR head `27581121`, 2026-06-14) | patched | **all 5 legs PASS** (build step + e2e) |

The failing step on base is exactly the step this PR modifies (`.github/scripts/build-all-images.sh`
runs there). Job logs from June are HTTP 410 (expired), so the literal `no space left on device`
string can no longer be re-read; the confirmation is step-level + all-legs-pattern, plus the PR
head turning green. Component match is exact (workflow + script named in the report are the files
patched). Verdict: **Confirmed**, mechanism message not directly re-verified.

One inaccuracy in the PR body worth noting: it claims the workflow "first pulls old Fluid images
via `helm install` … before building and loading 10 new images". In the actual workflow,
`Build current fluid docker images` runs **before** `gha-backward-compatibility.sh` (which does the
`helm install` of old Fluid). The disk-exhaustion mechanism remains plausible (10 built images +
build cache + kind node's own copies), but the narrative ordering is inverted.

## CI status note (round context said "failure")

The only failing check on the PR head is the `lint` job (Project Check workflow). It also fails on
the merge base `da69206` and on this PR's *empty* first commit `02be5ca` — it is a Go-code-only
lint job (golangci-lint, CRD/openapi validation, gofmt) and this PR touches no Go code.
**Attribution: pre-existing, not caused by this PR.**

## Observed vs expected

| id | claim | layer | polarity | observed (head) | observed (base) | verdict |
| --- | --- | --- | --- | --- | --- | --- |
| P0 | backward-compat build step fails on base | ci-observation | — | passes (5/5 legs) | fails (5/5 legs, build step) | Confirmed (step-level) |
| F1 | after each `kind load docker-image`, `docker rmi <img>` frees the host copy inside the loop | unit (mock) | contract | PASS: 10 rmi, each after its load | FAIL: 0 rmi | Confirmed (differential) |
| F2 | failing `docker rmi` does not abort the script (`set -e` safe) | unit (mock) | contract | PASS: exit 0, 10 warnings | n/a | Confirmed (code-path) |
| F3 | `Free disk space` step present, after cluster creation, before build; removes dotnet/android/ghc/CodeQL/ghcup + `docker image prune` | unit (yaml) | contract | PASS | FAIL (step missing) | Confirmed (differential) |
| F4 | shared script also used by `kind-e2e.yml` — change must not break it | ci-observation | contract | kind-e2e 5/5 legs green on head | — | Confirmed |

Raw outputs: `results/l1-head.out`, `results/l1-base.out`, `results/l1-rmi-fail.out`,
`results/l1-workflow-head.out`, `results/l1-workflow-base.out`, `results/ci-evidence.md`.

**Harness bites:** every contract test above was run against the **base** versions
(`git show da69206:<path>`) and fails there — the tests are red for the right reason, not vacuous.
`docker image prune --all --force` in the new step cannot remove the `kindest/node` image because
the step runs *after* cluster creation (the node container holds it in use) — checked in
`l1_workflow_contract.py` as a position assertion.

## Why mock stubs are the right L1 layer here

The change under review is a bash loop plus a workflow step. Its contract is the *sequence of
external commands* (`kind load` → `docker rmi`) and exit-code behavior under `set -e`, both of
which are fully decidable with PATH stubs for `kind`/`docker`/`make` (real `git`/`grep` run against
the actual Makefile). No docker daemon or kind cluster exists on the review host, and a real
cluster would not sharpen these assertions. The end-to-end layer for a CI-file PR *is* the CI run,
observed via the Actions API (see `results/ci-evidence.md`).

## How to run

```bash
# from any checkout of the harness branch (or grafted onto the PR head):
bash docs/verification/backward-compat-disk/scripts/re-verify.sh            # current PR head
bash docs/verification/backward-compat-disk/scripts/re-verify.sh <sha>      # any ref

# individual layers:
bash   docs/verification/backward-compat-disk/scripts/l1_mock_harness.sh "$(git rev-parse --show-toplevel)"
bash   docs/verification/backward-compat-disk/scripts/l1_mock_harness.sh "$(git rev-parse --show-toplevel)" --rmi-fail
python3 -I docs/verification/backward-compat-disk/scripts/l1_workflow_contract.py \
    .github/workflows/backward-compatibility-e2e.yml
```

## Continuing after the fix / next round

`re-verify.sh` (no args) fetches the current PR head from the manifest `pr` URL, grafts this
harness onto it in a temp dir, re-runs L1, and prints Fixed / Still-broken / Harness-update per
finding. All tests here are **contract** polarity (no canaries): they must PASS while the fix is
in place; a FAIL means regression or harness-drift (code shape changed → adjust stubs, re-run).
`F4` needs the head's CI run: all 5 `backward-compat-test` legs **and** all 5 `kind-e2e-test` legs
green (the shared script constraint). Advance `.last-reviewed` to the reviewed head after each
round.

Copy-paste kickoff for a fresh agent:

> Checkout branch `verify/backward-compat-disk-claude` (reviewer fork cheyang/fluid). Read
> `docs/verification/backward-compat-disk/README.md` and `verify-manifest.json`. Run
> `bash docs/verification/backward-compat-disk/scripts/re-verify.sh` with no argument — it
> resolves the current PR head for https://github.com/fluid-cloudnative/fluid/pull/6002 itself.
> Report per-finding Fixed/Still-broken/Partial/Harness-update; all tests are contract polarity.
> For F4, check the head commit's `backward-compat-test` and `kind-e2e-test` check conclusions
> via the GitHub API. Do not push to the PR author's fork.

## Production code untouched

The only files added on this branch live under `docs/verification/backward-compat-disk/`.
`git diff 27581121 -- . ':!docs/verification'` is empty by construction.
