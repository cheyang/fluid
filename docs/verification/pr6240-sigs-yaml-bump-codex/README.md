# Verification — PR #6240: bump sigs.k8s.io/yaml 1.4.0 → 1.6.0

Reviewer: Codex (Reviewer B). Round: initial.
Base: `ff99142f9ec5c32a0c9166de023cb8936f0e5ae5` — Head: `300d8ccf8bdea8883187bdf1088ffbc8720ae14e`
Production code under review: **untouched** by this harness (additive files only).

## Premise (P0) — Confirmed

The PR is a Dependabot bump of `sigs.k8s.io/yaml`. Upstream changes in scope:
v1.5.0 (unhashable-key merge bugfix, `%w` error wrapping, goyaml.v2/v3
deprecation redirecting to `go.yaml.in/yaml`) and v1.6.0 (adds opt-in
`kyaml`/`yamlfmt` subpackages — **not vendored here**, nothing imports them).
The bump swaps the YAML engine under `sigs.k8s.io/yaml` from the embedded
`sigs.k8s.io/yaml/goyaml.v2` copy to `go.yaml.in/yaml/v2 v2.4.4` (the
maintained fork of the archived `gopkg.in/yaml.v2`).

**The vendored engine code is byte-identical between base and head**, except
one doc-comment URL in `yaml.go` (`diff -r` of all 15 `.go` files). The only
runtime behavioral delta in the whole bump is in the wrapper:
`jsonUnmarshal`'s error changed from `%v` to `%w` (sigs.k8s.io/yaml#106), so
`errors.Is/As` can now reach the stdlib `encoding/json` error. Error *strings*
are unchanged. Verdict: **confirmed safe mechanical bump**.

## Findings → evidence

| id | claim | layer | result |
|----|-------|-------|--------|
| EQ1 | YAML behavior identical base↔head for fluid's usage shapes | unit | **Confirmed** — 16-case matrix byte-identical except documented C6 delta; golden-pinned contract test green on BOTH revisions |
| EQ2 | YAML manifests round-trip through a real API server | integration (envtest) | **Confirmed** — Dataset + AlluxioRuntime create/get spec round-trip identical on base and head |
| —   | vendored module graph consistent | script | `go build ./...` OK, `go mod verify` OK, `go mod tidy` NO-OP at head |

No defects found. There is nothing to "fix", so polarity is uniformly
`contract`: the harness exists so future rounds (and future yaml bumps)
re-prove equivalence.

### The one intended behavioral delta (C6-errunwrap)

```
base (v1.4.0):  errors.As(err, *json.UnmarshalTypeError) = false
head (v1.6.0):  errors.As(err, *json.UnmarshalTypeError) = true
```
Additive improvement (upstream PR #106); no fluid code depends on
non-unwrappability (error strings identical).

### Harness sensitivity (bites check)

- Reverting `%w`→`%v` in the head's vendored `yaml.go` → C6 flips back to
  `false` (detected). 
- Removing `"on"` from the bool table in `go.yaml.in/yaml/v2/resolve.go` →
  C3 flips `"c_on":true`→`"c_on":"on"` (detected).

## How to run

```bash
# from a checkout of this branch
TOPIC=docs/verification/pr6240-sigs-yaml-bump-codex

# L1 behavior matrix on both revisions + diff (expect only the C6 line)
bash $TOPIC/scripts/run-behavior-matrix.sh ff99142f9ec5c32a0c9166de023cb8936f0e5ae5
bash $TOPIC/scripts/run-behavior-matrix.sh 300d8ccf8bdea8883187bdf1088ffbc8720ae14e
diff $TOPIC/results/behavior-ff99142f9.txt $TOPIC/results/behavior-300d8ccf8.txt

# standalone contract test (golden pins) — green on either revision
go test ./pkg/utils/sigsyamlverify/ -run TestYamlBehaviorMatrix

# L2 integration (needs envtest assets; override path as needed)
KUBEBUILDER_ASSETS=/home/ecs-user/envtest-assets/k8s/1.30.3-linux-amd64 \
  bash $TOPIC/scripts/run-envtest-integration.sh 300d8ccf8bdea8883187bdf1088ffbc8720ae14e

# module consistency
bash $TOPIC/scripts/check-module-consistency.sh 300d8ccf8bdea8883187bdf1088ffbc8720ae14e

# existing tests of every first-party sigs.k8s.io/yaml importer
bash $TOPIC/scripts/run-existing-tests.sh

# one-line re-verification after any force-push of the PR
bash $TOPIC/scripts/re-verify.sh
```

## Existing-test sweep attribution

5 of 12 swept packages fail **identically on base and head** on this machine
(`jindofsx`/`jindo` TestDestroyWorker, `alluxio` metadata spec 1/150,
`cache/engine` 12/344 mount-exec specs, `utils` TestCheckMountPointBroken).
Same test names, same counts, only durations differ
(`results/existing-tests-*.txt`). These are pre-existing environment-sensitive
failures (fake-exec/mount dependent), **not** caused by this PR; the PR's own
CI `unittest` job is green.

## Continuing after the fix

This PR is a dependency bump; if it is rebased/recreated by Dependabot, run
`scripts/re-verify.sh` (no args — resolves the current PR head from
`verify-manifest.json`'s `pr`). `Fixed` = matrix/envtest still green on the new
head. If upstream sigs yaml changes engine behavior in a later bump, the golden
test fails: re-record expectations deliberately, case by case.
Machine-local prerequisites: Go ≥1.26, envtest assets for L2 (re-download per
machine; e.g. `setup-envtest use 1.30.3`), network for module downloads.

Kickoff prompt for a fresh agent: "Check out
`verify/pr6240-sigs-yaml-bump-codex` from https://github.com/cheyang/fluid.git,
read docs/verification/pr6240-sigs-yaml-bump-codex/README.md, run
scripts/re-verify.sh against the current head of
https://github.com/fluid-cloudnative/fluid/pull/6240."
