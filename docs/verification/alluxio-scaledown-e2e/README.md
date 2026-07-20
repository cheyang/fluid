# Verification — PR #6061: re-land AlluxioRuntime graceful worker scale-down

- **PR:** https://github.com/fluid-cloudnative/fluid/pull/6061
- **Title:** `feat: re-land graceful scale-down for AlluxioRuntime with e2e test`
- **Reviewed head:** `f55bab53` (branch `feat-alluxio-scaledown-e2e`, merge-base `a6d461c7`)
- **Harness branch:** `verify/alluxio-scaledown-e2e` (production code untouched — additive tests + this dir only)
- **Layers:** unit + static (no `KUBECONFIG`). The harness is deliberately **gomonkey-free**;
  the package's existing tests use gomonkey, which crashes on darwin/arm64 (environmental — CI's
  Linux `unittest` job is green and authoritative).

## Context — why this PR exists

PR #5805 added this feature with unit tests only and was **reverted (#6059)** for exactly one
reason: no e2e validating the scale-down → decommission → **data-integrity** path. This PR
re-applies the feature and adds `test/gha-e2e/alluxio-scaledown/`. All CI passes, including the
5 `kind-e2e-test` matrix jobs.

## Findings

| ID | Finding | Severity | Polarity | Verdict |
|----|---------|----------|----------|---------|
| **F2** | `getWorkerWebPort()` returns 30000 for the **default** (host-network) runtime while the worker binds a **dynamically-allocated** port → decommission targets the wrong port → graceful drain silently no-ops → ungraceful scale-down after the 10m deadline | **Blocking** | contract | ✅ **Confirmed (reproduced)** |
| **F1** | The new e2e cannot detect data loss: both read jobs `cat` a fixture from an **S3/minio-backed** dataset, so `read_after` passes via UFS fallback even under total cache loss / ungraceful scale-down | High (the revert's whole point) | canary | ✅ **Confirmed (gap present)** |
| **F3** | Does `report capacity -live` exclude a decommissioned-but-still-migrating worker? If it drops immediately, `--wait 0s` + `activeCount<=desired` deletes the pod before blocks migrate | Question | — | ⏳ Open (Alluxio semantics; not code-verifiable) |
| **F4** | `parseActiveWorkerCount` counts a trailing non-indented footer line as a worker → drain could stall to the deadline if the real report has a footer | Minor/latent | canary | ✅ Confirmed latent |

## Observed vs expected

| Finding | Test(s) | Observed on PR head | Evidence |
|---------|---------|---------------------|----------|
| F2 | `TestVerifyF2WorkerWebPortMatchesAllocatedPort` (+ `...ContainerNetworkModeIsUnaffected`) | contract **FAILS**: worker binds web port `2048x` (from range 20000-21000), `getWorkerWebPort()` returns `30000`. Container-network companion PASSES. | `results/unit-alluxio.out` |
| F1 | `TestVerifyF1E2ECannotDetectDataLoss` | canary **PASSES**: dataset is `s3://`-backed, both read jobs are identical `cat` reads, and `test.sh` never disrupts the UFS between reads | `results/unit-alluxio.out` |
| F4 | `TestVerifyF4ParseActiveWorkerCountTrailingFooter` | canary **PASSES**: a trailing footer line inflates the live count 2→3 | `results/unit-operations.out` |

### Harness-bites check (Step 4)

F2 was proven to fail for the right reason: applying the candidate fix (persist the resolved
worker web port into `runtime.Spec.Worker.Ports["web"]` inside `allocatePorts`, so
`getWorkerWebPort` reflects the port the worker actually binds) turns the contract test **green**;
reverting turns it **red** again. Production diff is empty.

## The F2 bug in detail

- AlluxioRuntime's default network mode is **host network** (`IsHostNetwork("") == true`,
  `api/v1alpha1/container_network.go`).
- In host-network mode, `transform.go` runs `allocatePorts()` →
  `allocateSinglePort(..., runtime.Spec.Worker.Ports, "web")`. When the user hasn't pinned
  `spec.worker.ports["web"]`, the web port is assigned **dynamically from the operator port
  range** — not 30000. That value is written into the Helm values, **never back into
  `runtime.Spec.Worker.Ports`**.
- `getWorkerWebPort` (`pkg/ddc/alluxio/replicas.go`) only reads `runtime.Spec.Worker.Ports["web"]`
  or falls back to `defaultWorkerWebPort = 30000`.
- So `getDecommissionAddresses` builds `<hostIP>:30000`, but the worker's web server is on the
  allocated port. `alluxio fsadmin decommissionWorker` hits the wrong port, the drain never
  progresses, and after `defaultWorkerDecommissionDeadline` (10m) the engine forces an
  **ungraceful** scale-down — the precise data-loss scenario the feature was built to prevent.
- **Container-network** mode is unaffected: `generateStaticPorts` always assigns 30000, matching
  the default. The e2e itself runs in the default host-network mode, but passes anyway because
  the deadline fallback still converges the StatefulSet and (see F1) the read is UFS-backed.

**Suggested fix:** make `getWorkerWebPort` return the port the worker actually binds — e.g.
persist the resolved worker web port (into the runtime status, or read it back from the worker
StatefulSet/configmap), rather than assuming the spec value or 30000.

## Why F1 matters

The revert demanded an e2e proving data integrity. This e2e proves the scale-down **flow runs
and the StatefulSet converges**, and that data is **still readable** — but because the fixture
lives in the minio UFS, readability is guaranteed by UFS fallback regardless of whether any
cached block was preserved. An ungraceful scale-down (or F2 degrading to one) passes the exact
same test. To actually test the property, the e2e would need data that only lives on the
Alluxio workers (e.g. a `MUST_CACHE` write), or to make the UFS unavailable before the
after-read, or to assert on cache-hit metrics.

## How to run

```bash
go test ./pkg/ddc/alluxio/ ./pkg/ddc/alluxio/operations/ -run TestVerify -count=1 -v
```

Raw output: `results/`.

## Continuing after a fix (resume on any machine)

All durable state is on `verify/alluxio-scaledown-e2e`.

```bash
git fetch origin && git checkout verify/alluxio-scaledown-e2e
bash docs/verification/alluxio-scaledown-e2e/scripts/re-verify.sh   # no sha; resolves PR head from manifest.pr
```

Polarity when reading results:
- **F2 (contract):** red now. Goes **green** when `getWorkerWebPort` reflects the real worker
  web port — that's the fix landing.
- **F1 (canary):** green now (gap present). When it **flips to red**, the e2e was strengthened
  to actually test data integrity — invert/retire the canary then.
- **F4 (canary):** green now (parser miscounts a footer). Flips if the parser is hardened.

After reviewing a new delta, advance the marker:

```bash
echo <new-head-sha> > docs/verification/alluxio-scaledown-e2e/.last-reviewed
git commit -am "review: advance last-reviewed"
```

### Copy-paste kickoff for a fresh agent

> Resume the review pipeline for https://github.com/fluid-cloudnative/fluid/pull/6061.
> Branch `verify/alluxio-scaledown-e2e` on remote `origin` holds the harness, manifest, and
> `.last-reviewed`. Check it out, run `scripts/re-verify.sh` (no sha — reads `manifest.pr`),
> report Fixed/Still-broken per finding honoring polarity (F2 contract = fixed when green;
> F1/F4 canaries = fixed only when they flip). Then review the `last-reviewed..head` delta and
> advance the marker. Note: package tests use gomonkey and crash on darwin — the verify tests
> are gomonkey-free by design.
