# gomega-1-44-bump — verification (PR #6231, Reviewer B / Codex)

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6231
(Dependabot: `github.com/onsi/gomega` 1.38.2 → 1.44.0, transitive `go.yaml.in/yaml/v3` 3.0.4 → 3.0.5).

Reviewed head: `b4c8873606811ec738408842834c456c267efd0c` (base `91a58c1d9d473c6063be7817f398a048233de0f2`).
Production code untouched by this branch — only `docs/verification/**` is added.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 0. Supply chain | go.mod/go.sum/vendor consistency; vendor vs upstream module zips | `go mod verify`; `go mod tidy && go mod vendor` → expect empty diff; `diff -rq vendor/... $(go env GOMODCACHE)/...` |
| 1. Unit | full build + vet + CI-equivalent unit sweep; gomega delta harness | `go build ./...`; `go vet ./...`; `go test $(go list ./... \| grep -v vendor \| grep -v controller \| grep -v e2etest)`; `go test ./docs/verification/gomega-1-44-bump/harness/` |
| 2. Integration | envtest controller suites that use gomega | `KUBEBUILDER_ASSETS=<envtest-bin-dir> go test -count=1 ./pkg/controllers/v1alpha1/dataprocess/ ./pkg/controllers/v1alpha1/alluxio/` |
| 3. Live | skipped per round setup (CI kind-e2e/backward-compat cover it) | — |

## Problem premise (P0)

| | |
|---|---|
| Claimed change | Dependabot bump of a **test-only** matcher library; no linked issue, no runtime bug claim |
| Reported component | `github.com/onsi/gomega` (661 `*_test.go` importers, **0** non-test importers) |
| Patched component | `go.mod`, `go.sum`, `vendor/github.com/onsi/gomega`, `vendor/go.yaml.in/yaml/v3`, `vendor/modules.txt` — nothing else |
| Component match | Yes |
| **Verdict** | **Confirmed** (legitimate maintenance bump; premise = "does not break the suite", validated below) |
| Evidence | results/l0-*, results/l1-* |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | Bump is vendor-consistent, matches upstream zips, suite unaffected | 0+1 | Confirmed | `go mod verify` OK; re-vendor diff empty; vendor byte-identical to `gomega@v1.44.0`/`yaml/v3@v3.0.5` module cache (results/l0-*.txt) |
| F1 | `pkg/controllers/v1alpha1/dataprocess` envtest suite can never boot: `BeforeSuite(..., 60)` (suite_test.go:75) is a ginkgo-v1-style timeout arg; ginkgo v2.26.0 aborts with `unknown decorator: '60'` | 2 | Confirmed, **pre-existing** (fails identically on base and head; ginkgo untouched by PR) | local run base+head; invisible to CI because `make test` excludes `controller` paths |
| F2 | `pkg/controllers/v1alpha1/alluxio` envtest suite can never boot: `BeforeSuite(NodeTimeout(60*time.Second), func(){...})` (suite_test.go:51) lacks a `SpecContext`-accepting callback; ginkgo v2 aborts | 2 | Confirmed, **pre-existing** (fails identically on base and head) | same runs |
| F3 | gomega v1.44.0 flips signed/unsigned `BeNumerically` semantics (onsi/gomega#925) | 1 | Confirmed delta, **no fluid impact** | `TestBeNumericallySignedUnsignedDelta` PASS on head / FAIL on base (`Expected <int>: -1 not to be == <uint64>: 18446744073709551615`) — harness-bites proof (results/l1-delta-*.txt) |

## Fluid's exposure to gomega v1.44.0 behavior changes (inventory at head)

| Upstream change | Fluid usage | Intersection |
|---|---|---|
| #925 BeNumerically signed/unsigned by value | 35 call sites, all `>`,`>=`,`<=`,`<` with int-compatible literals or same-type vars; no mixed signedness | none |
| #927 `BeNumerically("==", x, threshold)` float threshold | 0 uses of `"=="` with threshold | none |
| #928 `BeNumerically("~")` overflow | 0 uses of `"~"` | none |
| #926/#929 HaveKey/HaveKeyWithValue matcher keys | 216 + 89 call sites, all literal string keys/values | none |
| #930/#931 MatchJSON large numbers | 0 uses | none |
| #933 MatchYAML multi-doc | 0 uses | none |
| #932 MatchXML namespaces | 0 uses | none |
| #934 HaveExactElements messages | 0 uses | none |

`TestBeNumericallyFluidPatterns` + `TestHaveKeyWithValueLiteralPatterns` replicate every
call shape fluid uses and PASS on BOTH gomega versions (results/l1-delta-head.txt,
results/l1-delta-base.txt).

## Local environment note (NOT a PR finding)

On this host (go1.27.0 / also reproduced with pinned go1.26.10), 4 packages fail in the
CI-equivalent sweep — `pkg/utils/helm` (7 specs), `pkg/ddc/cache/engine` (12 specs),
`pkg/ddc/alluxio` (1 spec), `pkg/utils` (`TestCheckMountPointBroken`) — all gomonkey-based
exec patches that lose to inlining locally. Identical failure sets on base and head, all 4
pass with `-gcflags="all=-l"`, and CI's `unittest` job is green at this head
(`ok pkg/utils/helm`, `ok pkg/ddc/cache/engine`, `ok pkg/ddc/alluxio`, `ok pkg/utils` in run
42175241040/job 127752082214). Attribution: local environment, pre-existing, not PR-caused.

CI at head b4c88736: unittest, build (amd64+arm64), lint, staticcheck, CodeQL, SonarCloud,
DCO, kind-e2e (k8s 1.22–1.33), backward-compat (k8s 1.22–1.33) all SUCCESS; only `tide`
pending (merge-bot state, not a check).

## Continuing after changes (re-verify)

From a checkout of this branch:

```bash
bash docs/verification/gomega-1-44-bump/scripts/re-verify.sh            # auto-discovers current PR head via manifest.pr
```

Expected this round: F3 Fixed-by-design (contract passes), F1/F2 Still-broken (pre-existing
suite bugs — they fail on master too; note both suites run a test named `TestAPIs`, so the
matcher conflates them until both boot cleanly). Harness-bites: graft
`docs/verification/gomega-1-44-bump/harness/` onto base `91a58c1d` and run
`go test ./docs/verification/gomega-1-44-bump/harness/` — `TestBeNumericallySignedUnsignedDelta`
must FAIL there (proves version sensitivity) while the two fluid-pattern contracts PASS.

Prereqs: Go toolchain per go.mod (1.26+), envtest binaries for L2
(e.g. `setup-envtest use 1.31.0`), network for module downloads.
