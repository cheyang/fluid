# curvine-thinruntime-image — verification harness (Reviewer A / Claude)

Reproducible evidence for the review of [PR #6293](https://github.com/fluid-cloudnative/fluid/pull/6293)
(`docs(curvine): use the published Curvine image in the ThinRuntime addon`).

Harness runs against the **code under review** (`1fa8d327695abc8f12eeafb99ffb6173d4a74703`, PR head).
Production code is untouched — the harness is additive (one Go test file + this docs tree).

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (Go) | the PR's `addons/curvine/runtime-profile.yaml` decoded and pushed through `ThinEngine.transformFuse`; asserts image/command/args reach the values that render the FUSE daemonset | `go test ./pkg/ddc/thin/ -run TestCurvineAddonProfilePropagation -count=1 -v` |
| 1b. Unit (script) | the manifest and all 5 profile snippets embedded in the readmes/dev-guides match the ThinRuntimeProfile CRD openAPI schema and carry the required image/command/args | `python3 docs/verification/curvine-thinruntime-image-claude/scripts/validate-manifest.py .` |
| 1c. Premise (script) | external facts: old image 404 on Docker Hub, upstream build assets removed, `ghcr.io/curvineio/curvine` public with `latest`, both upstream Dockerfiles ship the Fluid scripts at the documented paths, upstream example matches this PR | `bash docs/verification/curvine-thinruntime-image-claude/scripts/premise-check.sh` |
| 2. Integration | not applicable — the PR changes docs + one example manifest only; no controller code changed. The existing ginkgo suite `go test ./pkg/ddc/thin/...` was run as a regression sweep and passes. | `go test ./pkg/ddc/thin/... -count=1` |
| 3. Live | skipped per round policy (no KUBECONFIG layer in this debate setup). The real cluster contract (FUSE pod starts, mounts curvine) is what layer 1 approximates at the controller-transform level. | — |

> Test polarity: `TestCurvineAddonProfilePropagation` and `validate-manifest.py` are **contract**
> tests — they PASS on the PR head and go RED if the shipped manifest stops matching the CRD /
> controller behavior. No bug-canaries (no defects were confirmed).

## Problem premise (P0)

Run against the **base** branch facts and the live registries/upstream (the patch is docs-only;
the premise is about the world, not the patched code).

| | |
|---|---|
| Claimed symptom | "`runtime-profile.yaml` and the readmes use `fluid-cloudnative/curvine-thinruntime:v1.0.0`. That name can't be pulled"; the dev guide "builds it with `curvine-docker/fluid/thin-runtime/build-image.sh` and describes `fluid-config-parse.py`", both removed upstream |
| Linked issue | none (`NONE` in the PR body) |
| Reported component | `addons/curvine/` — Curvine ThinRuntime addon docs + example manifest |
| Patched component | exactly `addons/curvine/` (5 files, all docs/example) |
| Component match | Yes |
| **Verdict** | **Confirmed** (positive evidence) |
| Evidence | Docker Hub `fluid-cloudnative/curvine-thinruntime` → HTTP 404 (the fluid Docker Hub org is `fluidcloudnative`, no hyphen — the old name was wrong either way); upstream `build-image.sh` and thin-runtime README → 404; `ghcr.io/curvineio/curvine` public, tags include `latest` and `v0.6.0-alpha`; both `Dockerfile_rocky9` and `Dockerfile_ubuntu24` COPY `fluid/entrypoint.sh → /fluid-entrypoint.sh` and `config-parse.py → /app/curvine/`; `deploy/entrypoint.sh` execs `/fluid-entrypoint.sh` for `fluid-thin-runtime`; upstream example uses the same image/command/args as this PR. See `results/premise-check.log` (all 17 checks PASS). |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | the problem the PR claims (dead image + dead build instructions) exists on base | 1c | Confirmed | `results/premise-check.log` |
| C1 | the PR's manifest is a schema-valid ThinRuntimeProfile and its `fuse.command`/`fuse.args` are honored by the thin controller (profile → `value.Fuse` → daemonset `command:`/`args:`) | 1 | Confirmed (contract test passes) | `results/go-test.log`; existing tests `pkg/ddc/thin/transform_fuse_test.go:119-120` already cover profile Command/Args propagation |
| C2 | all 5 embedded profile snippets (en+zh readmes, en+zh dev guides, manifest) match the CRD schema and agree on image/command/args | 1b | Confirmed | `results/validate-manifest.log` |
| R1 | regression sweep: full thin package suite on PR head | 2 | Pass | `results/go-test-full.log` (`ok pkg/ddc/thin`, `operations`, `referencedataset`) |

### Advisory findings (no defect, no harness — recorded in the debate ledger)

- **F1 [minor]** `addons/curvine/runtime-profile.yaml:9` — `imageTag: latest` + explicit
  `imagePullPolicy: IfNotPresent` in the shipped example: nodes pin whatever snapshot they
  first pulled, and `latest` is mutable. Matches Curvine's own example and the PR body
  acknowledges a release tag works, so non-blocking; a pinned tag (e.g. `v0.6.0-alpha`) in the
  *shipped* manifest would be more reproducible.
- **F2 [minor]** `addons/curvine/dev-guide/curvine.md:14` — "build it from source" via
  `make docker-build` produces a **local image named `curvine:latest`**, not
  `ghcr.io/curvineio/curvine`; the guide no longer tells the developer to retag/push or point
  the profile's `image` at the built image (the old text at least named the resulting image).

## Harness-bites check

Performed on the PR head: temporarily deleting `command:`/`args:` from
`addons/curvine/runtime-profile.yaml` makes `TestCurvineAddonProfilePropagation` FAIL with
`fuse command: want [/entrypoint.sh], got []` / `fuse args: want [fluid-thin-runtime], got []`;
restoring the manifest makes it PASS. The test genuinely exercises the path it claims.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/curvine-thinruntime-image-claude` (pushed to the reviewer
fork `cheyang/fluid`); re-verification is one command from a checkout of that branch:

```bash
bash docs/verification/curvine-thinruntime-image-claude/scripts/re-verify.sh
# (no ref: fetches the current PR head from the manifest's pr URL)
```

1. Graft: `git checkout <fixed-ref> && git checkout verify/curvine-thinruntime-image-claude -- docs/verification/curvine-thinruntime-image-claude pkg/ddc/thin/addons_curvine_profile_test.go`
2. Prereqs: Layer 1 = Go toolchain (repo's `go.mod`); Layer 1b/1c = `python3` + `pyyaml` + network; no cluster needed.
3. Polarity: everything is a contract test — all GREEN = the addon manifest and the controller contract still hold. If the maintainer pins the tag per F1, update the expected values in the test/script and the C1/C2 expectations.
4. `scripts/premise-check.sh` and `scripts/validate-manifest.py` are recorded evidence but are NOT wired into `re-verify.sh` layers (it only parses go-test/ginkgo output); run them manually after a re-verify round.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/curvine-thinruntime-image-claude of the fork
cheyang/fluid (review harness for fluid-cloudnative/fluid PR #6293, docs-only curvine addon
image fix). Read docs/verification/curvine-thinruntime-image-claude/README.md
("Continuing after the fix") and follow it: run scripts/re-verify.sh (auto-resolves the PR
head), then scripts/validate-manifest.py and scripts/premise-check.sh manually. All tests are
contract polarity (green = holds). Report the observed-vs-expected table; do not modify
production code.
```
