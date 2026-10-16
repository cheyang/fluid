# security-model-docs — verification of PR #6211 (Fluid security model docs)

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6211.

Layers run against the **code under review** (`818e8000`, PR head):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (script) | every code-path/symbol/constant claim in the two new docs; CRD inventory; EN/ZH YAML + heading identity; YAML parse | `python3 -I docs/verification/security-model-docs-claude/scripts/check_doc_claims.py` |
| 2. Integration (helm render) | the doc's control-plane RBAC claims against the *rendered* chart under both `helmDriver` settings: no wildcards, Secret grants only under `secret`, both webhooks `failurePolicy: Fail` | `bash docs/verification/security-model-docs-claude/scripts/render_chart_checks.sh` (needs `helm`; no cluster) |
| 3. Live | not needed — the PR is documentation-only and changes no runtime behavior | — |

> Polarity: every check is a **contract** test — it asserts the documented claim is
> true at the checked-out ref. A FAIL means the doc is wrong (or the code drifted
> past the doc, which the doc itself calls a documentation bug). No canaries.

## Problem premise (P0)

Docs-only PR — premise validation skipped per the review-pipeline carve-out.
Noted for the record: the gap the PR fills is real. Before this PR, `security/`
contained only `self-assessment.md` (CNCF TAG-Security self-assessment, no
resource tiers, no RBAC delegation guidance, no deployment scenarios) and
images. No security model existed, and the PR touches exactly the right
component (`security/`).

## Summary of results

Observed vs expected, per claim group. All checks: **59/59 L1 PASS,
5/5 L2 PASS** at `818e8000`.

| ID | Claim (from the doc) | Layer | Verdict | Evidence |
|----|-----------------------|-------|---------|----------|
| DOC-4 | 35 file-path references resolve; ~38 symbol references exist (incl. the doc faithfully reproducing the `transfromSecretsForPersistentVolumeClaimMounts` typo) | 1 | Confirmed | results/claim-checks.txt (D2, D3) |
| DOC-4 | "Fluid ships 14 user-facing `data.fluid.io` CRDs and one internal `workload.fluid.io` CRD" | 1 | Confirmed | D4: charts/fluid/fluid/crds = 14 + advancedstatefulsets |
| DOC-4 | Tier 0 is cluster-scoped: CacheRuntimeClass + ThinRuntimeProfile CRD `scope: Cluster` | 1 | Confirmed | manual check + D9 |
| DOC-4 | FUSE workloads privileged + hostPath + Bidirectional mount propagation (all runtimes incl. JuiceFS via Go-transform default) | 1 | Confirmed | charts/<runtime>/templates/fuse/daemonset.yaml; pkg/ddc/juicefs/transform.go:62 |
| DOC-4 | `spec.fuse.networkMode` defaults to HostNetwork | 1 | Confirmed | api/v1alpha1/container_network.go IsHostNetwork |
| DOC-4 | JuiceFS DataLoad joins options into `--key=value` string | 1 | Confirmed | pkg/ddc/juicefs/data_load.go:203-211 |
| DOC-4 | Alluxio backupPath → writable hostPath on master node; runAs override | 1 | Confirmed | backup_data.go:136,155-158; charts/fluid-databackup/alluxio/templates/databackup.yaml:114-116 |
| DOC-4 | DataLoad/DataProcess reject cross-namespace target Dataset | 1 | Confirmed | dataload/implement.go:94-100; dataprocess/implement.go:98-105 |
| DOC-4 | DataProcess podSpec is full PodSpec + serviceAccountName; DataMigrate inlines VersionSpec | 1 | Confirmed | dataprocess_types.go:42,59; datamigrate_types.go:31 |
| DOC-4 | DataLoad has no caller image/podSpec/volumes | 1 | Confirmed | dataload_types.go (grep zero hits) |
| DOC-4 | CacheRuntimeClass topology templates are deep-copied verbatim into workloads (privileged/hostPID/hostNetwork/SA flow through) | 1 | Confirmed | cacheruntimeclass_types.go:50; cache/engine/transform_common.go:41-42 |
| DOC-4 | Reference Dataset: default ReadOnlyMany, physical PV access modes constrain; subpath cannot escape; cannot chain references | 1 | Confirmed | referencedataset/volume.go:137-155; engine.go:233-262; base/dataset.go:54-81 |
| DOC-4 | CSI resolveMountSource: O_NOFOLLOW walk, symlink rejection, pinned inode; default bindMount; non-default symlink path lacks pinning | 1 | Confirmed | subpath_linux.go:34-109; nodeserver.go:186-193; values.yaml nodePublishMethod: bindMount |
| DOC-4 | Unprivileged sidecar mutator: privileged=false, SYS_ADMIN trimmed from Add, only known host volumes removed, no SYS_ADMIN in Drop; PVC→hostPath still happens | 1 | Confirmed | mutator_unprivileged.go:79-96; mutator_default.go:150-180 |
| DOC-4 | fuse-balloon nodeSelector added to copied FUSE DS; privilege retained; ConfigMap copy is unconditional (even for CSI consumers) | 1 | Confirmed | referencedataset/cm.go:62-65,35-73,75-145 |
| DOC-4 | Deprecated fuse.sidecar.fluid.io/inject still recognized by handler but matches neither default webhook selector | 1 | Confirmed | annotations.go:132; webhookconfiguration.yaml:26-27,47-48 |
| DOC-4 | Webhook: no validating webhook shipped for fuse fields | 1 | Confirmed | pkg/webhook/handler has mutating only; chart has no ValidatingWebhookConfiguration |
| DOC-4 | config/rbac editor/viewer roles not referenced by kustomization or chart | 1 | Confirmed | kustomization.yaml; no refs in charts/ |
| DOC-4 | Chart RBAC: no wildcards; helmDriver default configmap; cluster-wide Secret grants only under secret driver; thinruntime-controller has no Secret perms by default | 1+2 | Confirmed | L2 render: configmap=2 namespaced webhook rules, secret=+7 controller ClusterRoles |
| DOC-4 | Both injection webhooks failurePolicy: Fail with limited selectors | 1+2 | Confirmed | rendered output: 2× Fail |
| DOC-4 | csi.useNodeAuthorization default true; kubelet creds for Node reads/patches | 1 | Confirmed | values.yaml:72; nodeserver.go:453-523 |
| DOC-4 | EN/ZH: identical YAML blocks (byte-identical), 32=32 headings, all YAML parses, all relative links resolve | 1 | Confirmed | D6, D7, D8 |
| DOC-1 | "a `local://` mountPoint becomes a `hostPath` on Jindo" | 1 | **Partial** | jindofsx/transform.go:302-310 and jindocache/transform.go:402-410 implement it (jindocache is the default engine for JindoRuntime, pkg/utils/jindo/jindo.go:36-37); the legacy pkg/ddc/jindo engine ignores `local://` (transform.go:226-245 handles only hdfs/s3/oss) |
| DOC-2 | security model docs reachable from existing entry points | 1 | **Refuted (gap)** | nothing in README.md / SECURITY.md / self-assessment.md links to security-model.md or security-model_zh.md (repo-wide grep) |
| DOC-3 | doc mentions kubelet-config fallback for CSI node authorization | 1 | **Refuted (gap)** | pkg/csi/plugins/register.go:37 silently continues without node authorization when the kubelet config file is missing; the doc's §5 statement is accurate as written but incomplete |

## Harness-bites check

The harness was proven to actually bite in both directions (production code
restored after each bite; final `git status` clean apart from the harness):

1. **Doc-side path** — insert `pkg/nonexistent/bogus_ref.go` into the doc → `D2 FAIL` (missing path detected).
2. **Doc-side YAML** — mutate one ZH YAML block → `D6 FAIL` (EN/ZH identity check detects).
3. **Code-side default** — flip `values.yaml` `useNodeAuthorization: true` → `false` → `D9 FAIL`.
4. **L2 render** — flip a webhook `failurePolicy: Fail` → `Ignore` in the chart → render check FAILs.

## Per-finding detail

### DOC-1 [minor] — `local://` → hostPath claim is engine-specific
`security/security-model.md` Tier 1, Dataset row: "a `local://` mountPoint
becomes a `hostPath` on Jindo". The mechanism exists in `pkg/ddc/jindofsx/transform.go:302-310`
and `pkg/ddc/jindocache/transform.go:402-410` (rendered as hostPath volumes in
`charts/jindofsx/templates/master/statefulset.yaml:226-231`), and `jindocache`
is the **default** engine impl for `JindoRuntime`
(`pkg/utils/jindo/jindo.go:36-37`, env `JINDO_ENGINE_TYPE` selects jindofs/jindofsx).
But the legacy `pkg/ddc/jindo` engine's mount loop (`transform.go:226-245`)
handles only `hdfs://`, `s3://`, `oss://` and silently `continue`s past a
`local://` mountPoint. The claim is true for every Jindo engine a modern
deployment uses by default; it is imprecise for the legacy engine, and unlike
most claims in the doc it carries no code citation. Suggest wording it as
"on JindoFSx/JindoCache" or citing the two transform paths.

### DOC-2 [minor] — no inbound links to the security model
The two new docs link out to `self-assessment.md`, `../SECURITY.md`, and each
other, but nothing in the repo links **in**: `README.md`, `SECURITY.md`, and the
self-assessment's "Security links" table (the natural home — it currently lists
only SECURITY.md) all lack a reference. The doc's §3 tells operators they "must
know which Fluid resources expose that permission before delegating access";
that audience enters through SECURITY.md or the README. Suggest adding one link
in SECURITY.md and one row in the self-assessment security-links table.

### DOC-3 [nit] — CSI node-authorization fallback not mentioned
`pkg/csi/plugins/register.go:37` — if the kubelet config file does not exist,
the CSI driver logs a warning and continues **without** a node-authorized
client (nil client), while the chart (correctly, per the doc) omits the nodes
rule from the ClusterRole when `useNodeAuthorization: true`. In that state Node
reads/patches fall back to the ServiceAccount and fail on RBAC. The doc's §5
statement is accurate; one more sentence on the failure mode would make the
operational guidance complete.

### DOC-4 [nit] — §9 "some tests already exist" is 2-of-3
Subpath escape is tested (`pkg/ddc/base/dataset_test.go:97` —
`GetPhysicalDatasetSubPath`), CSI symlink handling is tested
(`pkg/csi/plugins/subpath_test.go:27-73` — includes a symlink-rejection case),
but cross-namespace target rejection in DataLoad/DataProcess has no direct unit
test (`dataload/implement_test.go` only covers `IsTargetPathUnderFluidNativeMounts`).
The doc's own §9 already calls for those tests, so this is consistent — just
note the cross-namespace rejection is the first coverage gap to close.

## Proposed fixes (NOT applied to production here)

- **DOC-1**: reword the Dataset row's `local://` clause to name JindoFSx/JindoCache (or add the two transform paths as citations).
- **DOC-2**: add links to `security/security-model.md` from `SECURITY.md` and the self-assessment's Security links table.
- **DOC-3**: one sentence in §5 about the kubelet-config-missing fallback.
- **DOC-4**: no change needed; §9's coverage ask already covers the gap.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/security-model-docs-claude` (production
code untouched), so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/security-model-docs-claude
   git checkout <fixed-ref>
   git checkout FETCH_HEAD -- docs/verification/security-model-docs-claude
   ```
2. Re-run both layers:
   ```bash
   python3 -I docs/verification/security-model-docs-claude/scripts/check_doc_claims.py
   bash   docs/verification/security-model-docs-claude/scripts/render_chart_checks.sh
   ```
   Exit 0 = all doc claims hold at that ref.
3. Or just: `bash docs/verification/security-model-docs-claude/scripts/re-verify.sh`
   (fetches the current PR head itself and reports per-finding Fixed / Still-broken / Partial).

Polarity: all checks are contract tests. If the author fixes DOC-1/DOC-2/DOC-3
in the docs, the L1 checks do not automatically flip (they assert claims
against code, not prose wording) — re-review the prose delta by reading
`last-reviewed..head`; the checks re-assert every *code* claim the prose makes.

Kickoff prompt for a fresh agent:

> You are re-verifying PR fluid-cloudnative/fluid#6211 (security model docs).
> Check out the harness branch verify/security-model-docs-claude from the
> reviewer fork https://github.com/cheyang/fluid.git, run
> docs/verification/security-model-docs-claude/scripts/re-verify.sh, then read
> the last-reviewed..head delta of security/security-model*.md and check any
> new/changed claim against the code the way README.md's table describes.
