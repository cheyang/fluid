# Verification — gomega 1.38.2 → 1.44.0 (fluid PR #6231)

Reviewer: Claude (Reviewer A, PR-debate round 2026-11-07, mode: initial).
Branch: `verify/gomega-bump-1.44-claude`, based on PR head `b4c88736`
(base `91a58c1d`). Production code is untouched — the diff to the PR head is
purely this harness (`docs/verification/gomega-bump-1.44/` + `pkg/gomegacontract/`).

## Premise (P0) — CONFIRMED

The PR is a Dependabot bump of the direct test dependency `github.com/onsi/gomega`
(1.38.2 → 1.44.0) plus its transitive `go.yaml.in/yaml/v3` (3.0.4 → 3.0.5).

- **Base state**: base `go.mod` pins gomega v1.38.2 (`results/premise-base.txt`) — the bump is real and applicable.
- **Scope is exactly what it claims**: the non-vendor diff is two version pins; the vendored gomega and yaml/v3 trees are **byte-identical** to the checksum-verified upstream modules (`go mod download` + `diff -r`, `results/vendor-integrity.txt`; empty diffs). No tampering, no extra changes hidden in vendor.
- **Component match**: no non-test Go file in fluid imports gomega — the change is confined to the test framework layer, so a matcher-semantics change can only manifest as changed test outcomes, not runtime behavior.

## Findings table (observed vs expected)

| ID | Claim | Layer | Verdict | Evidence |
| --- | --- | --- | --- | --- |
| P0 | Premise: outdated dependency, bump applies cleanly, vendor untampered | unit | **Confirmed** | `results/premise-base.txt`, `results/vendor-integrity.txt` |
| V1 | The bump is behaviorally in effect: new matcher semantics hold at head (and are RED at base) | unit | **Confirmed** | `results/contract-head.txt` (all pass) vs `results/contract-base.txt` (all 7 subtests fail on 1.38.2) — the harness bites |
| V2 | Repo compiles with gomega 1.44.0 | unit | **Confirmed** | `results/build-head.txt` (`go build ./...` exit 0) |
| V3 | CI-equivalent unit suite passes at head | unit | **Confirmed (with one environmental exception)** | `results/unittest-head.log`: 23 packages `ok`, 1 package (`pkg/utils/helm`) fails — **fails identically on base** (missing `ddc-helm` wrapper + gomonkey-vs-Go1.27 toolchain artifact; CI, which uses Go 1.26.10, passes it — check `unittest` = SUCCESS on head b4c88736). Not PR-caused. |
| V4 | Controller test packages (excluded from CI's `make test`) still pass at head | integration | **Confirmed except 2 pre-existing broken suites** | `results/controllers-head.log` vs `results/controllers-base.log`: identical 13 ok / 2 FAIL on both base and head → the 2 failures are pre-existing, not introduced by the bump |
| F-alluxio-suite-broken | `pkg/controllers/v1alpha1/alluxio/suite_test.go:51` — `BeforeSuite(NodeTimeout(60*time.Second), func(){…})` without a context-accepting callback → ginkgo refuses to run the whole suite | integration | **Still-broken (pre-existing)** | Fails identically on base and head; broken since it was written; never noticed because CI excludes `*controller*` paths from unit tests |
| F-dataprocess-suite-broken | `pkg/controllers/v1alpha1/dataprocess/suite_test.go:75` — `BeforeSuite(func(){…}, 60)` passes a bare `60` as a ginkgo "decorator" → suite refuses to run | integration | **Still-broken (pre-existing)** | Fails identically on base and head; introduced 2023 (commit 3c723c40f); hidden by the same CI exclusion |
| V5 | No fluid test assertion depends on matcher semantics that changed in the bump | static | **Confirmed** | `scripts/static-guard.sh` passes: `MatchJSON`/`MatchYAML`/`MatchXML`/`HaveExactElements` are unused outside the harness; `HaveKey`/`HaveKeyWithValue` (89/216 call sites) all use literal keys, never a key matcher; all 44 `BeNumerically` call sites are plain int/int64 comparisons, none use the 3-operand threshold form or mix signedness. Both F-alluxio/F-dataprocess and V3/V4 runs are green where they should be. |

## How to run

```bash
# unit layer (static blast-radius guard + gomega semantics contract tests)
bash docs/verification/gomega-bump-1.44/scripts/static-guard.sh
go test ./pkg/gomegacontract/ -v

# integration layer (the two pre-existing broken suites; needs envtest assets)
KUBEBUILDER_ASSETS=~/.local/share/kubebuilder-envtest/k8s/1.31.0-linux-amd64 \
  go test -count=1 ./pkg/controllers/v1alpha1/alluxio/ ./pkg/controllers/v1alpha1/dataprocess/

# full re-verify (grafts this harness onto the current PR head, applies polarity)
bash docs/verification/gomega-bump-1.44/scripts/re-verify.sh
```

## Polarity

All harness tests are **contract tests** for the post-1.44 semantics: they PASS on
gomega ≥ 1.44.0 (the PR head) and FAIL on gomega 1.38.2 (the base — demonstrated in
`results/contract-base.txt`, including the map-iteration-order-sensitive
HaveKeyWithValue case, which happened to fail on base in the recorded run but is
order-dependent there). If a future revision accidentally downgrades gomega or
regresses these matchers, `TestGomegaBumpContractSemantics` goes red.

The two pre-existing findings (F-alluxio-suite-broken, F-dataprocess-suite-broken)
are encoded as contract polarity against `TestAPIs` in the integration layer: they
report STILL-BROKEN until someone fixes the ginkgo decorators, independent of this PR.

## Not attributable to this PR (observed, differential evidence)

- `pkg/utils/helm` local test failures: `exec: "ddc-helm": executable file not found in $PATH`
  → same failure on base and head locally (Go 1.27 toolchain + no ddc-helm on PATH);
  CI's unittest job passes on the head commit (Go 1.26.10). Local environment noise.
- The two broken controller suites above: fail on base and head identically.

## Continuing after the fix / next round

This is a first-round (initial) review; `.last-reviewed` = `b4c88736`. On the next
round, from a checkout of this branch:

```bash
bash docs/verification/gomega-bump-1.44/scripts/re-verify.sh   # auto-fetches PR head
# review the delta: git diff $(cat docs/verification/gomega-bump-1.44/.last-reviewed)..FETCH_HEAD
```

A fresh agent can resume from this branch alone: the manifest's `pr` field resolves
the current PR head machine-independently; envtest assets are machine-local (re-run
`setup-envtest use 1.31.0` if missing); L3/live is not applicable for a test-only
dependency. Expected next-round outcome if the PR is unchanged: V1 fixed (contract
tests green), F-alluxio/F-dataprocess still-broken (pre-existing, non-blocking).

## Merge checklist (reviewer's acceptance criteria)

1. ✅ Non-vendor diff is exactly the version pins; vendored trees byte-identical to checksum-verified upstream modules.
2. ✅ `go build ./...` passes at head.
3. ✅ CI-equivalent unit suite passes at head (environmental helm exception differential-tested against base; CI green).
4. ✅ No fluid test assertion depends on changed matcher semantics (static guard + full suite).
5. ✅ Required CI green on head b4c88736 (unittest, staticcheck, lint, build amd64/arm, kind-e2e ×6, backward-compat ×6, CodeQL, SonarCloud, DCO).
6. ⚠️ Non-blocking, pre-existing: fix the two broken ginkgo suites (alluxio, dataprocess) under a tracking issue — they predate this PR and are invisible to CI.
