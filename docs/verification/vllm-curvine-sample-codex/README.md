# Verification harness — PR #6253 (reviewer B / codex)

PR: https://github.com/fluid-cloudnative/fluid/pull/6253
Reviewed head: `33c1eef59404f55d4f582a0cec84af62fb2e1d79` (base `ff99142f9ec5c32a0c9166de023cb8936f0e5ae5`)
Round: initial. Production code untouched — this branch adds only `docs/verification/vllm-curvine-sample-codex/**`.

## Premise (P0)

Docs-only PR: adds a CacheRuntime/vLLM sample in en+zh plus TOC entries. The premise claim
checked here is "the sample is followable as written". Everything statically or
integration-decidable was verified: **P0 confirmed** at L1/L2. The live GPU run (L3) was the
author's own kind-cluster report; not repeated per round policy (see `verify-manifest.json`
`liveNote` for the observable signal).

## Claims → tests

| claim | test | layer | polarity |
| --- | --- | --- | --- |
| Embedded manifests parse, right kinds | TestDocManifestsParseAsYAML | unit | contract |
| en/zh manifests byte-identical | TestEnZhManifestParity | unit | contract |
| Unquoted heredocs have no `$`/backtick | TestHeredocsExpansionSafe | unit | contract |
| Relative links + TOC entries resolve | TestRelativeLinksResolve | unit | contract |
| Label selector / DataLoad phase / helm toggle / CRD fields match code | TestDocClaimsMatchSource | unit | contract |
| Delegated steps 1/2/4 exist in referenced doc with matching names | TestCrossReferencedStepsExist | unit | contract |
| "Only quota (1Gi→4Gi) differs" vs basic sample | TestRuntimeSampleDeltaIsOnlyQuota | unit | contract |
| Doc manifests accepted by real apiserver + round-trip | TestEnvtestAppliesDocManifests | integration | contract |
| **F1**: referenced doc must carry #6252 fixes | TestReferencedCurvineDocCarriesDependencyFixes | unit | contract |

## Observed vs expected (head 33c1eef)

| finding | observed | expected | verdict |
| --- | --- | --- | --- |
| P0 | all L1 checks pass; L2 apiserver accepts all 5 manifests, fields round-trip | same | Confirmed |
| F1 (merge-order dep on #6252) | test RED: referenced doc at base mangles `${CURVINE_HOME}`→`/conf` (results/f1-heredoc-mangling.txt) and pins `curvine/curvine:0.3.6-alpha` which 404s on Docker Hub (results/external-evidence.txt) | green only after #6252 merges | **OPEN — merge #6252 first** |

External pins verified 2026-11-16: `vllm/vllm-openai:v0.33.0`, `curvine/curvine:v0.3.6-alpha`,
`curlimages/curl:8.22.0` all exist; vLLM v0.33.0 `vllm-openai` image entrypoint is
`["vllm", "serve"]`; NVIDIA device-plugin README anchor `#shared-access-to-gpus` exists.

## Harness-bites proof

- F1: applied #6252's diff locally → `TestReferencedCurvineDocCarriesDependencyFixes` goes
  green; reverted → red again. (contract polarity: green == dependency satisfied)
- Parity: temporarily set zh quota 4Gi→5Gi → `TestEnZhManifestParity` red; reverted → green.

## How to run

```bash
# L1 unit (from repo root of this branch)
go test ./docs/verification/vllm-curvine-sample-codex/harness/ \
  -run 'TestDoc|TestEnZh|TestHeredocs|TestRelativeLinks|TestCrossReferenced|TestRuntimeSample|TestReferenced' -v

# L2 integration (needs envtest assets: kube-apiserver+etcd)
KUBEBUILDER_ASSETS=<dir with kube-apiserver+etcd> \
  go test ./docs/verification/vllm-curvine-sample-codex/harness/ -run TestEnvtest -v

# full re-verify against a newer PR head (grafts harness, prints per-finding verdicts)
bash docs/verification/vllm-curvine-sample-codex/scripts/re-verify.sh
```

envtest assets are machine-local; on this machine `/home/ecs-user/envtest-assets/k8s/1.31.0-linux-amd64`.
Elsewhere: `setup-envtest use 1.31.0 -p path`.

## Continuing after a fix

- When #6252 merges: F1 flips green with no harness change. Then this PR can merge.
- If either doc's sample YAML drifts beyond the pinned quota delta, `TestRuntimeSampleDeltaIsOnlyQuota`
  goes red = re-check the doc's "only one thing changes" sentence (harness-update semantics).
- `.last-reviewed` holds the reviewed head; advance it after each round.

Kickoff prompt for a fresh machine:
> Review fluid-cloudnative/fluid PR #6253. Fetch the fork branch verify/vllm-curvine-sample-codex,
> read docs/verification/vllm-curvine-sample-codex/README.md, then run
> `bash docs/verification/vllm-curvine-sample-codex/scripts/re-verify.sh` and review the
> `.last-reviewed`..head delta.
