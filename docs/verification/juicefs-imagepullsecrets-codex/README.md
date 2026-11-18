# Verification — PR #6245: hierarchical imagePullSecrets for JuiceFSRuntime

Reviewer: Codex (Reviewer B). Round: incremental (round 4).
Head under review: `be3337dd00b5356cae2571d540f3b27d2c9e9ade` (base `ff99142f9ec5c32a0c9166de023cb8936f0e5ae5`, previous head `d7e316803a0e4b667ad4f9a19d10c605579e1ad1`).

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

## Round 3 (incremental, head d7e316803) — observed vs expected

Delta `3271f28d..d7e316803`: (1) api_doc.md regenerated with the pinned tool
(fixes F-apidoc-rows-misplaced); (2) **new behavior** — imagePullSecrets wired
into `SyncRuntime` (worker + fuse leaf diffs, fuse generation bump, caller
records the runtime-wide value once both are synced) plus the PR's own
`TestSyncImagePullSecrets`. This closes the round-1 sync-path gap in-PR.

| ID | expected | observed | verdict |
|----|----------|----------|---------|
| A1-A7 | still pass | all pass (results/reverify/unit-round3.parsed) | contracts hold |
| F-apidoc-rows-misplaced | rows in own type tables | FIXED - committed api_doc.md byte-identical to crd-ref-docs v0.3.0 regen (results/apidoc-regen-vs-committed-round3.diff empty) | fixed |
| F1 (canary) | flips only if master/jobWorker wired | still passing = still inert | STILL-PRESENT (documented, by design) |
| AC9-sync-propagation (new) | add/override-shield/remove converge pods + configmap, idempotent, fuse generation bumped once | all 3 pass; bites-verified (results/bites-round3.txt sabotage A/B) | confirmed |
| F-worker-lost-update-missing-scriptcm (new) | worker sts updated or sync retried | RED: with the worker-script configmap missing, syncWorkerSpec returns changed=true before Client.Update; SyncRuntime persists the mutated value; sts stays stale and the next reconcile sees no diff (results/reverify/unit-round3.parsed) | STILL-BROKEN (minor; pre-existing early-return structure, new field inherits it) |
| F-docs-recreate-note-stale (new) | samples must not claim recreate-to-change | RED: docs/{en,zh}/samples/image_pull_secrets.md still say changing secrets needs recreating the runtime (scripts/docs_staleness_check.sh exit 1; bites-verified) | STILL-BROKEN (minor, docs-only) |

Fix-direction proof for the lost-update: making syncWorkerSpec return an error
when the worker command is unavailable flips the repro green and keeps every
other harness test green (results/bites-round3.txt).

New tests: `pkg/ddc/juicefs/sync_image_pull_secrets_verify_test.go`
(S1/S2/S3 contracts + lost-update repro + docs-freshness wrapper). The branch
is rebased onto d7e316803, so `go test ./pkg/ddc/juicefs -run TestVerifyPr6245`
runs everything directly; note the full package run on this branch FAILS only
on the two intentional repro tests - the clean-head suite is green
(results/reverify/pkg-suite-round3-clean-head.txt).

## Round 4 (incremental, head be3337dd00) — observed vs expected

Delta `d7e316803..be3337dd00`: single commit `be3337dd0` "fix(juicefs):
compare image pull secrets against the live templates" - exactly the fix
direction round 3 pre-accepted for the recorded-state-vs-cluster desync class.
`checkAndSetWorkerChanges` / `checkAndSetFuseChanges` now diff the desired
effective secrets against the LIVE pod template (not the recorded value) and
record the raw per-component override unconditionally; the caller persists the
configmap when `changed || overridesChanged`. The PR's own test gains the two
missing scenarios (skipped-sync catch-up, rotation+override) plus
`TestSyncImagePullSecretsRotationThenOverrideRemoval`.

| ID | expected | observed | verdict |
|----|----------|----------|---------|
| A1-A7 | still pass | all pass (results/reverify-round4-head.json) | contracts hold |
| AC9-sync-propagation | add/override-shield/remove still converge, idempotent | 3/3 pass on head | contracts hold |
| F1 (canary) | flips only if master/jobWorker wired | still passing = still inert | STILL-PRESENT (documented, by design) |
| F-apidoc-rows-misplaced | rows in own type tables | pass | still fixed |
| F-worker-lost-update-missing-scriptcm | drift never lost: keeps reporting changed=true while blocked, worker converges once the worker-script cm returns | FIXED - TestVerifyPr6245WorkerSecretsRetryAndConvergeWhenScriptConfigmapMissing GREEN on head; RED on previous head d7e316803 (pass 2 was a false no-op; results/bites-round4-previous-head.txt). Contract re-scoped from "apply-or-error" to the round-3 pre-accepted live-template fix; the configmap leading the live template while blocked is now harmless bookkeeping. The pre-existing early-return hole for the OTHER eight synced fields is unchanged and out of this PR's scope. | fixed |
| F-sync-strategy-flip-advance | worker catches up on the reconcile after the strategy flip | FIXED - TestVerifyPr6245WorkerCatchesUpAfterStrategyFlip GREEN on head; RED on d7e316803 (worker stuck on [a]) | fixed |
| F-sync-override-not-recorded | rotation a->b + override a records the override; later removal falls back to b | FIXED - TestVerifyPr6245RotationThenOverrideRemovalConverges GREEN on head; RED on d7e316803 (override lost, worker stuck on [a]); PR's own TestSyncImagePullSecretsRotationThenOverrideRemoval also green | fixed |
| F-docs-recreate-note-stale | samples must not claim recreate-to-change | still RED (results/reverify-round4-head.json) - delta touches no docs | STILL-BROKEN (minor, docs-only; the remaining pre-merge MUST) |

Full package run at head with the harness grafted: the ONLY failing test is
TestVerifyPr6245DocsFreshness (the intentional stale-docs contract); every
repo test passes including the author's updated TestSyncImagePullSecrets
(results/repo-suite-round4-head.txt, single failure attributed via -json).

Note (behavior change, intentional): because the secrets diff is now taken
against the live template, the controller continuously enforces pod-template
imagePullSecrets - out-of-band manual edits to the worker sts / fuse ds
secrets are reverted on the next reconcile. Other synced fields still diff
recorded-vs-spec; secrets are now the only drift-corrected field. Safe
direction, consistent with controller semantics.

New tests this round (all in
`pkg/ddc/juicefs/sync_image_pull_secrets_verify_test.go`):
TestVerifyPr6245WorkerSecretsRetryAndConvergeWhenScriptConfigmapMissing
(replaces TestVerifyPr6245WorkerSecretsLostWhenScriptConfigmapMissing),
TestVerifyPr6245WorkerCatchesUpAfterStrategyFlip,
TestVerifyPr6245RotationThenOverrideRemovalConverges.

## Round 5 (incremental, head 2150b49ef) — observed vs expected

Delta `be3337dd00..2150b49ef`: single docs commit `2150b49ef` replacing the
stale "needs the runtime to be recreated" sentence in
docs/{en,zh}/samples/image_pull_secrets.md. PR body also updated: now
references `fixes #6248` and the special notes answer the carried question
Q-env-default-now-live-synced (env default live-sync is intentional;
out-of-band pod-secret edits are reverted).

Branch hygiene fix this round: the round-4 harness commit had been stacked on
the round-3 (d7e316803-based) tree instead of be3337dd00, so the branch's own
copies of pkg/ddc/juicefs/sync_runtime.go and sync_image_pull_secrets_test.go
were the pre-fix versions (merge-base with be3337dd00 was d7e316803). This
never affected re-verify RESULTS (re-verify.sh grafts only the harnessPaths
onto the real head), but the branch tree itself was stale; a corrective commit
restores every non-harness file to head 2150b49ef so the branch diff against
head is harness-only again. The stale tracked *.parsed files under
results/reverify/ are a second known quirk (the persist cp runs before the
cleanup checkout restores tracked files); the authoritative raw output is the
untracked unit.out / integration.out plus the round-named copies.

| ID | expected | observed | verdict |
|----|----------|----------|---------|
| A1-A7, AC9, AC10 contracts | still pass | all pass (results/reverify-round5-head-unit.txt, reverify-round5-head-integration.txt) | contracts hold |
| F1 (canary) | flips only if master/jobWorker wired | still passing = still inert | STILL-PRESENT (documented, by design) |
| F-docs-recreate-note-stale | recreate-claim gone from both samples | GREEN on head; sentence present at be3337dd (grep control, both languages), absent at 2150b49ef | FIXED |
| A11-fuse-rollout-is-lazy | default clean policy = OnRuntimeDeleted; sync enforces OnDelete fuse ds (flips RollingUpdate back), template converges without pod restart | PASS on head (TestVerifyPr6245FuseSyncNeverRestartsRunningPods) | confirmed mechanism pin |
| F-docs-fuse-restart-overstated | samples must not claim a secrets change restarts fuse pods | RED on head: en "the fuse pods are restarted", zh "fuse pod 会重启" (results/reverify-round5-head-integration.txt) | STILL-BROKEN (new, minor, docs-only) |

F-docs-fuse-restart-overstated mechanism (independently verified, matches the
Copilot flag on this head): charts/juicefs renders the fuse DaemonSet
updateStrategy OnDelete (values.yaml default) and syncFuseSpec forcibly flips
any other strategy to OnDelete before syncing the template, so a template
update never restarts running pods; the fuse generation bump recycles fuse
pods only lazily via CSI NodeUnstage (nodeserver.go getCleanFuseFunc /
checkIfFuseNeedUpdate) and only under cleanPolicy OnFuseChanged/OnDemand -
SetupFuseCleanPolicy maps the unset policy to OnRuntimeDeleted (no eager
clean). So on a default runtime the running fuse pods keep the old secrets
until recreated, exactly like the worker pods the same sentence describes
correctly. Harness-bites: the doc contract is RED on head, GREEN on a scratch
tree with the sentence reworded to the lazy/on-recreate behavior, and the
claim is absent at the previous head (control). Suggested fix: one clause -
"...takes effect on the next sync; the worker and fuse pods pick up the new
secrets when they are recreated (fuse pods are recreated lazily when
application pods churn if spec.fuse.cleanPolicy is OnFuseChanged)".

Repo suite on clean head 2150b49ef: go build ./... clean,
go test ./pkg/ddc/juicefs/ ok (results/repo-suite-round5-head.txt). CI green
at head (all 22 checks incl. unittest, kind-e2e, backward-compat; verified
read-only via gh).

## Round 5b (post-freeze addendum, live head a0baafae0) — observed vs expected

While this round was under review the author pushed `a0baafae0` "docs: say
that syncing image pull secrets doesn't restart running pods", rewording the
round-5 sentence to exactly the verified mechanism: templates update on the
next sync, no pod restart; worker pods pick up secrets as replaced; fuse ds is
OnDelete and with the default OnRuntimeDeleted clean policy running fuse pods
keep the old secrets until deleted (en+zh in parity). Re-verify on a0baafae0:
all 18 unit + 5 integration harness tests pass, including
TestVerifyPr6245DocsFuseRestartClaim - F-docs-fuse-restart-overstated is FIXED
at the live head (results/reverify-round5b-livehead-unit.txt,
reverify-round5b-livehead-integration.txt). CI on a0baafae0 still running at
review time (8 checks pass, e2e/backward-compat pending; was fully green on
2150b49ef).
