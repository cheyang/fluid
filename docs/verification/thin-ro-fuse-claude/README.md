# Verification — thin: keep `ro` in fuse options when the profile defines options

- **PR:** https://github.com/fluid-cloudnative/fluid/pull/6274 (head `6fed668e8b4a8cfb51b48aae8cb5e57309478e7c`)
- **Issue:** #6273 — `[BUG] ThinRuntime: the "ro" fuse option is dropped when the ThinRuntimeProfile defines fuse options`
- **Merge-base:** `896d21e945e6513ef4dd0e452ea9ec154acb0414`
- **Reviewer branch:** `verify/thin-ro-fuse-claude` (pushed to the reviewer's fork `cheyang/fluid`)
- **Layers run:** unit only (round policy for this debate: unit + integration; no live/KUBECONFIG layer). The integration layer adds nothing here — the claim is decided entirely by deterministic rendering logic against a fake client.

## Premise (P0) — CONFIRMED

Claim (from issue #6273, quoted):

> `ThinEngine.parseFuseOptions` starts with `ro` when the dataset is ReadOnlyMany (the default when `accessModes` is empty), then does `options = profile.Spec.Fuse.Options` when the ThinRuntimeProfile has any fuse options. That replaces the map, so `ro` is lost and the fuse container's `MOUNT_OPTIONS` has no `ro`: a read-only dataset is mounted read-write. The assignment also makes `options` the profile's own map, so the runtime options that are copied in afterwards are written into the profile object.

Reproduced on the **merge-base** (`896d21e`, no patch) by grafting this harness onto it:

- `results/base-harness-repro.txt` — **1 Passed | 3 Failed**
  - `contract: the rendered fuse container MOUNT_OPTIONS keeps ro …` — FAILED (no `ro` in `MOUNT_OPTIONS`)
  - `contract: runtime fuse options must not be written into the profile's option map` — FAILED (profile map gained `option3=value3`)
  - `contract: profile options are merged, they cannot silently drop ro` — FAILED
  - `contract: no ro is added for a writable dataset with profile options` — PASSED (guard: correct on base too)
- `results/head-harness.txt` — all four PASS on the PR head.

The premise is confirmed on exactly the component the patch touches (`pkg/ddc/thin/transform_fuse.go`, `ThinEngine.parseFuseOptions`), and the symptom matches. Component match: yes.

Note on the second half of the claim (profile-map aliasing): `utils.GetThinRuntimeProfile` reads via `client.Get` and the profile is retained on `engine.runtimeProfile` across a reconcile, so writing runtime options into `profile.Spec.Fuse.Options` is not merely cosmetic — the runtime's options accumulate into the profile object the engine keeps using, and any other runtime sharing that cached object would see them. The unit layer makes the mutation itself observable; no cluster is needed to prove it.

## Observed vs expected

| ID | Claim | Layer | Base (`896d21e`) | Head (`6fed668`) | Verdict |
| --- | --- | --- | --- | --- | --- |
| P0 | premise: `ro` dropped + profile map aliased | unit | 3 FAIL / 1 PASS | 4 PASS | **Confirmed** (base), **Fixed** (head) |
| `ro-drop-mount-options` | read-only dataset + profile options ⇒ `ro` present in `MOUNT_OPTIONS` | unit | FAIL | PASS | **Fixed** |
| `profile-map-aliasing` | runtime options not written into `profile.Spec.Fuse.Options` | unit | FAIL | PASS | **Fixed** |
| `writable-no-ro-guard` | writable dataset (ReadWriteMany) ⇒ no `ro` | unit | PASS | PASS | guard holds on both |

Polarity: all tests are **contract** tests (red = bug present, green = fixed). No canaries.

The harness-bites check is the base column itself: the tests fail for exactly the claimed reason on the unpatched merge-base (the PR's proposed fix = the patch under review, so the red/green differential is base vs. head, with the production diff of this branch remaining empty apart from the additive test file).

## How to run

```bash
# from a checkout of verify/thin-ro-fuse-claude
go test -gcflags=all=-l -count=1 -timeout 300s -v ./pkg/ddc/thin/ \
  -ginkgo.focus 'fuse option verification harness'

# re-run against the current PR head (fetches it, grafts the harness, reports per finding)
bash docs/verification/thin-ro-fuse-claude/scripts/re-verify.sh
```

The PR's own added specs (focused with `-ginkgo.focus "parseFuseOptions"`, output in `results/head-pr-tests.txt`) also pass on head: 5 specs, all green.

## Findings carried into the round ledger

- `ro-drop-mount-options` — the bug the PR fixes; fixed on head (differential evidence).
- `profile-map-aliasing` — second bug fixed by the same one-line change; fixed on head (differential evidence).
- Non-blocking review observations (behavior-change note on upgrade, missing transformFuse-level assertion in the PR's own tests, dead `dataset` parameter, non-deterministic option order) live in the round's findings document, not in this harness — none of them is a defect introduced by the PR.

## Continuing after the fix / next round

```bash
git clone https://github.com/cheyang/fluid.git && cd fluid
git checkout verify/thin-ro-fuse-claude
bash docs/verification/thin-ro-fuse-claude/scripts/re-verify.sh   # resolves the PR head from manifest.pr
```

Per-finding verdicts print as Fixed / Still-broken / Partial / Harness-update. All tests are contract polarity, so "fixed" == green. After each round, advance `.last-reviewed` to the reviewed head (re-verify.sh prints the exact command).

Kickoff prompt for a fresh agent: *"You are resuming the review verification of fluid PR #6274 from the branch verify/thin-ro-fuse-claude. Run docs/verification/thin-ro-fuse-claude/scripts/re-verify.sh, read the README there, report per-finding Fixed/Still-broken/Partial, then review the last-reviewed..head delta."*
