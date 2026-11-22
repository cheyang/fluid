# Verification: PR #6274 — keep the `ro` fuse option when the profile defines options

- PR: https://github.com/fluid-cloudnative/fluid/pull/6274 (fixes #6273)
- Base: `896d21e945e6513ef4dd0e452ea9ec154acb0414` — Head: `6fed668e8b4a8cfb51b48aae8cb5e57309478e7c`
- Harness: additive only; production code untouched (`pkg/ddc/thin/transform_fuse_ro_verify_test.go` + this directory).

## Premise (P0) — CONFIRMED on base

Issue #6273 (OPEN, 2026-11-19) claims `ThinEngine.parseFuseOptions` drops `ro` when the
ThinRuntimeProfile defines any fuse options, mounting a read-only dataset read-write, and
that runtime options are written into the profile's own map. Reproduced on base 896d21e:

- `TestVerifyRoKeptWhenProfileDefinesOptions` FAIL: `MOUNT_OPTIONS="profile-opt1=p1,profile-opt2=p2,runtime-opt=r1"` — no `ro`.
- `TestVerifyProfileOptionsMapNotMutated` FAIL: profile map gained `runtime-opt:r1`.
- The PR's own two new ginkgo specs grafted onto base FAIL the same way.

Component match: report names `parseFuseOptions` in `pkg/ddc/thin`; the PR patches exactly that file.

## Findings and observed-vs-expected

| ID | Claim | Polarity | Base (896d21e) | Head (6fed668) | Verdict |
|----|-------|----------|----------------|----------------|---------|
| P0/F1 | RO dataset + profile options => `ro` kept in MOUNT_OPTIONS | contract | FAIL (`results/base-unit-harness.txt`) | PASS (`results/head-unit-harness.txt`) | bug confirmed on base, fixed on head |
| P0/F2 | runtime options not written into profile's map | contract | FAIL (profile mutated) | PASS | bug confirmed on base, fixed on head |
| F3 | writable (RWX) dataset + profile options => no `ro` | contract | PASS | PASS | guard, no regression |
| F4 | RO dataset + profile setting `rw` => MOUNT_OPTIONS contains both `ro` and `rw` in random order | contract (observation) | n/a (pre-fix profile replaced map) | PASS (`ro,rw`) | minor: contradictory config now yields nondeterministic effective mode (libfuse last-wins); pre-fix it was deterministically (and wrongly) RW |

The PR's own three new specs: fail-before / pass-after verified (`results/base-pr-ginkgo-grafted.txt`
3 passed / 2 failed on base; `results/head-pr-ginkgo.txt` all pass on head).
Full `pkg/ddc/thin/...` suite on head: green (`results/head-full-thin-package.txt`).

F2 production blast radius (base): the profile is fetched via a fresh `client.Get` per call
(`pkg/ddc/thin/util.go:49`, `pkg/utils/runtimes.go:126`), i.e. a deep copy from the informer
cache, so the mutation did not corrupt the shared cache and nothing else in the reconcile reads
`profile.Spec.Fuse.Options` afterwards. The mutation was a latent aliasing bug with limited
impact; the `ro` loss (F1) is the user-facing defect.

## How to re-run

From a checkout of this branch:

```bash
bash docs/verification/thin-fuse-ro-profile-options/scripts/re-verify.sh   # resolves current PR head via manifest.pr
# or explicitly:
go test -gcflags=all=-l ./pkg/ddc/thin/ -run 'TestVerify' -v
```

To re-check the premise against base: graft `pkg/ddc/thin/transform_fuse_ro_verify_test.go`
onto base 896d21e and run the same command; F1/F2 must FAIL there.

## Continuing after the fix

All four tests are contract tests: green = correct behavior. If a future change flips F1/F2 red,
the bug is back. F4 documents current edge-case behavior; if a follow-up makes dataset-RO win
deterministically (e.g. stripping `rw` or applying `ro` last), update F4's assertion.
