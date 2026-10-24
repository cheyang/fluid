# compress-bump-claude — dependency-bump verification

Reproducible evidence for the review of [PR #6214](https://github.com/fluid-cloudnative/fluid/pull/6214)
(bump vendored `github.com/klauspost/compress` 1.17.11 → 1.18.7, Dependabot security bump).

Harness branch: `verify/compress-bump-claude` (production code untouched — the diff vs the PR
head is entirely under `docs/verification/compress-bump-claude/`).

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | zstd encode/decode round-trip incl. the upstream #1134 all-zero / empty boundary, and the streaming Write+Close path promhttp uses | `go test -count=1 -v ./docs/verification/compress-bump-claude/harness/ -run 'TestZstd'` |
| 2. Integration | the real user-visible contract: controller-runtime-style promhttp metrics endpoint negotiating Identity/Gzip/**zstd** over real HTTP (httptest), incl. 16 concurrent zstd scrapes (run with `-race`) | `go test -race -count=1 -v ./docs/verification/compress-bump-claude/harness/ -run 'TestPromhttp'` |
| 3. Live | skipped (no cluster needed; see manifest `liveNote` for an optional live signal) | — |

> Polarity: **all contract tests** — they assert intended-correct behavior and must PASS. There is
> no bug-canary in this harness because no defect was confirmed against the PR head.

## Problem premise (P0)

Run against the **base** branch (86e3adf3) without the patch, because the question is whether
the problem exists today.

| | |
|---|---|
| Claimed symptom | Dependabot security alert: `klauspost/compress` pinned at 1.17.11; v1.18.7 release notes: "Fix s2.NewDict OOB read. Security release without other changes." |
| Linked issue | none (Dependabot PR, no `fixes #N`) |
| Reported component | module `github.com/klauspost/compress`, version pinned in go.mod (vendored) |
| Patched component | go.mod/go.sum + `vendor/github.com/klauspost/compress/**` |
| Component match | Yes — the PR bumps exactly the flagged module |
| **Verdict** | **Confirmed** (version-level), with a reachability caveat |
| Evidence | base go.mod pins `v1.17.11`, inside GO-2026-5841 / GHSA-259r-337f-4rfw (CVE-2026-63209) affected range `[1.16.0, 1.18.7)`; `results/premise-base.out`, `results/osv.json` |

Reachability caveat (recorded separately, as the premise does not cover it): the only advisory
against 1.17.11 in the Go/OSV DB affects **package `s2`** only. `s2` is not vendored on base or
head and nothing in the vendored graph imports it — the vulnerable code is never compiled into
any Fluid binary. The bump's security value here is alert hygiene (scanners flag go.mod versions),
not the closing of an exploitable path. The one klauspost package that *is* compiled is `zstd`,
consumed by prometheus/client_golang's `promhttp` (metrics endpoint compression, reached via
controller-runtime's metrics server in every Fluid controller binary), which is exactly what the
harness covers.

## Summary of results

All runs on Go 1.27.0 (go.mod: `go 1.25.0`; compress v1.18.7 requires go 1.24 — compatible).

| ID | Claim | Layer | Verdict (head b4d0c2f0, v1.18.7) | Verdict (base 86e3adf3, v1.17.11) | Evidence |
|----|-------|-------|----------------------------------|-----------------------------------|----------|
| P0 | base pin v1.17.11 is in the advisory's vulnerable range | — | n/a | **Confirmed** (version-level; s2 not compiled) | `results/premise-base.out`, `results/osv.json` |
| build | whole repo builds with the new vendored tree | — | PASS (`go build ./...` exit 0) | PASS | `results/build-head.out` |
| vendor | committed vendor tree is exactly what `go mod vendor` regenerates at v1.18.7 | — | PASS (0 diff, incl. modules.txt) | — | run log in this README's history |
| P1 | every payload round-trips byte-identically through zstd `EncodeAll`/decode, and output is a well-formed frame (incl. empty, 1-byte zero, 64KiB/1MiB zeros = upstream #1134 boundary) | 1 | **PASS** (all 6 subtests, with `-race`) | FAIL — empty subtest only: v1.17.11 `EncodeAll([])` emits **0 bytes** (no frame) | `results/head.out`, `results/base-harness.out` |
| P1b | streaming Write+Close (what promhttp actually does) emits a valid frame and round-trips, incl. the empty payload | 1 | **PASS** (all 3 subtests) | FAIL — empty subtest only: v1.17.11 emits **0 bytes**, i.e. an empty body under `Content-Encoding: zstd` | `results/head.out`, `results/base-harness.out` |
| P2 | metrics endpoint negotiates zstd for `Accept-Encoding: zstd`; zstd body decodes to exactly the identity body; gzip/identity unaffected | 2 | **PASS** (with `-race`) | PASS | `results/head.out`, `results/base-harness.out` |
| P3 | 16 workers × 20 concurrent zstd scrapes all decode identically (covers upstream #1143 pooled hash tables in the encode path) | 2 | **PASS** with `-race` (no data race) | PASS | `results/head.out`, `results/base-harness.out` |

**Differential reading:** the only base/head behavior deltas are the two empty-payload subtests,
and both are in the **fix direction** (v1.18.7 emits a valid empty frame where v1.17.11 emitted
nothing — a scraper receiving `Content-Encoding: zstd` with an empty body could not decode it).
No contract regressed from base to head.

## Harness-bites check

Two independent checks that the tests can actually detect a defect:

1. **Permanent self-control** — `TestHarnessBitesZstdCorruption` flips one byte in the middle of
   an encoded frame and asserts the decode path reports it (error or mismatch).
2. **Temporary sabotage** — vendored `zstd/encoder.go` `EncodeAll` was patched to drop the last
   output byte: `TestZstdRoundTripContract` went red on 5 subtests. Sabotage reverted; suite green
   again; `git status` confirmed zero non-harness changes. Raw output: `results/bite-check.out`.

## Not PR-attributed (local environment noise)

`go test ./pkg/utils/... ./pkg/common/...` on the head has two failures: `pkg/utils` (panic in
`os/exec.findExecutable`, 112s) and `pkg/utils/helm` (ListReleases/ListReleaseMap/… nil-pointer
panics). **Both reproduce identically on the base branch** (same tests, same panics — they shell
out to the `helm` binary), so they are pre-existing/environmental, not caused by this PR.
12 packages pass. Recorded in `results/fluid-unit-subset.out`.

## Per-finding detail

No defect was confirmed against the PR head. The review findings (see the debate document)
are informational; this harness is the regression suite guarding the contract the bump touches:

- **P1/P1b** (unit): `harness/zstd_promhttp_contract_test.go` — `TestZstdRoundTripContract`,
  `TestZstdStreamingContract`. Encode with `zstd.SpeedFastest`, mirroring
  `vendor/github.com/prometheus/client_golang/prometheus/promhttp/http.go:449`.
- **P2/P3** (integration): `TestPromhttpZstdNegotiationContract`,
  `TestPromhttpZstdConcurrentScrapes`, using `promhttp.HandlerFor(registry, HandlerOpts{ErrorHandling: HTTPErrorOnError})`
  — mirroring `vendor/sigs.k8s.io/controller-runtime/pkg/metrics/server/server.go:218`, which is
  how every Fluid controller binary (cmd/alluxio, cmd/dataset, cmd/jindo, cmd/vineyard, …) serves
  metrics.

## Proposed fixes (NOT applied to production here)

None — no defect found. The PR as submitted is the fix for the version-level alert; merge-ready
from this harness's perspective once CI is green.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/compress-bump-claude` (fork `cheyang/fluid`), based on the
PR head; it grafts onto any future head:

```bash
git fetch https://github.com/cheyang/fluid.git verify/compress-bump-claude
git checkout <fixed-ref>
git checkout FETCH_HEAD -- docs/verification/compress-bump-claude/
bash docs/verification/compress-bump-claude/scripts/re-verify.sh   # no arg: resolves current PR head via manifest.pr
```

Prereqs: Go toolchain ≥ 1.24 (repo pins 1.25). Layer 2 needs no cluster — httptest only.
Read results via the polarity table: all tests are contracts, all should be GREEN.
`re-verify.sh` prints per-ID Fixed/Still-broken/Partial and exits 0 iff everything passes.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/compress-bump-claude (fork cheyang/fluid).
Background: PR https://github.com/fluid-cloudnative/fluid/pull/6214 bumps the vendored
klauspost/compress 1.17.11 -> 1.18.7; the contract harness found no defect at head b4d0c2f0.
Read docs/verification/compress-bump-claude/README.md ("Continuing after the fix") and follow
it: graft the harness onto the current PR head, run scripts/re-verify.sh (all contracts must
pass; P1/P1b empty-payload subtests were red on the pre-bump base by design), run the
harness-bites check, and report an observed-vs-expected table. No cluster is needed.
```
