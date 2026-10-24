# pr6214-compress-bump-codex — dependency-bump verification

Reproducible evidence for the review of
[fluid-cloudnative/fluid#6214](https://github.com/fluid-cloudnative/fluid/pull/6214)
("chore(deps): bump github.com/klauspost/compress from 1.17.11 to 1.18.7", dependabot).

- Reviewed head: `b4d0c2f04b4c7cfc6fc34418ea5894d3e8229f58`
- Base (merge-base): `86e3adf35a7c26875e818edad3e01dfac15f0290`
- Harness branch: `verify/pr6214-compress-bump-codex` (based on the head above; production/vendor code untouched — only this docs dir + `test/verification/pr6214compress/` are added)

Layers run against the **code under review** (head `b4d0c2f0`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | module/vendor integrity (`go mod download` + `go mod verify` + `go mod vendor` regeneration diff), full-repo build, zstd encoder contract incl. Reset-reuse and the 0-byte boundary, vendored-version guard | `bash docs/verification/pr6214-compress-bump-codex/scripts/run_full_checks.sh` |
| 2. Integration | the real consumer path: `promhttp.HandlerFor` over `httptest`, Accept-Encoding negotiation, vendored zstd decode round-trip | `go test ./test/verification/pr6214compress/ -run 'TestPromhttp' -count=1 -v` |
| 3. Live | skipped for this debate round (per instructions); covered by the PR's green kind-e2e CI on k8s v1.22–v1.33 | — |

> Test polarity: all behavioral tests are **contract** tests (assert intended behavior). They
> must PASS on both base (v1.17.11) and head (v1.18.7); a head failure would be a regression
> introduced by the bump. `TestVendoredCompressVersion` is a contract on the post-bump state:
> it FAILS on base by design (that expected failure is its harness-bites proof) and must PASS
> on every head >= the bump.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | Dependabot PR body: "Bumps github.com/klauspost/compress from 1.17.11 to 1.18.7." Release notes quote upstream fixes: "Fix s2.NewDict OOB read" (v1.18.7, security release), "s2: Fix amd64 stack frame corruption" (v1.18.6), "zstd: Fix crash when changing encoder dictionary with same ID", "zstd: Default to full zero frames" (v1.18.5), "Downstream CVE-2025-61728" (v1.18.3). No linked issue; no closing keyword. |
| Linked issue | none (dependabot maintenance/security bump) |
| Reported component | `github.com/klauspost/compress` (indirect dependency, vendored) |
| Patched component | `go.mod`, `go.sum`, `vendor/github.com/klauspost/compress/{fse,huff0,internal/cpuinfo,internal/le,internal/snapref,zstd}` |
| Component match | Yes — the PR touches exactly the vendored module it claims to bump |
| **Verdict** | **Confirmed** (the base genuinely vendors v1.17.11 lacking the upstream fixes), with a scope nuance: **s2, gzhttp and flate are not in fluid's compiled package set** (absent from `vendor/modules.txt` at both base and head), so the v1.18.6/v1.18.7 s2 security fixes — the headline motivation — do not change fluid binaries. The compiled delta is zstd/fse/huff0, reached only through prometheus/client_golang's promhttp metrics handler (`zstd.NewWriter(rw, zstd.WithEncoderLevel(zstd.SpeedFastest))`, no dictionaries — so even the zstd dict-crash fix is not on a path fluid executes). Real, low-urgency maintenance. |
| Evidence | `results/premise-p0.txt` |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | base vendors compress v1.17.11 missing upstream fixes; bump is real | 1 | Confirmed | `results/premise-p0.txt` |
| C1 | vendored tree at head is exactly upstream v1.18.7 (`go mod vendor` regenerates byte-identical tree; `go mod verify` OK) | 1 | Confirmed | `results/head-vendor-integrity.txt` |
| C2 | whole repo builds at head with vendored deps (`go build ./...` exit 0; module requires go 1.24, repo toolchain go1.25.12) | 1 | Confirmed | `results/head-go-build.txt` |
| C3 | promhttp metrics path negotiates zstd and round-trips through the vendored decoder (incl. empty-payload boundary hit by the v1.18.5 "full zero frames" default change) | 2 | No regression (pass on base AND head) | `results/head-go-test.txt`, `results/base-go-test.txt` |
| C4 | gzip negotiation path unaffected (control) | 2 | No regression | same files |
| C5 | bump landed and is guarded: vendored version >= 1.18.7 | 1 | head PASS / base FAIL (expected; proves the guard bites) | same files |
| H0 | harness bites: mutating promhttp to drop zstd negotiation makes C3 fail | 2 | Confirmed | `results/harness-bites-mutation.txt` |

## Per-finding detail

This PR introduced no defect findings. The contracts above are the no-regression evidence for
the only compiled consumer path. The full observed-vs-expected matrix:

| Test | Base (v1.17.11) | Head (v1.18.7) | Meaning |
|------|-----------------|----------------|---------|
| TestPromhttpZstdRoundTrip | PASS | PASS | consumer path healthy on both |
| TestPromhttpZstdEmptyRegistry | PASS | PASS | 0-byte frame change decodes fine on both |
| TestZstdSpeedFastestResetReuse | PASS | PASS | NewWriter+Reset reuse round-trips 0B/1B/24B/1MiB |
| TestPromhttpGzipControl | PASS | PASS | control path unchanged |
| TestVendoredCompressVersion | FAIL (expected) | PASS | bump landed; guard bites |

## Proposed fixes

None — no defect found. Non-blocking observations are recorded in the review findings
(findings-codex.md): the s2 security fixes do not reach fluid's compiled code (informational),
and fluid has no in-repo test for its metrics endpoint compression (pre-existing; this harness
covers it on the verify branch).

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/pr6214-compress-bump-codex` (production code untouched).

```bash
# fresh machine, from a clone of the reviewer's fork or upstream:
git fetch https://github.com/cheyang/fluid.git verify/pr6214-compress-bump-codex
git checkout verify/pr6214-compress-bump-codex
# one-line re-verification against the current PR head (auto-resolved from manifest.pr):
bash docs/verification/pr6214-compress-bump-codex/scripts/re-verify.sh
# deeper checks (build + vendor regeneration diff + tests):
bash docs/verification/pr6214-compress-bump-codex/scripts/run_full_checks.sh
```

Prerequisites: Go >= 1.25 (module requires go 1.24; repo toolchain directive is go1.25.12),
network access for `go mod download`/`go mod vendor` in the integrity check. No cluster needed
(L3 skipped).

Polarity guide for future rounds: every test except `TestVendoredCompressVersion` must keep
PASSING on any future head; a flip to FAIL is a regression. `TestVendoredCompressVersion` must
PASS as long as the vendored version stays >= v1.18.7 (adjust the constant if a later review
deliberately pins a higher floor).

Kickoff prompt for a fresh agent: "Fetch branch verify/pr6214-compress-bump-codex from
https://github.com/cheyang/fluid.git, read docs/verification/pr6214-compress-bump-codex/README.md,
run scripts/re-verify.sh, and report Fixed/Still-broken per contract."
