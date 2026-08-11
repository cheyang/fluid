# pr6148-juicefs-edition-shared-encrypt — bug verification

Reproducible evidence for the findings raised while reviewing
[fluid-cloudnative/fluid#6148](https://github.com/fluid-cloudnative/fluid/pull/6148)
("fix(juicefs): read metaurl from SharedEncryptOptions when deciding the edition").

Layers run against the **code under review** (PR head `c7637341`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `genEdition` call site in `transform()` (PR's own table) | `go test ./pkg/ddc/juicefs/ --ginkgo.focus='edition derived by transform'` |
| 2. Integration | `transform()` → `genEdition` → `genFormatCmd`: edition + FormatCmd downstream | `go test ./pkg/ddc/juicefs/ --ginkgo.focus='transform format-cmd'` |
| 3. Live | real controller binary vs real cluster; values ConfigMap `edition`+`formatCmd` | `scripts/00-setup.sh` → `scripts/05-run-pr-controller.sh` → `scripts/10-community.sh` → `scripts/20-enterprise.sh` → `scripts/99-teardown.sh` |

> Polarity: all findings are **contract** tests (assert intended behavior; FAIL on buggy code, PASS when fixed). No bug-canaries.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| B1 | metaurl in `SharedEncryptOptions` → edition `community` (call site in `transform`) | 1 (unit) | **Confirmed** | revert the one-line fix → only the "metaurl in SharedEncryptOptions" entry fails (17 pass / 1 fail); with fix 18/18. `results/unit-without-fix.txt` |
| B2 | downstream: edition `community` **AND** `genFormatCmd` emits a non-empty community `juicefs format … ${METAURL}` (resolves "fs never formatted") | 2 (integration) | **Confirmed** | `transform_format_verify_test.go` PASS with fix; revert → FAIL (`edition must be community`, got `enterprise`). `results/integration-with-fix.txt`, `results/integration-without-fix.txt` |
| B3 | on a real cluster the PR controller writes `edition=community` + non-empty ce `formatCmd` + the CE image; `juicefs format` succeeds against a real OSS bucket and shutdown cleans cache via the community `getUUID` branch (no `${METAURL}` retry); the installed buggy `v1.1.0-36f0467` controller writes `edition=enterprise` + empty `formatCmd` + the EE image for the same Dataset | 3 (live) | **Confirmed (end-to-end)** | `results/LIVE-EVIDENCE.txt`, `results/live-community-realbucket-configmap.txt`, `results/live-community-worker-realbucket.log`, `results/live-community-teardown.log` — buggy: `edition=enterprise, formatCmd=None`; PR: `edition=community`, worker `juicedata/mount:ce-v1.3.0` Running, `juicefs format` → `Volume is formatted as {Storage:oss, Bucket:https://my-jfs.oss-cn-hongkong.aliyuncs.com}`, `juicefs mount` → `OK, jfscomm is ready`; teardown ran `juicefs status` (community getUUID) → real uuid → `Remove cache in worker pod` → clean exit, no `retryShutdown` |
| B4 | regression: enterprise (token in `SharedEncryptOptions`) stays `enterprise` with the ee `juicefs auth --token=${TOKEN}` cmd; PR does not touch enterprise classification | 2 (integration) + 3 (live) | **Confirmed (no regression)** | integration test PASS with and without fix; live enterprise configmap `edition=enterprise, formatCmd=/usr/bin/juicefs auth --token=${TOKEN} … jfsee`. `results/live-enterprise-configmap.txt` |

## Per-finding detail

### B1 — edition classification call site
- **Mechanism:** `transform()` (pkg/ddc/juicefs/transform.go:69) calls
  `j.genEdition(dataset.Spec.Mounts[0], value, dataset.Spec.SharedEncryptOptions)`
  (PR fix). Before the fix it passed `dataset.Spec.Mounts[0].EncryptOptions`, so
  `genEdition` scanned `Mounts[0].EncryptOptions` twice and never scanned
  `SharedEncryptOptions`; a Dataset with `metaurl` only in `SharedEncryptOptions`
  was left at the `EnterpriseEdition` default.
- **Command:** `go test ./pkg/ddc/juicefs/ --ginkgo.focus='edition derived by transform'`
- **Observed vs expected:** with fix → 18/18 pass; revert fix → exactly the
  "metaurl in SharedEncryptOptions" entry fails (`<enterprise> to equal <community>`), 17 pass / 1 fail. Matches the PR author's reported 173 pass / 1 fail on their tree.

### B2 — downstream FormatCmd (the "filesystem never formatted" half)
- **Mechanism:** `transform()` → `transformFuse()` → `genFormatCmd(value.Edition, …)`.
  On the buggy path `Edition==enterprise` and `Configs.TokenSecret==""`, so `genFormatCmd`
  hits the enterprise early-return (`// skip juicefs auth` → `return`) and leaves
  `FormatCmd` empty. The fix makes `Edition==community`, so the community branch emits
  `/usr/local/bin/juicefs format … ${METAURL} <name>`.
- **Command:** `go test ./pkg/ddc/juicefs/ --ginkgo.focus='transform format-cmd'` (file `transform_format_verify_test.go`).
- **Observed:** with fix → both specs PASS (community gets non-empty ce `format`; enterprise gets `auth --token=${TOKEN}`). Revert fix → community spec FAILS on `Edition` (and would on `FormatCmd`); enterprise spec still PASS (unaffected).

### B3 — live bug-repro vs fix (both PR claims, end-to-end)
- **Mechanism (claim #2 of the PR):** buggy `Edition==enterprise` + `genValue` reading
  `SharedEncryptOptions` → `Source="${METAURL}"` → `getUUID` enterprise branch returns
  `uuid=Source="${METAURL}"` → `cleanupCache` deletes `<cacheDir>/${METAURL}/raw/chunks`
  → `cmdguard` rejects the `$` → `Shutdown` errors → `retryShutdown++`. With the fix,
  `Edition==community` so `getUUID` takes the community branch (`juicefs status`),
  resolves a real uuid, and `cleanupCache` deletes `<cacheDir>/<real-uuid>/raw/chunks`.
- **Live contrast (same Dataset spec, real OSS bucket `my-jfs`):**
  - buggy installed controller `v1.1.0-36f0467` (built from a `master` commit that
    predates this PR — `master` is still buggy, the PR is open): `edition=enterprise`,
    `formatCmd=None`, `image` enterprise.
  - PR controller (built from this branch, sole reconciler): `edition=community`,
    `image=juicedata/mount:ce-v1.3.0`, `formatCmd=/usr/local/bin/juicefs format … ${METAURL} jfscomm`.
- **Claim #1 (fs never formatted) — RESOLVED, end-to-end:** the CE worker pod
  (`juicedata/mount:ce-v1.3.0`, Running 1/1) ran `juicefs format` against the real OSS
  bucket: `Data use oss://my-jfs/jfscomm/` → `Volume is formatted as {Storage: oss,
  Bucket: https://my-jfs.oss-cn-hongkong.aliyuncs.com, AccessKey: <REDACTED>}`, then
  `juicefs mount` → `OK, jfscomm is ready at …/juicefs-fuse`. Pre-teardown status:
  `workerPhase=Ready, fusePhase=Ready`. (`results/live-community-worker-realbucket.log`)
- **Claim #2 (teardown fails & retries) — RESOLVED, observed:** on delete the PR
  controller shutdown path ran the community `getUUID` branch — `juicefs status ${METAURL}`
  succeeded with a real status JSON → `Remove cache in worker pod` (real uuid, no literal
  `${METAURL}` in the path) → `clean up fuse count n=0` → clean exit, finalizer cleared.
  No `cmdguard` `$`-rejection, no `retryShutdown` increment. (`results/live-community-teardown.log`)
- **Setup note (why the worker needs the CE image):** an earlier run with a placeholder
  bucket saw the worker pod use `juicedata/mount:ee-5.2.10-eb0a8b3` and fail with
  `unsupported scheme redis`. That was the *buggy* configmap (`edition=enterprise` → EE
  image) being picked up before the PR controller became the sole reconciler. Once the
  PR controller was sole and the Dataset was recreated fresh, the configmap carried
  `image=ce-v1.3.0`, the worker used the CE binary (which supports `redis://`), and
  format + mount succeeded.

### B4 — enterprise regression
- **Mechanism:** the PR changes only the third argument to `genEdition`; enterprise
  classification (no `metaurl` present) is reached identically before and after.
- **Observed (live + integration):** enterprise Dataset (token in
  `SharedEncryptOptions`) → `edition=enterprise`, `formatCmd=/usr/bin/juicefs auth
  --token=${TOKEN} --accesskey=${ACCESS_KEY} --secretkey=${SECRET_KEY} --bucket=… jfsee`.
  Integration spec PASS with and without the fix.

## Live run notes
- Cluster: ACK, 3 nodes (`cn-hongkong.*`), Fluid helm `fluid-1.1.0` (app `1.1.0-676f47a`),
  installed juicefsruntime-controller image `fluidcloudnative/juicefsruntime-controller:v1.1.0-36f0467`
  (normally scaled to 0). Restored to 0 + original image after the run.
- PR controller run out-of-cluster (`cmd/juicefs` binary built from the PR head) with
  `KUBECONFIG` pointing at the cluster; same env (`JUICEFS_CE_IMAGE_ENV`,
  `JUICEFS_EE_IMAGE_ENV`, `MOUNT_ROOT`) as the in-cluster deploy.
- **Making the PR controller the sole reconciler:** the `dataset-controller` scales the
  in-cluster juicefsruntime-controller to 1 "on demand" whenever a JuiceFSRuntime exists
  (Fluid's lazy-controller), and K8s keeps the old (working, buggy) ReplicaSet alive
  during a failed rollout. So `--replicas=0` alone is reverted. To make the PR
  controller authoritative: patch the in-cluster deploy image to a non-pullable tag
  AND delete the old (working-image) ReplicaSet, so only ErrImagePull pods can be
  created and the buggy controller cannot reconcile. Restored after.
- **Out-of-cluster controller prerequisites:** `ddc-helm` and the `/charts/juicefs`
  helm chart (with sibling `/charts/library`) are bundled inside the controller image
  but absent on the host. Extract both from the image (`docker cp`) onto the host PATH /
  `/charts` so `SetupMaster` can `helm install` the worker StatefulSet. `05-run-pr-controller.sh`
  assumes the host is set up this way for a full live run.
- metaurl backend: an in-cluster `redis:7-alpine` (`redis://<svc-clusterIP>:6379/N`).
- OSS backend: real bucket `my-jfs` (oss-cn-hongkong, IA). `juicefs format` succeeded and
  `juicefs mount` reported `jfscomm is ready`; teardown ran the community `getUUID` branch
  and cleaned cache without the `${METAURL}` retry.
- Credentials (JuiceFS enterprise token, OSS access-key/secret-key) were supplied only
  via environment variables to the scenario scripts; **no secret material is captured in
  this branch** — configmap `formatCmd` fields use `${TOKEN}`/`${ACCESS_KEY}` placeholders,
  and worker/teardown logs are redacted (`<REDACTED-AK/SK/TOKEN>`).

## Proposed fixes (NOT applied to production here)
- **B1/B2/B3:** the PR's one-line fix is the correct, minimal fix and is confirmed by
  all three layers. No additional production change is needed. (Optional test-strength
  follow-up: the PR's own table asserts `Edition` only; `transform_format_verify_test.go`
  in this harness additionally pins `FormatCmd` so the downstream "format never runs"
  half is regression-guarded going forward.)

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/pr6148-juicefs-edition-shared-encrypt` (production
code untouched — diff is only `docs/verification/…` + the additive test file), so it
grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/pr6148-juicefs-edition-shared-encrypt
   git checkout verify/pr6148-juicefs-edition-shared-encrypt -- docs/verification/pr6148-juicefs-edition-shared-encrypt pkg/ddc/juicefs/transform_format_verify_test.go
   ```
2. Re-run unit + integration (deterministic, no cluster needed):
   ```bash
   go test ./pkg/ddc/juicefs/ --ginkgo.focus='edition derived by transform'
   go test ./pkg/ddc/juicefs/ --ginkgo.focus='transform format-cmd'
   ```
   All contract specs must PASS on fixed code.
3. Re-run live (needs a cluster + kubeconfig + creds in env):
   ```bash
   export KUBECONFIG=… REPO=$(pwd)
   bash docs/verification/pr6148-juicefs-edition-shared-encrypt/scripts/00-setup.sh
   bash docs/verification/pr6148-juicefs-edition-shared-encrypt/scripts/05-run-pr-controller.sh
   METAURL=redis://$(kubectl -n jfs-verify get svc redis -o jsonpath='{.spec.clusterIP}'):6379/1 \
     ACCESS_KEY=… SECRET_KEY=… \
     bash docs/verification/pr6148-juicefs-edition-shared-encrypt/scripts/10-community.sh
   JFS_TOKEN=… ACCESS_KEY=… SECRET_KEY=… \
     bash docs/verification/pr6148-juicefs-edition-shared-encrypt/scripts/20-enterprise.sh
   bash docs/verification/pr6148-juicefs-edition-shared-encrypt/scripts/99-teardown.sh
   ```
4. Automated re-verify (unit+integration, grafts onto current PR head):
   ```bash
   bash docs/verification/pr6148-juicefs-edition-shared-encrypt/scripts/re-verify.sh
   ```

### Copy-paste kickoff prompt (for a fresh agent elsewhere)
> You are resuming the review-pipeline verification for
> https://github.com/fluid-cloudnative/fluid/pull/6148. The harness is on branch
> `verify/pr6148-juicefs-edition-shared-encrypt` in cheyang/fluid. Graft
> `docs/verification/pr6148-juicefs-edition-shared-encrypt/` and
> `pkg/ddc/juicefs/transform_format_verify_test.go` onto the current PR head, run
> the unit + integration ginkgo focuses listed in the README, then (if a cluster
> + kubeconfig + creds are available) run the scripts under `…/scripts/` in order.
> Report observed-vs-expected per finding (B1–B4). All findings are contract
> polarity; on fixed code every contract spec must PASS. Push the advanced
> `.last-reviewed` after the round.

## Polarity table (for the next loop)

| ID | Layer | Polarity | On buggy code | On fixed code |
|----|-------|----------|---------------|---------------|
| B1 | unit | contract | FAIL | PASS |
| B2 | integration | contract | FAIL (community spec) | PASS |
| B4 | integration | contract | PASS (enterprise unaffected) | PASS |
| B3 | live | contract (observed) | `edition=enterprise, formatCmd=None` | `edition=community, formatCmd=<ce format>` |
