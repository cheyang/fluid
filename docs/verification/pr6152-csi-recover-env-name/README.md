# pr6152-csi-recover-env-name — bug verification

Reproducible evidence for the findings raised while reviewing
[fluid-cloudnative/fluid#6152](https://github.com/fluid-cloudnative/fluid/pull/6152)
("update env name for RECOVER_WARNING_THRESHOLD", fixes #6151).

The PR is a two-line chart change: it renames the env var the CSI daemonset sets from the
misspelled `REVOCER_WARNING_THRESHOLD` to `RECOVER_WARNING_THRESHOLD`, and tidies a
`| quote}}` → `| quote }}` spacing nit.

Refs under test:

| Role | Ref |
|------|-----|
| Pre-fix (merge-base) | `7c00ed2b3353269ab164551d643a57b26b7bb3f7` |
| Post-fix (PR head) | `53d2017247417bd09b0ac0dcf34a12db997c6b0b` |

Production code is **untouched** on this branch; the diff is the harness only.

## Layers

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | threshold resolution in `pkg/csi/recover` (`GetIntValueFromEnv` + fallback) | `go test ./pkg/csi/recover/ -count=1 -run 'TestThresholdDropped\|TestThresholdHonoured\|TestNoGoConstant'` |
| 2. Render (integration) | the real `helm` binary rendering the real `charts/fluid/fluid` | `go test ./test/chartverify/ -count=1` |
| 3. Live | not used — see "Why no live layer" | — |

Because the defect lives in the chart rather than in Go, **layer 2 is the layer that
discriminates the fix.** Layer 1 is green on both refs by design and exists to quantify the
impact.

> Test polarity: all four findings are **contract** polarity (must PASS on fixed code), which
> is what `re-verify.sh` keys on. But only F1 and F2 *discriminate the fix* — they are the ones
> that go RED pre-fix. F3 and F4 are green on any ref and document impact and context; do not
> read them as fix confirmation. The manifest records this as `discriminatesFix`.
> There are no bug-canaries here, so nothing needs inverting after the fix.

## Summary of results

| ID | Claim | Layer | Polarity | Verdict | Evidence |
|----|-------|-------|----------|---------|----------|
| F1 | Chart must emit the threshold under the name Go reads; pre-fix it emitted `REVOCER_WARNING_THRESHOLD`, so `csi.recoverWarningThreshold` was silently dropped | 2 | contract | **Confirmed — fixed by this PR** | pre-fix RED: rendered env names `[… REVOCER_WARNING_THRESHOLD …]`, no `RECOVER_WARNING_THRESHOLD`; post-fix GREEN with `=100`. `results/` |
| F2 | Every env name the chart sets on the csi-plugin container should be read by something | 2 | contract | **Confirmed — fixed by this PR** | pre-fix RED: `unread: [REVOCER_WARNING_THRESHOLD]` (exactly one, no false positives); post-fix GREEN: all 8 consumed |
| F3 | With only the misspelled name set, the operator's value is discarded and the threshold silently becomes `defaultRecoverWarningThreshold` = 50 | 1 | contract (no discrim.) | **Confirmed** | `configured=100 via REVOCER_WARNING_THRESHOLD -> effective threshold=50` |
| F4 | The chart toolchain is silent about an env name nobody reads, so CI's `helm lint` sweep could not have caught this | 2 | contract (no discrim.) | **Confirmed** | render of the misspelled chart exits 0 with no warning |

Net: the PR is a correct and complete fix for a real, previously unguarded defect. No
blocking issue found in the change itself. The one gap that remains is F2 — the *guard*
against this drift recurring, which this PR does not add.

## Per-finding detail

### F1 — the operator's value never reached the process

`charts/fluid/fluid/values.yaml:78` exposes `csi.recoverWarningThreshold`, and the daemonset
template gated it on `{{- if .Values.csi.recoverWarningThreshold }}`, so users could set it.
But the emitted env name was `REVOCER_WARNING_THRESHOLD`, while the only reader —
`pkg/csi/recover/recover.go:49` — declares:

```go
RecoverWarningThreshold = "RECOVER_WARNING_THRESHOLD"
```

A tree-wide search confirms the asymmetry: pre-fix, `REVOCER` appears **once**, in the chart
template and nowhere else; `RECOVER_WARNING_THRESHOLD` appears **once**, in Go and nowhere
else. The two halves never met.

```
pre-fix  → FAIL: csi-plugin container has no env var named RECOVER_WARNING_THRESHOLD …
                 rendered env names: [MOUNT_ROOT REVOCER_WARNING_THRESHOLD ALLOW_PATCH_STALE_NODE
                 CSI_ENDPOINT NODEPUBLISH_METHOD NODE_ID KUBELET_ROOTDIR NODE_IP]
post-fix → PASS
```

### F2 — nothing guarded the chart↔Go env-name contract

`.github/workflows/project-check.yml:83` is the only chart check:

```bash
find ./charts | grep Chart.yaml | xargs dirname | xargs helm lint
```

`helm lint` validates chart structure and template syntax. Both spellings are equally valid
YAML strings, so lint is structurally incapable of noticing. `TestCsiPluginEnvNamesAreAllConsumed`
closes that gap by rendering the chart and asserting each emitted env name is grepped up in
`pkg/`, `cmd/`, or `csi/` (with `NODE_ID` / `CSI_ENDPOINT` allowlisted, since those are
dereferenced via `$(VAR)` in the container's own `args`). Pre-fix it names the offender and
nothing else.

### F3 — blast radius is narrower than it looks

`defaultRecoverWarningThreshold` is 50 (`recover.go:46`) and the chart's own default is also
`50` (`values.yaml:78`). So for any install that left the value alone, the effective threshold
was 50 before and after — **the bug was invisible at the default**, which is why it survived.
Only operators who explicitly tuned `csi.recoverWarningThreshold` to something other than 50
were affected; they silently got 50. The consequence is confined to when
`FuseRecover.recover` logs `excessive mount count detected` and increments
`umountDuplicate` behavior around `recover.go:276-280` — a warning/observability threshold,
not data correctness.

This also means the fix carries essentially **zero regression risk**: default installs render
a functionally identical value under a new name, and customized installs start getting what
they always asked for.

### F4 — why it went unnoticed

Rendering the deliberately-misspelled chart exits 0 with no diagnostic. Helm has no notion of
which env names the image reads, so no chart-level tool can catch this class of drift; only a
cross-check against the consuming source can.

## Why no live layer

The fix is a field in a rendered manifest, fully observable at the render layer, so an L3 run
adds no discriminating power. If you want end-to-end confirmation anyway, see `liveNote` in
`verify-manifest.json`: install with `--set csi.recoverWarningThreshold=<n != 50>`, confirm the
daemonset's `plugins` container carries `RECOVER_WARNING_THRESHOLD=<n>`, then confirm the
csi-nodeplugin log reports `threshold=<n>` rather than `threshold=50`.

## Harness-bites check

Done, and it caught two real harness defects before any conclusion was drawn:

1. The first render implementation invoked `helm template` on the whole chart, which hit
   `templates/webhook/plugins-profile.yaml`'s `lookup` call and tried to reach the reviewer's
   cluster — making results depend on an unreachable API server. Fixed by rendering a temp
   copy with `lookup`-calling templates removed and `KUBECONFIG=/dev/null`.
2. The first coverage-gap test asserted on `helm lint` exit status, which failed for an
   unrelated reason (local helm v3.1.1 predates `lookup`, so linting this chart errors out
   regardless). Replaced with a claim the local toolchain can actually support.

After those fixes: grafted onto pre-fix `7c00ed2b`, the three contract assertions go RED with
the right messages while `TestScraperSanity` stays GREEN — so the failures are the defect, not
a broken parser. Grafted onto PR head `53d20172`, everything is GREEN. Production diff verified
empty on both.

## Proposed fixes (NOT applied to production here)

- **F1** — already fixed by this PR as written. No change requested.
- **F2** — optional follow-up, not a merge blocker for a typo fix: add a chart-render check
  that fails when the chart sets an env var no source file reads.
  `test/chartverify/csi_env_render_verify_test.go` in this branch is a working implementation
  and could be upstreamed roughly as-is (it needs only `helm` on PATH and skips without it).
- **Also worth a look, pre-existing and out of scope for this PR:**
  - `RECOVER_FUSE_PERIOD` (`recover.go:48`) is read by Go but never set by any chart, so
    `csi.recoverFusePeriod` has no values.yaml knob at all — the mirror image of this bug.
  - `{{- if .Values.csi.recoverWarningThreshold }}` treats `0` as unset (Helm falsiness), so
    the value cannot be explicitly zeroed. Probably harmless for a threshold.

## Environment notes / limitations

- The render layer needs `helm` on PATH and **skips** without it. Verified with helm v3.1.1.
- 3 specs in the pre-existing ginkgo suite `TestRecover` fail on darwin with
  `util/mount on this platform is not supported`. Confirmed identical on the merge-base with
  the harness removed, so this is **unrelated to PR #6152 and to this harness** — a macOS
  limitation of `k8s.io/utils/mount`. CI's linux `unittest` job is green. The `-run` filter in
  the manifest's unit layer avoids them.
- All 23 GitHub checks on the PR were green at `53d20172` when reviewed.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/pr6152-csi-recover-env-name` (production untouched), so it
grafts onto whatever the code becomes.

```bash
git fetch origin verify/pr6152-csi-recover-env-name
git checkout origin/verify/pr6152-csi-recover-env-name
bash docs/verification/pr6152-csi-recover-env-name/scripts/re-verify.sh
```

`re-verify.sh` takes no sha: it resolves the current PR head from `manifest.pr` and the review
delta from `.last-reviewed`. Requires `git`, `go`, `jq`, and `helm`.

Reading the result: F1/F2 are contract tests and must be GREEN on fixed code. F3 (`mechanism`)
and F4 (`evidence`) are green on both refs — do **not** read them as fix confirmation. Nothing
needs inverting, since there are no bug-canaries.

To re-confirm the harness still bites, graft it onto `7c00ed2b` and check that F1/F2 go red
while `TestScraperSanity` stays green.

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/pr6152-csi-recover-env-name (origin =
github.com/cheyang/fluid). Background: reviewing fluid-cloudnative/fluid#6152 produced
findings F1-F4 about a Helm chart env var name typo (REVOCER_WARNING_THRESHOLD ->
RECOVER_WARNING_THRESHOLD); a render-layer harness confirmed F1/F2 and the PR as written
fixes them. Read docs/verification/pr6152-csi-recover-env-name/README.md ("Continuing after
the fix") and follow it: run scripts/re-verify.sh (no sha needed), then incrementally review
the .last-reviewed..head delta for regressions. Mind the polarity table — F3 is `mechanism`
and F4 is `evidence`, both green on any ref, so neither confirms a fix. Report an
observed-vs-expected table and advance .last-reviewed. The render layer needs `helm` on PATH.
```
