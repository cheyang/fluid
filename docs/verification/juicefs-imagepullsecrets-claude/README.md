# Verification — juicefs hierarchical imagePullSecrets (PR #6245, Reviewer A / Claude)

Branch: `verify/juicefs-imagepullsecrets-claude`, rebased onto PR head
`d7e316803a0e4b667ad4f9a19d10c605579e1ad1` (base `ff99142f9ec5c32a0c9166de023cb8936f0e5ae5`;
round 1 head `6711374b5ccbdf0371069b4cc9c0ca9593891528`, round 2 head
`3271f28d7c3bc126d221422abdb9e6e0048eb9c9`).
Production code is **untouched** — the only additions are this directory,
`pkg/ddc/juicefs/imagepullsecrets_harness_test.go`,
`pkg/ddc/juicefs/imagepullsecrets_apidoc_harness_test.go` and
`pkg/ddc/juicefs/sync_imagepullsecrets_harness_test.go` (round 3).

## Premise (P0) — CONFIRMED

The PR claims (via issue #4222, OPEN) that JuiceFSRuntime cannot express
`imagePullSecrets` hierarchically. Confirmed against the **base** branch:
`api/v1alpha1/juicefsruntime_types.go` has no `ImagePullSecrets` anywhere,
`pkg/ddc/juicefs` never reads `Spec.ImagePullSecrets`, and `transformWorkers`
at base only reads the controller env `IMAGE_PULL_SECRETS`. AlluxioRuntime has
the full hierarchy at base, so this is a parity gap. Component match holds.
Evidence: `results/premise-base-evidence.txt`.

## Harness

One ginkgo file, five scenarios, each running the **full** `transform()` (the
same entry point `generateJuicefsValueFile()` uses), asserting:

1. the value-struct precedence (env default < `spec.imagePullSecrets` <
   `spec.worker.imagePullSecrets` / `spec.fuse.imagePullSecrets`);
2. the `sigs.k8s.io/yaml` marshaling (the exact keys the chart templates read);
3. `helm template` of the real `charts/juicefs` with those marshaled values,
   asserting the pod-level `imagePullSecrets` of the worker StatefulSet and the
   fuse DaemonSet (component secrets, else the global fallback).

Requires: `go` and `helm` (v3) on PATH. No cluster needed.

| scenario | env default | spec | worker | fuse | worker pod | fuse pod |
| --- | --- | --- | --- | --- | --- | --- |
| s1-env-default-only | default-secret | – | – | – | default-secret | default-secret |
| s2-runtime-replaces-env-default | default-secret | runtime-secret | – | – | runtime-secret | runtime-secret |
| s3-component-overrides-runtime | default-secret | runtime-secret | worker-secret | fuse-secret | worker-secret | fuse-secret |
| s4-worker-only-no-default | – | – | worker-secret | – | worker-secret | (none) |
| s5-fuse-only-no-default | – | – | – | fuse-secret | (none) | fuse-secret |
| s6 chart-defaults (round 2) | – (pure chart defaults, no `-f`) | – | `[]` declared | `[]` declared | (none) / chart-global-secret via `--set imagePullSecrets[0].name` | same |

All polarity: **contract tests** (fail on broken code, pass on correct code).
Plus one plain Go contract test (round 2):
`TestJuicefsApiDocImagePullSecretsPlacement` in
`pkg/ddc/juicefs/imagepullsecrets_apidoc_harness_test.go` — asserts the
`api_doc.md` row placement (see Round 2 below).

## Observed vs expected

| claim | verdict | evidence |
| --- | --- | --- |
| P0 premise (feature absent at base) | Confirmed | `results/premise-base-evidence.txt` |
| C1 controller precedence + marshaled keys | Confirmed (5/5 scenarios pass) | `results/integration-ginkgo-report.json` |
| C2 chart render fallback (worker/fuse) | Confirmed (helm template renders expected pod secrets) | same report; `results/values-dumps/` |
| C3 back-compat for env-only installs | Confirmed (s1 identical to base behavior) | same report |
| Full package regression (182 specs incl. the PR's own 3 tests) | Pass | `results/unit-go-test.txt` |
| Round 2 on new head 3271f28: full suite | 183/183 ginkgo specs pass (182 + new s6); `go build ./...` clean; `go test ./api/...` ok; the ONLY red is the C4 reproduction | `results/reverify-round2/` |

## Harness bites (proven, then reverted)

- Sabotage A: removed the worker-override block from `pkg/ddc/juicefs/transform.go`
  → s3/s4 fail (+ the PR's own "prefers the runtime and worker secrets").
  `results/harness-bites-sabotageA-transform.txt`
- Sabotage B: reverted the two chart template expressions to
  `{{- with .Values.imagePullSecrets }}` (base form) → s3/s4/s5 fail at the
  pod imagePullSecrets assertions. `results/harness-bites-sabotageB-chart.txt`
- Round 2, api_doc: `TestJuicefsApiDocImagePullSecretsPlacement` is RED on the
  head docs and GREEN after applying the pinned crd-ref-docs v0.3.0 regeneration
  (`make update-api-doc`); docs then restored. `results/apidoc-bites.txt`
- Round 2, s6: reverting the worker template fallback to
  `{{- with .Values.worker.imagePullSecrets }}` (no `| default`) → s6 fails
  together with s1/s2. `results/harness-bites-sabotageC-values-defaults.txt`

## Round 2 (delta 6711374..3271f28, "docs(juicefs): document runtime level imagePullSecrets")

The delta is a docs-focused commit answering round 1's SHOULD items. Findings:

| claim | verdict | evidence |
| --- | --- | --- |
| C1/C2/C3 (round-1 claims) on the new head | Confirmed unchanged — 183/183 ginkgo specs pass | `results/reverify-round2/` |
| C4-apidoc-row-placement | **CONFIRMED DEFECT (still open)** — the three imagePullSecrets rows in `docs/{en,zh}/dev/api_doc.md` are misplaced | `results/apidoc-bites.txt`, `results/apidoc-regen-groundtruth.txt` |

C4 detail: `api_doc.md` is generated (`make update-api-doc`: pinned
elastic/crd-ref-docs v0.3.0 + `cp` zh→en). Round 2 hand-edited it instead of
running the generator:

- the `JuiceFSCompTemplateSpec` row ("...Only the worker uses it for now...") is
  orphaned right after the `#### JuiceFSRuntime` heading — the
  `JuiceFSCompTemplateSpec` table (where `spec.master/worker/jobWorker` fields
  are documented) has **no** imagePullSecrets row;
- the `JuiceFSFuseSpec` row ("...pull the JuiceFS fuse image...") was pasted as
  the first row of the **JuiceFSRuntimeSpec** table — so the runtime-level field
  is documented with the fuse wording, including "If unspecified, the runtime's
  imagePullSecrets are used", which is meaningless for the runtime-level field
  itself; the `JuiceFSFuseSpec` table has **no** imagePullSecrets row;
- the `JuiceFSRuntimeSpec` row ("...for all JuiceFS components...") is orphaned
  right after the Vineyard `#### MasterSpec` heading — a JuiceFS row inside the
  Vineyard MasterSpec section;
- regenerating with the pinned tool (verified locally) puts all three rows in
  the right tables, in struct-declaration order. The same regeneration at the
  BASE commit reproduces the committed base file's non-JuiceFS content
  (zero imagePullSecrets lines in that control diff), proving the toolchain
  matches the repo's; the non-JuiceFS hunks of the head diff (MediumSource area
  etc.) are pre-existing master drift, not this PR's doing.

Fix: run `make update-api-doc` (or `./bin/crd-ref-docs --source-path=api
--output-path=docs/zh/dev/api_doc.md --config=./tools/crd-ref-docs/config.yaml
--renderer=markdown && cp docs/zh/dev/api_doc.md docs/en/dev/api_doc.md`) and
commit the regenerated files. Note the regeneration will also fold in the
pre-existing MediumSource drift — that is the repo's normal update path.

s6 (new scenario) covers the delta's `values.yaml` change: the newly declared
`worker.imagePullSecrets: []` / `fuse.imagePullSecrets: []` must stay "unset"
for the `| default .Values.imagePullSecrets` fallback. Confirmed: chart defaults
render no pod secrets; global-only renders the global secret on both pods.

Both sabotages were reverted; `git status` shows no production changes.

## How to run

From a checkout of this branch:

```bash
go test ./pkg/ddc/juicefs/ -count=1                       # full package sweep
go test ./pkg/ddc/juicefs/ -run TestJuicefs -ginkgo.focus "imagePullSecrets"   # just this harness
# dump the marshaled values fed to helm for offline inspection:
HARNESS_OUT=/tmp/vals go test ./pkg/ddc/juicefs/ -count=1 -run TestJuicefs -ginkgo.focus "imagePullSecrets"
```

Re-verify against the current PR head (or any fixed ref) — the manifest's
`pr` URL resolves the head machine-independently:

```bash
bash docs/verification/juicefs-imagepullsecrets-claude/scripts/re-verify.sh
```

## Continuing after a fix / on another machine

1. Clone fluid, `git fetch` this fork's `verify/juicefs-imagepullsecrets-claude`
   and check it out.
2. Run `bash docs/verification/juicefs-imagepullsecrets-claude/scripts/re-verify.sh`
   (no arguments) — it fetches the current PR head, grafts this harness onto it,
   runs the unit + integration layers, and prints per-claim
   Fixed / Still-broken / Partial / Harness-update.
3. All claims here are contract tests: **fixed ⇔ the listed It entries pass**.
   If the PR renames `transformWorkers` or restructures the value struct, expect
   `Harness-update` — adjust `imagepullsecrets_harness_test.go`, not the claim.
4. If a future change intentionally alters precedence (e.g. merge instead of
   replace for the env default), promote the new behavior to the contract and
   record the decision in the round ledger.

Kickoff prompt for a fresh agent:

> You are continuing the review of fluid PR #6245. Check out branch
> verify/juicefs-imagepullsecrets-claude, read
> docs/verification/juicefs-imagepullsecrets-claude/README.md and
> verify-manifest.json, run scripts/re-verify.sh, and report the per-claim
> verdicts plus the last-reviewed..head delta review.

## Round 3 (delta 3271f28..d7e3168: "regenerate api_doc.md" + "sync image pull secrets to worker and fuse on runtime updates")

Two commits. `16cd329f` regenerates `docs/{en,zh}/dev/api_doc.md` with the pinned
toolchain — exactly the fix round 2 asked for. `d7e3168` implements the sync-path
propagation of imagePullSecrets (round-2 deferred item / tracking issue #6248):
`checkAndSetWorkerChanges` / `checkAndSetFuseChanges` gain an imagePullSecrets
block (component list wins over the runtime-wide one, `apiequality.Semantic`
comparison), the fuse path bumps the fuse generation (same treatment as a fuse
image change, so fuse pods roll), and `SyncRuntime` records the runtime-wide
secrets in the values configmap.

New harness file `pkg/ddc/juicefs/sync_imagepullsecrets_harness_test.go`:
`TestJuicefsSyncImagePullSecretsPropagation` drives the FULL controller entry
point (`JuiceFSEngine.SyncRuntime`, the function `pkg/ddc/base/syncs.go` calls
on every reconcile) against a fake cluster seeded with the previous state
(old configmap value produced by the real `transform()` on the old spec), then
edits the runtime CR the way a user would. Eight scenarios:

| scenario | outcome |
| --- | --- |
| runtime-wide secrets added | both pod templates updated, fuse generation bumped, configmap recorded |
| worker override removed | worker falls back to runtime-wide |
| fuse override replaced | only fuse changes, generation bumped |
| secrets removed entirely | both templates cleared |
| spec removed, env default set | pods revert to the env default (creation-path replacement semantics) |
| global changed under a worker override | worker pods keep the override, fuse moves, configmap records the new global |
| no change | nothing written, no generation bump, idempotent on second sync |
| configmap predating the feature + env default | env default recorded; **observed: one-time fuse generation bump (fuse pod roll) on the first reconcile after controller upgrade** — the pod templates end up with the same secrets the pods already pulled with |

Plus `TestJuicefsSampleDocsSyncClaim`: `docs/{en,zh}/samples/image_pull_secrets.md`
still end their JuiceFSRuntime section with "changing them on an existing runtime
needs the runtime to be recreated" (en) / "修改已有 runtime 的密钥需要重新创建
runtime" (zh) — now contradicted by the sync commit. Contract test: RED on head
(the docs sentence must be updated), GREEN after.

| claim | verdict | evidence |
| --- | --- | --- |
| C4-apidoc-row-placement (round 2) | **FIXED** — regeneration committed; `TestJuicefsApiDocImagePullSecretsPlacement` green on head | `results/reverify-round3/` |
| C5-sync-propagation (#6248) | **FIXED** — contract test fails on 3271f28 (all 8 scenarios) and passes on d7e3168 | `results/reverify-round3/previous-head-sync-repro.txt`, `results/reverify-round3/head-sync-propagation.txt` |
| C6-sample-docs-sync-claim | **OPEN** — stale "recreate the runtime" claim in both languages | `results/reverify-round3/head-sync-propagation.txt` |
| C1/C2/C3 + full package suite | unchanged, green on head (incl. the author's new `TestSyncImagePullSecrets`) | `results/reverify-round3/` |

Polarity: C5's previous-head run doubles as the harness-bites check (removing
the sync code = the round-2 head, where all scenarios fail). C6 bites by being
RED on the committed docs.

## Round 4 (delta d7e3168..be3337d, "fix(juicefs): compare image pull secrets against the live templates")

One commit answering round 3's three desync findings (Copilot-flagged, both
reviewers confirmed by tracing): `checkAndSetWorkerChanges` /
`checkAndSetFuseChanges` now compare the desired effective secrets against the
**live pod template** instead of the saved value, component overrides are
recorded unconditionally, and `SyncRuntime` commits the configmap when only an
override changed (`overridesChanged`). The harness runs via
`scripts/re-verify.sh` (grafts onto the current PR head `be3337dd`).

Harness updates this round (production code still untouched):

- **New** `TestJuicefsSyncSecretsMissingScriptConfigmapRecovers` (C7,
  F-worker-lost-update-missing-scriptcm): pass 1 with the worker-script
  configmap missing observes the transient divergence (sts keeps `[g1]`,
  configmap records `[g2]`, no error); after the configmap is restored, pass 2
  converges the sts to `[g2]`. **RED on d7e3168** (pass 2 was a no-op — the
  permanence round 3 flagged), **GREEN on be3337d**.
- **New** `TestJuicefsSyncSecretsStrategyFlipCatchesUp` (C8,
  F-sync-strategy-flip-advance): pass 1 with a RollingUpdate worker strategy
  flips the strategy and returns; pass 2 converges the worker template. **RED
  on d7e3168**, **GREEN on be3337d**.
- **New** `TestJuicefsSyncSecretsRotationThenOverrideRemoval` (C9,
  F-sync-override-not-recorded): rotation a→b + worker override `a` records the
  override (pass 1); removing the override drops the worker onto `b` (pass 2).
  **RED on d7e3168** on both facets (override never recorded; worker stuck on
  `a`), **GREEN on be3337d**.
- **Harness-update** on the round-3 scenario "configmap predating the feature
  with env default set records the env default": it now asserts the fuse
  generation is **NOT** bumped (a bookkeeping-only catch-up no longer rolls
  fuse pods, since the live template already carries the desired secrets). The
  updated expectation fails on d7e3168 (where the bump was observed), so it
  still bites; the author confirms the change is intentional.

| claim | verdict | evidence |
| --- | --- | --- |
| C1/C2/C3 (precedence, chart fallback, env back-compat) | **unchanged, green** on be3337d | `results/reverify-round4/console-head.txt` |
| C4-apidoc-row-placement | **FIXED** (unchanged from round 3) | same |
| C5-sync-propagation | **FIXED** — all 8 scenarios pass with the round-4 expectations; the three new desync recovery tests pass | `results/reverify-round4/` |
| C6-sample-docs-sync-claim | **OPEN** — `docs/{en,zh}/samples/image_pull_secrets.md` still end the JuiceFSRuntime section with "changing them … needs the runtime to be recreated" (en:108 / zh:104); the fix commit did not touch the docs. This is round 3's single pre-merge MUST, still unmet. | `results/reverify-round4/console-head.txt` |
| C7/C8/C9 desync recovery | **FIXED** on be3337d (RED on d7e3168) | `results/reverify-round4/previous-head-desync-repro.txt` |
| Full package suite on head | 183/183 ginkgo specs pass, `go build ./...` clean, `go test ./api/...` ok; the only red is the C6 docs reproduction; the author's `TestSyncImagePullSecrets` (7 scenarios) + `TestSyncImagePullSecretsRotationThenOverrideRemoval` pass | `results/reverify-round4/full-package-head.txt` |

Residual (nit, non-gating): while the worker-script configmap is missing, the
values configmap still advances past the worker StatefulSet and no error is
surfaced — but the divergence is now **transient and self-healing** (every
reconcile retries; convergence proven by C7), which is what round 3 actually
asked for. CI at head (SonarCloud, DCO) green.

## Round 5 (delta be3337d..2150b49, "docs: describe how image pull secret changes reach existing JuiceFS runtimes")

One commit, docs-only (`docs/{en,zh}/samples/image_pull_secrets.md`, one sentence
each) — exactly the round-3/4 pre-merge MUST: the stale "changing them on an
existing runtime needs the runtime to be recreated" / "修改已有 runtime 的密钥
需要重新创建 runtime" claims are gone, replaced with the actual behavior
(next-sync propagation, fuse pods restarted, worker pods pick the secrets up as
they are replaced, controller env default included, controller-owned so direct
sts/ds edits are reverted). The PR body also now carries `fixes #6248`
(auto-close on merge, verified via `closingIssuesReferences`) and a
special-notes paragraph declaring the env-default live-sync intentional and the
manual-edit enforcement — answering round 4's Q1.

Doc-sentence factual check against the code at head (round 5 review): worker sts
updateStrategy is OnDelete in `charts/juicefs` ("as they are replaced" is
accurate); the fuse roll rides the standard generation/clean-policy mechanism
verified since round 3; transform.go:134-137 + the live-template diff cover the
env-default and enforcement sentences; zh is a faithful translation.

| claim | verdict | evidence |
| --- | --- | --- |
| C6-sample-docs-sync-claim | **FIXED** — `TestJuicefsSampleDocsSyncClaim` RED on be3337d (round 4) → GREEN on 2150b49 (the only red in round 4's package sweep is gone) | `results/reverify-round5/console.txt`, `results/reverify-round5/unit.out` |
| C1/C2/C3/C4/C5/C7/C8/C9 | **unchanged, green** on 2150b49 (re-verify.sh: all findings FIXED, pass counts identical to round 4) | `results/reverify-round5/` |
| Full repo suite on clean head | `go build ./...` clean; `go test ./pkg/ddc/juicefs/...` ok (both packages); `go test ./api/...` ok; no red anywhere | `results/reverify-round5/full-package-head.txt` |
| CI at head | 23/23 checks SUCCESS at 2150b49 incl. build, unittest, lint, staticcheck, CodeQL, SonarCloud, DCO, codecov and the full kind-e2e-test + backward-compat-test matrices (v1.22–v1.33); PR OPEN, MERGEABLE | `results/reverify-round5/ci-head.txt` |

All round-4 action items are settled: [MUST] docs sentence — done; [MUST] CI
green on final head — done; [SHOULD] #6248 auto-close — done via `fixes #6248`;
[SHOULD] env-default/enforcement disclosure — done (PR body + docs). Still open,
all non-gating: maintainer decision on an in-repo chart-render test (round-1
consensus: acceptable to merge without, Alluxio #4242 precedent), the nit about
surfacing an error while the worker command configmap is missing, and Q2 (fuse
negative assertion in the author's leaf test).

## Round 6 (delta 2150b49..a0baafae, "docs: say that syncing image pull secrets doesn't restart running pods")

One commit, docs-only (`docs/{en,zh}/samples/image_pull_secrets.md`, one
sentence each) — the fix for round-5's F-docs-fuse-restart-overstated, now IN
the frozen reviewed head. The round-5 replacement sentence's "the fuse pods are
restarted" / "fuse pod 会重启" claim is replaced with the verified mechanism:
templates update on the next sync, no running pod restarts, fuse DaemonSet is
`OnDelete`, and under the default `OnRuntimeDeleted` clean policy running fuse
pods keep the old secrets until they are deleted — zh in faithful parity.
Correction of this harness's own round-5 record: the round-5 section above said
"the fuse roll rides the standard generation/clean-policy mechanism" and signed
off the docs as accurate — that was the miss Codex's round-5 harness caught;
the mechanism pillars (fuse DS OnDelete in values.yaml; syncFuseSpec flipping
non-OnDelete back and returning before the template sync; SetupFuseCleanPolicy
defaulting to OnRuntimeDeleted) were re-checked at a0baafae and the round-5
wording was wrong on the default configuration.

New this round: **C10-docs-fuse-restart-claim** (contract, unit) —
`TestJuicefsSampleDocsFuseRestartClaim`. This harness lacked the fuse-restart
docs contract in round 5 (the gap Codex's harness covered); it is added
additively now and anchors the mechanism in code (chart values, syncFuseSpec
flip, SetupFuseCleanPolicy default) so a future behavior change makes the
test's premise stale instead of silently pinning a wrong sentence.

| claim | verdict | evidence |
| --- | --- | --- |
| C10-docs-fuse-restart-claim | **FIXED** — contract RED on previous head 2150b49 (C10 STILL-BROKEN pass=0 fail=1; the overstatement present in both languages) → GREEN on a0baafae; mechanism pillars re-read at head | `results/reverify-round6/previous-head-2150b49e-c10-red.txt`, `results/reverify-round6/unit-head.out` |
| C1–C9 | **unchanged, green** on a0baafae (re-verify.sh: all 10 findings FIXED, pass counts identical to round 5) | `results/reverify-round6/unit-head.out`, `results/reverify-round6/integration-head.out` |
| Full repo suite on clean head | `go build ./...` clean; `go test ./pkg/ddc/juicefs/ ./api/...` ok, no red anywhere | `results/reverify-round6/full-package-suite-head.txt` |
| CI at head | 25/25 checks PASS at a0baafae (build x2, unittest, lint, staticcheck, CodeQL, Analyze(go), SonarCloud x2, DCO, codecov x2, backward-compat x5 and kind-e2e x5 across k8s v1.22.17–v1.33.2); PR OPEN, MERGEABLE — round-5's pending CI gate is resolved | `results/reverify-round6/ci-head.txt` |

The two live-layer notes stand (L3 intentionally not run; pipeline is unit +
integration only). All round-5 action items: [MUST] CI green on final head —
done, at the frozen head itself; [SHOULD] re-freeze on a0baafae — done, this
round reviews exactly that head. Still open, all non-gating and unchanged:
maintainer decision on the in-repo chart-render test, the Alluxio follow-up
issue for the #6248 side-note, the surfaced-error nit, and Q2 (fuse negative
assertion in the author's leaf test).
