# Verification — PR #6293: docs(curvine): use the published Curvine image in the ThinRuntime addon

- PR: https://github.com/fluid-cloudnative/fluid/pull/6293
- Reviewed head: `1fa8d327695abc8f12eeafb99ffb6173d4a74703` (base `b8ff60e309c341b58a9e26b085b76e6067e6bb60`)
- Harness: additive only — one Go test file + this directory. Production code untouched.

## Premise (P0) — CONFIRMED

Claim (PR body): the addon's old image `fluid-cloudnative/curvine-thinruntime:v1.0.0` cannot be
pulled, the old thin-runtime build assets were removed upstream (CurvineIO/curvine#571), and the
Fluid integration scripts now ship in the regular image `ghcr.io/curvineio/curvine`
(CurvineIO/curvine#1078), whose `/entrypoint.sh` hands off to `/fluid-entrypoint.sh` for the
`fluid-thin-runtime` argument.

Re-checked live on 2026-11-27 (see `results/premise-check.txt`, re-run
`scripts/premise-check.sh`):

| claim | observed | verdict |
| --- | --- | --- |
| old Docker Hub image gone | `hub.docker.com/v2/repositories/fluid-cloudnative/curvine-thinruntime` → HTTP 404 | confirmed |
| new image exists with `latest` | GHCR tag list contains `latest`, `v0.6.0-alpha`, … | confirmed |
| `build-image.sh`/`fluid-config-parse.py` removed | `curvine-docker/fluid/thin-runtime/` now contains only `curvine-thinruntime.yaml` | confirmed |
| Fluid scripts in regular image | `Dockerfile_rocky9` copies `fluid/entrypoint.sh` → `/fluid-entrypoint.sh`, `config-parse.py` → `/app/curvine/` | confirmed |
| entrypoint handoff | `deploy/entrypoint.sh` matches `fluid-thin-runtime` and `exec /fluid-entrypoint.sh "$@"` | confirmed |
| PR matches upstream example | upstream `curvine-thinruntime.yaml` uses identical image/command/args | confirmed |
| dev-guide build instructions valid | `make docker-build` target exists in Curvine root Makefile | confirmed |

## Findings / acceptance contracts

This is a docs + example-manifest PR; the review produced no code defects (one nit, see the
review document). The harness therefore encodes the PR's acceptance contracts as regression
tests — if a future revision breaks them, they fail.

| id | layer | polarity | test | status at 1fa8d32 |
| --- | --- | --- | --- | --- |
| P0 | script | premise | `scripts/premise-check.sh` | CONFIRMED |
| A1 | unit | contract | `TestVerifyPR6293CurvineAddonProfileParses` | PASS |
| A2 | unit | contract | `TestVerifyPR6293CurvineProfileFlowsIntoFuseValue` | PASS |
| A3 | script | contract | `scripts/chart-render-check.sh` | PASS (see `results/chart-render.txt`) |

- A1: the shipped `addons/curvine/runtime-profile.yaml` parses as a valid
  `ThinRuntimeProfile` with `image: ghcr.io/curvineio/curvine`, `imageTag: latest`,
  `command: [/entrypoint.sh]`, `args: [fluid-thin-runtime]`.
- A2: `ThinEngine.transformFuse` honors those profile fields
  (`pkg/ddc/thin/transform_fuse.go` `parseFromProfileFuse`), i.e. the values reach the fuse pod.
- A3: `charts/thin` renders `fuse.command`/`fuse.args`/image into the fuse DaemonSet container,
  closing the last mile from profile → pod spec.

## How to run

```bash
# unit layer (Go, deterministic)
go test ./pkg/ddc/thin/ -run 'TestVerifyPR6293' -v

# A3 chart render (needs helm)
bash docs/verification/curvine-addon-image-docs/scripts/chart-render-check.sh

# P0 premise (needs network; hits Docker Hub / GHCR / github.com)
bash docs/verification/curvine-addon-image-docs/scripts/premise-check.sh

# full re-verification of the unit layer against the current PR head
bash docs/verification/curvine-addon-image-docs/scripts/re-verify.sh
```

## Continuing after a fix / next round

All tests are `contract` polarity: PASS means the contract holds. If the addon example changes
legitimately (e.g. pinned tag instead of `latest`), update the expected values in
`pkg/ddc/thin/verify_pr6293_curvine_profile_test.go` — that is a Harness-update, not a failure.
`.last-reviewed` records the reviewed head; advance it after each round.
