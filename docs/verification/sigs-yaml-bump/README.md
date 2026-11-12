# sigs-yaml-bump — dependency-bump verification

Reproducible evidence for the review of [PR #6240](https://github.com/fluid-cloudnative/fluid/pull/6240)
(bump `sigs.k8s.io/yaml` 1.4.0 → 1.6.0, Dependabot, head `300d8ccf8`).

The PR is a pure dependency bump: `go.mod`, `go.sum`, and a vendor refresh
(vendor/ is excluded from line review per review-scope rules; it was verified
by building and by checksum verification instead). The verification question is
therefore not "is there a bug in the patch" but **"is the bump behavior-preserving
for every way fluid uses the library"** — plus "does the module graph stay
consistent".

Layers run against the **code under review** (`300d8ccf8`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `yaml.Marshal` / `yaml.Unmarshal` contract fluid depends on (golden output, scalar typing, merge keys, error text) | `go test ./pkg/ddc/juicefs/ -run 'TestSigsYaml' -v` |
| 2. Integration | the two consumer packages that round-trip YAML through their real code paths (juicefs engine incl. `transform`/values generation, dataprocess `generate_values.go`) | `go test ./pkg/ddc/juicefs/... ./pkg/dataprocess/...` |
| 3. Live | skipped per review setup (unit+integration only) | — |

> Test polarity: **all tests are CONTRACT tests** — they assert the behavior the
> bump must preserve. There are no bug-canaries: no defect was found to pin.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | (Dependabot chore) fluid pins `sigs.k8s.io/yaml` v1.4.0; upstream released v1.5.0/v1.6.0 (maintained fork migration, error wrapping, merge-key fix, kyaml) |
| Linked issue | none (no closing reference) |
| Reported component | go module `sigs.k8s.io/yaml` (vendored) |
| Patched component | `go.mod`/`go.sum` + `vendor/sigs.k8s.io/yaml`, new `vendor/go.yaml.in/yaml/v2` |
| Component match | Yes |
| **Verdict** | **Confirmed** — base pins the outdated version; the bump applies, builds, and is behavior-preserving (evidence below) |
| Evidence | `results/unit-base-differential.out` vs `results/unit-head.out`: the identical test file passes on both, byte-identical behavior |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | bump preserves Marshal/Unmarshal golden behavior | 1 | Confirmed (both heads identical) | `results/unit-head.out`, `results/unit-base-differential.out` |
| R1 | round-trip fidelity, merge keys, error text unchanged | 1 | Confirmed | same artifacts |
| R2 | consumer packages still pass with the new vendored lib | 2 | Confirmed | `results/integration-head.out` (0 fail events) |

## Static differential evidence (stronger than tests)

A file-by-file comparison of the vendored YAML v2 engine between base
(`vendor/sigs.k8s.io/yaml/goyaml.v2/` from v1.4.0) and head
(`vendor/go.yaml.in/yaml/v2/` v2.4.4) shows **all 13 source files byte-identical
except one comment URL line in `yaml.go`**. The only facade changes in
`vendor/sigs.k8s.io/yaml/yaml.go` are:

- `fmt.Errorf("while decoding JSON: %v", err)` → `%w` — renders identical text,
  only adds unwrapping capability;
- `DisallowUnknownFields` moved from `yaml_go110.go` (deleted) into `yaml.go` —
  same exported API.

So the 22 direct `sigs.k8s.io/yaml` call sites in `pkg/ddc/*` and
`pkg/dataprocess` (Helm-values `Marshal` in alluxio/jindofsx/jindo/jindocache/
juicefs/dataprocess `master_internal`/`load_data`/`generate_values`; config
`Unmarshal` in juicefs/thin/vineyard) execute the **same parser code** before
and after the bump.

Also verified: `go build ./...` passes on head; `go mod download` +
`go mod verify` confirm the new go.sum checksums (`sigs.k8s.io/yaml v1.6.0`,
`go.yaml.in/yaml/v2 v2.4.4`) are authentic; `vendor/modules.txt` is coherent.

## Environment noise (NOT PR-caused)

Four test failures were observed when running the wider engine suites on this
machine; **all four reproduce identically on the base worktree** (pre-existing,
local-environment), so they are not PR findings:

- `pkg/ddc/alluxio` `syncMetadataInternal` — "should successfully sync metadata and update dataset status" (`GetMetadataInfoFailed`)
- `pkg/ddc/jindofsx` `TestDestroyWorker`
- `pkg/ddc/jindo` `TestDestroyWorker`
- `pkg/ddc/cache/engine` `TestCacheEngine`

PR CI (GitHub status rollup) is green on the head.

## Live run notes

Skipped per the debate setup (unit + integration only).

## Proposed fixes

None — no defect found. The bump is behavior-preserving and was verified as such.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/sigs-yaml-bump-claude` (fork
`cheyang/fluid`), production code untouched. It doubles as the regression net
for the **next** yaml-library bump: any future `sigs.k8s.io/yaml` /
`go.yaml.in` upgrade that changes marshal/unmarshal semantics will turn these
contract tests red.

1. Graft it onto whatever ref you want to check:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/sigs-yaml-bump-claude
   git checkout <ref>
   git checkout FETCH_HEAD -- docs/verification/sigs-yaml-bump pkg/ddc/juicefs/sigsyaml_contract_test.go
   ```
2. Prereqs: Go toolchain only (module is vendored — no network needed).
3. Re-run: `bash docs/verification/sigs-yaml-bump/scripts/re-verify.sh` (no
   argument → auto-resolves the current PR head from `manifest.pr`), or the two
   layer commands in the table above.
4. Polarity: all tests are contract tests; they should be GREEN. A RED result
   means the code under test changed YAML behavior — treat that as the finding.
5. Harness-bites: see `results/bites-check.txt` (corrupted golden → RED).

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/sigs-yaml-bump-claude (fork cheyang/fluid).
Background: review of fluid PR #6240 (sigs.k8s.io/yaml 1.4.0 -> 1.6.0 bump) verified the
bump is behavior-preserving; the harness in docs/verification/sigs-yaml-bump/ pins that
contract. Re-check the current PR head: run scripts/re-verify.sh (or the layer commands
in README.md), confirm all contract tests stay green, and check the last-reviewed..head
delta for anything that changes YAML usage. Report an observed-vs-expected table.
```
