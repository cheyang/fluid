# Verification — PR #6229: bump container-storage-interface/spec 1.8.0 → 1.13.0

Reviewer: Codex (Reviewer B). Branch: `verify/csi-spec-1-13-bump-codex` (based on PR head
`7b5d0397f1d67d24cf61ee0faf680314a7c1d00f`; merge-base `30d488bb7ecd601fb8f338387d9b1fec1c4a2a2a`).
Harness is additive only — production code untouched (`git diff` vs head shows only this
directory plus `pkg/csi/plugins/zz_verify_pr6229_interface_contract_test.go`).

## P0 — Premise (runs against head, differential vs base): REFUTED

The PR's implicit claim (dependabot bump; upstream release notes say
"Breaking changes/Deprecations: None") is that csi spec v1.13.0 is a drop-in
replacement for v1.8.0. It is not, for Go implementers:

- spec ≤ v1.9.0 generated combined `csi.pb.go` whose server interfaces had no
  embedding requirement. spec ≥ v1.10.0 regenerated with a newer
  protoc-gen-go-grpc: every server interface now includes
  `mustEmbedUnimplementedXxxServer()` (verified: v1.9.0 has no
  `csi_grpc.pb.go`/mustEmbed; v1.10.0 does).
- fluid's CSI plugin chain `cmd/csi → pkg/csi → pkg/csi/plugins` is built on
  `github.com/kubernetes-csi/drivers/pkg/csi-common` (v1.0.2, repo
  **archived/EOL since 2021**), whose `DefaultIdentityServer`,
  `DefaultControllerServer`, `DefaultNodeServer` embed nothing.
- Result on head: `vendor/.../csi-common/utils.go:76,84,92` — `*DefaultIdentityServer
  does not implement csi.IdentityServer (missing method mustEmbedUnimplementedIdentityServer)`;
  the whole plugin chain (`pkg/csi/plugins`, `pkg/csi`, `cmd/csi`) no longer compiles.

CI agrees: `build` (both arches), `lint`, `staticcheck`, `unittest`, and all
`kind-e2e` / `backward-compat` jobs on 7b5d0397 fail at `make vet`
(Makefile:189) with this exact error (`results/ci-failure-excerpt.log`).
The same job on the merge-base commit is green (Project Check run 41834068553).

## Claims

| id | claim | polarity | layer |
|----|-------|----------|-------|
| C1 | `go build ./pkg/csi/... ./cmd/csi/...` succeeds | contract | unit |
| C2 | `go vet ./pkg/csi/...` succeeds (CI `make vet` subset, Makefile:189) | contract | unit |
| C3 | interface-satisfaction assertions in `zz_verify_pr6229_interface_contract_test.go` compile and pass | contract | unit |
| C4 | existing `go test ./pkg/csi/...` compiles and passes | contract | integration |

All four wrap the shell scripts in `scripts/` via `harness_test.go` and run with
the go.mod-pinned toolchain (`GOTOOLCHAIN` defaults to the `toolchain` directive,
go1.25.12, matching CI; override with `GOTOOLCHAIN=local`).

## How to run

```bash
# from the repo root of a checkout of this branch
go test -count=1 ./docs/verification/csi-spec-1-13-bump/
# or individually:
bash docs/verification/csi-spec-1-13-bump/scripts/check-build.sh
bash docs/verification/csi-spec-1-13-bump/scripts/check-vet.sh
bash docs/verification/csi-spec-1-13-bump/scripts/check-interface-contract.sh
bash docs/verification/csi-spec-1-13-bump/scripts/check-unit-tests.sh
```

## Observed vs expected

| claim | base 30d488bb (csi v1.8.0) | head 7b5d0397 (csi v1.13.0) | head + minimal fix* |
|-------|---------------------------|----------------------------|---------------------|
| C1 build | PASS (results/base-build-csi.log, exit=0) | FAIL (results/head-build-csi.log) | PASS |
| C2 vet   | PASS (part of base-harness-gotest.log, exit=0) | FAIL (results/head-vet-csi.log) | PASS |
| C3 contract | PASS | FAIL (`pkg/csi/plugins [build failed]`) | PASS |
| C4 unit tests | PASS | FAIL (`pkg/csi`, `pkg/csi/plugins [build failed]`) | PASS |
| full `go build ./...` | (green CI on merge-base) | only `kubernetes-csi/drivers/pkg/csi-common` broken (results/head-build-all.log) | — |

Aggregated harness runs: `results/head-harness-gotest.log` (exit=1, all four FAIL),
`results/base-harness-gotest.log` (exit=0), `results/head-with-fix-harness-gotest.log` (exit=0).

\* Harness-bites check: the minimal fix shape — embedding
`csi.UnimplementedIdentityServer` / `csi.UnimplementedControllerServer` /
`csi.UnimplementedNodeServer` into csi-common's three default servers
(3 one-line additions under vendor/, applied temporarily then reverted) — turns
all four contracts green on the PR head. This proves (a) the harness exercises
the real failure path, and (b) no further breakage hides behind the first
compile error. The vendor patch is a demonstration only, NOT a shippable fix:
`go mod vendor` would wipe it.

## Fix directions (author's choice)

1. **Close the bump** — fluid uses none of the spec 1.9–1.13 additions
   (VolumeGroupSnapshot, ModifyVolume GA, VolumeHealth, GetSnapshot, Snapshot
   Topology); nothing outside `pkg/csi/plugins` even imports the package.
2. **Pin `< v1.10.0`** (e.g. v1.9.x) — still compiles with csi-common v1.0.2,
   but keeps the EOL dependency.
3. **Do the bump properly**: drop/replace the archived
   `github.com/kubernetes-csi/drivers/pkg/csi-common` (inline the ~200 lines of
   helpers fluid actually uses, or fork with the three `Unimplemented*` embeds),
   then bump. This is the only path that makes ≥ v1.10.0 work.

## Continuing after the fix

```bash
git fetch https://github.com/cheyang/fluid.git verify/csi-spec-1-13-bump-codex
git checkout verify/csi-spec-1-13-bump-codex
bash docs/verification/csi-spec-1-13-bump/scripts/re-verify.sh          # auto-fetches current PR head
```

All findings have `contract` polarity: green = fixed. When csi-common is
replaced/inlined (finding comes back `Harness-update` if
`pkg/csi/plugins` stops importing it), keep
`zz_verify_pr6229_interface_contract_test.go` — the assertions still pin the
contract against whatever helpers replace csi-common; delete only if the
imports are gone (the file would then fail to compile for the wrong reason).

Kickoff prompt for a fresh agent: "Re-run the PR #6229 verification harness on
branch verify/csi-spec-1-13-bump-codex per
docs/verification/csi-spec-1-13-bump/README.md; all contracts must pass."
