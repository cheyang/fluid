# Verification — PR #6002 `fix: resolve disk-space exhaustion in backward-compat CI`

Reviewer: Codex (stage ② of review-pipeline). Production code untouched — this
branch adds only `docs/verification/pr-6002-ci-disk-space/`.

- PR: https://github.com/fluid-cloudnative/fluid/pull/6002
- Base (merge-base): `da69206da6388e46a8e9e32834d6b22e53fe14a9`
- Head under review: `27581121b535fd809804316feaa27bb70043c935`
- Layers run: L1 deterministic (stubbed binaries) + L2 git-level integration
  (YAML structure, three-way merge coherence vs current master). L3 (live GHA
  runner) intentionally skipped — the change is self-executing in CI.

## P0 — premise

Claim (PR body): *"The `backward-compat-test` CI job was failing with
`no space left on device` when loading Docker images to the kind cluster …
leaving ~36 MB free, not enough for `docker save` temp files."*

**Verdict: not-reproduced (historically corroborated; currently latent on
master) — NOT a blocker.**

- The June-2026 failure logs are past GitHub's 90-day retention; the original
  failing run cannot be re-inspected. (Not-reproduced is not refuted.)
- Corroboration that this failure class was real in this CI family at the time:
  master merged the same fix pattern for kind-e2e as #6123 (`5438f7a43`,
  2026-07-14, "fix: free disk space in kind-e2e CI to prevent out-of-disk
  failures"), and this PR's own June-14 run passed all 5 backward-compat jobs
  with the fix applied.
- Current state of master (evidence: `results/external/master-backcompat-disk.txt`):
  backward-compat job on 2026-11-04 shows `/dev/root 145G, 85G avail` before the
  build and 81–82G after — no disk pressure. Master's shared
  `build-all-images.sh` already prunes builder/buildx caches before loading and
  skips the ~470MB e2e-only images for backward-compat (`WITH_E2E_TEST_IMAGES`
  opt-in), which independently relieved the pressure.
- The only *recent* backward-compat failures (2026-11-05, dependabot PRs) are a
  Go toolchain mismatch (`go.mod requires go >= 1.26.0, running go 1.25.12`),
  unrelated to disk (`results/external/dependabot-failure-mode.txt`).

Net: the fix is cheap, standard, matches the maintainer-accepted #6123 pattern,
and restores parity for the one workflow that still lacks it. Worth merging even
though the acute failure is not currently observable.

## Observed vs expected

| check | layer | base (da69206) | head (27581121b) | meaning |
| --- | --- | --- | --- | --- |
| `rmi_after_load_pairing` | L1 | **fail** (10 loads, 0 rmis) | pass (10 load→rmi pairs, 0 violations) | PR adds rmi-after-load as claimed |
| `rmi_failure_tolerated` | L1 | **fail** (no rmi to fail) | pass (rc=0, 10 loads, 10 warnings) | `\|\| echo` guard beats `set -e` |
| `load_failure_aborts` | L1 | pass | pass (rc=1, 0 rmis after failed load) | `set -e` still aborts on load failure |
| `final_cleanup_runs` | L1 | pass | pass | trailing `cleanup_docker_caches` intact |
| `script_exits_zero` | L1 | pass | pass | happy path unchanged |
| `workflow_yaml_parses` / `free_disk_step_*` | L2 | **fail** (no such step) | pass (ordered kind-create < free-disk < build; expected contents) | workflow change present + correctly placed |
| `merge_clean` + `merge_keeps_*` | L2 | n/a | pass vs master `30d488bb7` (2026-11-04) | 5-month-stale PR merges conflict-free and preserves master's `WITH_E2E_TEST_IMAGES` + pre-load prune |

The L1/L2 fails on base are the harness-bites proof: the tests exercise exactly
the behavior this PR introduces, not incidental properties.

Downstream-consumer trace (static, code-path evidence): the only callers of
`build-all-images.sh` are `backward-compatibility-e2e.yml` and `kind-e2e.yml`.
After the script runs, both workflows only invoke `deploy-fluid-to-kind.sh`
(helm + sed on values.yaml; no docker/kind-load) and `gha-e2e.sh` /
`gha-backward-compatibility.sh` (kubectl/helm only). The e2e-only images
(`oss-emulator:e2e`, `mooncake:e2e`) are consumed cluster-side with
`imagePullPolicy: IfNotPresent` (`test/gha-e2e/jindo/oss-emulator.yaml`), never
from the host daemon. Removing host-local copies after `kind load` has no
consumer left to break. `docker image prune --all --force` in the new step runs
after kind cluster creation; the in-use `kindest/node` image is protected by
the daemon. The identical step has run green in kind-e2e on master since July.

## Re-run

From a checkout of this branch:

```bash
# against the current PR head (auto-discovered via manifest.pr):
bash docs/verification/pr-6002-ci-disk-space/scripts/re-verify.sh
# against a specific ref:
bash docs/verification/pr-6002-ci-disk-space/scripts/re-verify.sh <sha>
```

All six manifest checks are `contract`; "Fixed" means the PR's behavior is
present and intact on the target ref. `.last-reviewed` = head reviewed this
round; advance it after each round.

## Handoff / kickoff prompt for another machine

```
git fetch https://github.com/cheyang/fluid.git verify/pr-6002-ci-disk-space-codex
git checkout verify/pr-6002-ci-disk-space-codex
bash docs/verification/pr-6002-ci-disk-space/scripts/re-verify.sh   # needs: bash, git, jq, python3+PyYAML, network
```

No docker, kind, cluster, or kubeconfig required. Prerequisites: git ≥ 2.38
(`merge-tree --write-tree`), jq, python3 with PyYAML.
