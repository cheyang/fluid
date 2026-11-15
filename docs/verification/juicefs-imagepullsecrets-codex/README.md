# Verification — PR #6245: hierarchical imagePullSecrets for JuiceFSRuntime

Reviewer: Codex (Reviewer B). Round: incremental (round 2).
Head under review: `3271f28d7c3bc126d221422abdb9e6e0048eb9c9` (base `ff99142f9ec5c32a0c9166de023cb8936f0e5ae5`, previous head `6711374b5ccbdf0371069b4cc9c0ca9593891528`).

## Premise (P0) — CONFIRMED

Issue #4222 (OPEN) asks for hierarchical `imagePullSecrets` on JuiceFSRuntime:
spec-level plus `spec.worker` / `spec.fuse` overrides. On the merge-base the
feature does not exist: zero `imagePullSecrets` fields in
`juicefsruntime_types.go`, and the juicefs chart consumes only the top-level
`.Values.imagePullSecrets` (fed from the controller's `IMAGE_PULL_SECRETS`
env). Component match: the issue names JuiceFSRuntime spec/worker/fuse; the PR
touches exactly the juicefs API, engine transform and chart. Evidence:
`results/premise-base.log`.

## Claims and layers

| ID | claim | layer | polarity | test / script |
|----|-------|-------|----------|----------------|
| A1 | env default still applies when runtime sets nothing | unit | contract | TestVerifyPr6245EnvDefaultStillApplies |
| A2 | spec.imagePullSecrets replaces env default at top level | unit | contract | TestVerifyPr6245RuntimeOverridesEnv |
| A3 | worker/fuse overrides land in component values | unit | contract | TestVerifyPr6245WorkerAndFuseOverrides |
| A4 | serialized values expose imagePullSecrets / worker.imagePullSecrets / fuse.imagePullSecrets (chart contract) | unit | contract | TestVerifyPr6245HelmValuesShape |
| A5 | regenerated deepcopy isolates the new slices | unit | contract | TestVerifyPr6245DeepCopyIsolated |
| A6 | rendered worker/fuse pods get the right secrets in 7 scenarios incl. byte-identical base-vs-head render for legacy values | integration (helm template via TestVerifyPr6245HelmRenderScenarios) | contract | scripts/helm_render_check.sh |
| A7 | regenerated CRD exposes the fields and validates old+new CRs | integration (jsonschema via TestVerifyPr6245CRDSchemaCheck) | contract | scripts/crd_schema_check.py |
| F1 | spec.master / spec.jobWorker imagePullSecrets are accepted by the API but inert (no consumer) | unit | **canary** | TestVerifyPr6245MasterAndJobWorkerSecretsInert |
| F-apidoc-rows-misplaced | the three new imagePullSecrets rows in docs/{en,zh}/dev/api_doc.md sit inside their own type sections' tables | integration | contract | TestVerifyPr6245APIDocPlacement / scripts/apidoc_placement_check.py |

## How to run

```bash
go test ./pkg/ddc/juicefs/ -run 'TestVerifyPr6245' -count=1 -v   # unit + integration wrappers
# or directly:
bash docs/verification/juicefs-imagepullsecrets-codex/scripts/helm_render_check.sh   # needs helm
python3 docs/verification/juicefs-imagepullsecrets-codex/scripts/crd_schema_check.py "$(git rev-parse --show-toplevel)"
```

## Observed vs expected (round 1, head 6711374b)

| ID | expected | observed | verdict |
|----|----------|----------|---------|
| P0 | feature absent on base | absent (compile break against base API) | confirmed |
| A1-A5 | all pass on head | all pass | confirmed |
| A6 | 14 scenario assertions pass, base/head renders identical modulo version label | pass | confirmed |
| A7 | schema exposes 5 paths, new+legacy CRs validate | pass | confirmed |
| F1 | master/jobWorker secrets reach no chart value | confirmed inert | open (minor) — see findings |

## Continuing after a fix

`bash docs/verification/juicefs-imagepullsecrets-codex/scripts/re-verify.sh` from a
checkout of this branch re-runs unit+integration layers against the current PR
head (resolved from `pr` in verify-manifest.json). Polarity: A* are contract
tests (pass = good); F1 is a canary — if master/jobWorker secrets ever get
wired to a consumer, it flips red and should be inverted into a contract test.

Production code is untouched by this harness (additive test + docs only).

## Round 2 (incremental, head 3271f28d) — observed vs expected

Delta `6711374b..3271f28d`: doc-comment caveat for master/jobWorker, values.yaml
component keys, regenerated api_doc.md (en+zh), new sample section. Re-verify:

| ID | expected | observed | verdict |
|----|----------|----------|---------|
| A1-A7 | still pass on new head | all pass (results/reverify/) | contracts hold |
| F1 (canary) | flips only if master/jobWorker get wired | still passing = still inert | STILL-PRESENT (documented now, ask resolved) |
| F-apidoc-rows-misplaced | rows inside own type tables | FAIL: CompTemplateSpec row orphaned under `#### JuiceFSRuntime` (line 1323); FuseSpec row tops the JuiceFSRuntimeSpec table (1352), mislabeling spec.imagePullSecrets as fuse-specific; RuntimeSpec row orphaned under `#### MasterSpec` (Vineyard, line 1396); en+zh identical | STILL-BROKEN, confirmed (results/apidoc-placement-head.log) |

Harness-bites: the same check passes on the output of the repo-pinned
crd-ref-docs v0.3.0 run at head (`make update-api-doc` equivalent), where the
rows land in JuiceFSCompTemplateSpec:1323 / JuiceFSFuseSpec:1352 /
JuiceFSRuntimeSpec:1396 (results/apidoc-placement-regen-fixed.log). The
committed file was not produced by the pinned tool: a genuine regen also adds
missing pre-existing sections (EmptyDirMediumSource, HostPathMediumSource,
MediumSource, DataLoad policy/schedule rows) that the committed file lacks
(results/apidoc-regen-vs-committed.diff); the author spliced only the three
JuiceFS rows into the stale committed file, one section off each time.

Full suite on head: `go build ./...` clean, `go test ./pkg/ddc/juicefs/...` ok
(round-2 run in a detached head worktree).
