# Verification harness — PR #6211 (docs: Fluid security model)

Reviewer: Codex (Reviewer B). Round: initial. Reviewed head: `818e800087909d0f086c525888f6ef22adca33e3`.

## Premise (P0)

Docs-only PR: adds `security/security-model.md` + `security/security-model_zh.md`.
No linked issue; PR body states "This is a documentation-only change. It does not
change permissions or runtime behavior." Per the review rubric, docs PRs skip the
"why" defense; the substance reviewed here is **factual accuracy** — the document
sets its own bar: "A reference that no longer resolves is a documentation bug."
Component match: OK (docs touch docs). Premise verdict: **confirmed** (documents
a real, non-obvious trust model; every spot-checked claim was verified against
the code at the PR head — see the claim-guard tests below).

## Findings under verification

| ID | Severity | Claim | Polarity | Test |
|----|----------|-------|----------|------|
| F1 | minor | §3 cites `spec.fuse.command` (and `spec.fuse.args`) generically for "Runtime APIs with a FUSE spec", but `spec.fuse.command` exists only on ThinRuntime (args: Alluxio/Jindo/Thin) | contract | `TestL1_FuseCommandClaimScoped` |
| F2 | nit | §4 note cites the scaffolded roles as `*-editor-role.yaml`/`*-viewer-role.yaml`; real files use underscores (`dataload_editor_role.yaml`, ...) so the literal glob matches nothing | contract | `TestL1_ConfigRBACGlobPatternMatches` |

Both contract tests FAIL on the code under review (= reproduction) and PASS with
a proposed doc rewording applied (harness-bites check performed, then reverted;
the harness branch carries no production/doc changes beyond `docs/verification/**`).

## Layers

- **L1 unit (deterministic)**: `go test ./docs/verification/security-model-codex/ -run TestL1 -v`
  - Claim guards (all PASS at review head): YAML examples parse and are identical
    EN/ZH; relative links resolve; heading parity (33/33); 35 code-path references
    and 19 path→function citations resolve; 14 `data.fluid.io` + 1 `workload.fluid.io`
    CRD; config/rbac editor/viewer roles unreferenced; default `helmDriver: configmap`;
    Thin publish-secret copy naming/default-namespace/policy-default claims;
    reference-Dataset subpath escape guard; DataLoad/DataProcess cross-namespace
    rejection; unprivileged-mutator claims (privileged=false, SYS_ADMIN trimmed from
    Add only, dataset PVC→hostPath still applied).
  - Finding contracts (FAIL at review head): F1, F2 above.
- **L2 integration (helm render)**: `go test ./docs/verification/security-model-codex/ -run TestL2 -v`
  (requires the `helm` binary; skips otherwise). Renders the chart with both
  `helmDriver=configmap` and `helmDriver=secret` and asserts: no wildcard
  apiGroups/resources/verbs; cluster-wide Secret grants only under `secret`;
  both injection webhooks render `failurePolicy: Fail` with label selectors;
  `thinruntime-controller` has no Secret rule by default; the webhook's cert
  Secret access is a namespace-scoped Role. All PASS at review head.
- **L3 live**: not applicable to a docs-only PR (skipped by design).

## Observed vs expected

| Finding | Expected (intended) | Observed at 818e8000 | Verdict |
|---------|--------------------|-----------------------|---------|
| F1 | `spec.fuse.command` claim scoped to ThinRuntime | §3 line mentions `spec.fuse.command` without scoping (en+zh) | Confirmed (minor) |
| F2 | doc glob matches real files | `*-editor-role.yaml` matches 0 of config/rbac | Confirmed (nit) |
| All other §1–§9 claims | hold | hold (14 L1 + 4 L2 guards green) | Verified accurate |

## Continuing after a fix

`bash docs/verification/security-model-codex/scripts/re-verify.sh` (from a checkout
of this branch) fetches the current PR head via the manifest's `pr` URL, grafts
the harness, and reruns L1+L2. F1/F2 are contract tests: they report **FIXED**
once green. Claim guards are not findings; if a guard turns red after a code
change, the doc has gone stale — treat as Harness-update/doc-bug.

Kickoff prompt for a fresh machine:
> Check out branch `verify/security-model-codex` of the reviewer's fork, then run
> `bash docs/verification/security-model-codex/scripts/re-verify.sh`. Review the
> printed delta `<last-reviewed>..head` and advance `.last-reviewed`.
