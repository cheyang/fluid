# Verification — juicefs hierarchical imagePullSecrets (PR #6245, Reviewer A / Claude)

Branch: `verify/juicefs-imagepullsecrets-claude`, based on PR head
`6711374b5ccbdf0371069b4cc9c0ca9593891528` (base `ff99142f9ec5c32a0c9166de023cb8936f0e5ae5`).
Production code is **untouched** — the only additions are this directory and
`pkg/ddc/juicefs/imagepullsecrets_harness_test.go`.

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

All polarity: **contract tests** (fail on broken code, pass on correct code).

## Observed vs expected

| claim | verdict | evidence |
| --- | --- | --- |
| P0 premise (feature absent at base) | Confirmed | `results/premise-base-evidence.txt` |
| C1 controller precedence + marshaled keys | Confirmed (5/5 scenarios pass) | `results/integration-ginkgo-report.json` |
| C2 chart render fallback (worker/fuse) | Confirmed (helm template renders expected pod secrets) | same report; `results/values-dumps/` |
| C3 back-compat for env-only installs | Confirmed (s1 identical to base behavior) | same report |
| Full package regression (182 specs incl. the PR's own 3 tests) | Pass | `results/unit-go-test.txt` |

## Harness bites (proven, then reverted)

- Sabotage A: removed the worker-override block from `pkg/ddc/juicefs/transform.go`
  → s3/s4 fail (+ the PR's own "prefers the runtime and worker secrets").
  `results/harness-bites-sabotageA-transform.txt`
- Sabotage B: reverted the two chart template expressions to
  `{{- with .Values.imagePullSecrets }}` (base form) → s3/s4/s5 fail at the
  pod imagePullSecrets assertions. `results/harness-bites-sabotageB-chart.txt`

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
