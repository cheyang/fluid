# Verification — PR #6292: fix(tools): describe the requested runtime in the diagnose scripts

Reviewer: Codex (Reviewer B, pr-debate pipeline). Production code is **untouched** on this
branch; the only additions are this directory and `test/verification/pr6292/`.

- PR: https://github.com/fluid-cloudnative/fluid/pull/6292
- Base: `b8ff60e309c341b58a9e26b085b76e6067e6bb60`
- Head reviewed: `073407aa56b5fa2396a1804e770f8e12386bfb09` (recorded in `.last-reviewed`)

## Premise (P0) — CONFIRMED

The PR claims `kubectl_resource` in the four diagnose scripts passes `${name}` — a variable
never set — to `kubectl describe <runtime-kind>`, so the archive's `<kind>runtime-<name>.yaml`
describes **every** runtime of that kind in the namespace instead of the one given with `-r`.

Verified against the **base** branch:

- Unit layer (stub kubectl): the stub records
  `describe alluxioruntime --namespace ns1` (and likewise for cacheruntime/jindoruntime/
  juicefsruntime) — no name argument. See `results/unit-base.json`.
- Integration layer (envtest + real kubectl + shipped CRDs, ns `ns1` holding runtimes
  `demo` and `decoy`): `alluxioruntime-demo.yaml` (etc.) also describes the unrelated
  runtime `decoy`. See `results/integration-base.json`.

Component match: the report and the patch both name `tools/diagnose-fluid-*.sh` —
`kubectl_resource`. Confirmed.

## Findings and observed-vs-expected

| ID | claim | layer | polarity | base (buggy) | head 073407aa (fixed) |
| --- | --- | --- | --- | --- | --- |
| F1 | each script must pass the `-r` runtime name to `kubectl describe <kind>` | unit (stub kubectl) | contract | FAIL (4/4 subtests: name arg missing) | PASS (4/4) |
| F1 | archive file for `demo` must describe only `demo`, not other runtimes in the ns | integration (envtest, real kubectl) | contract | FAIL (4/4: `decoy` leaks into `*-demo.yaml`) | PASS (4/4) |

There are no bug-canaries in this harness; all tests are contract tests. "All green" ==
fixed. If a future change re-breaks this, the same tests go red again.

## How to run

```bash
# unit layer (stub kubectl/helm; no cluster needed)
go test ./test/verification/pr6292/ -run 'TestDescribeRuntimeUsesRequestedName_Stub' -v

# integration layer (envtest; needs KUBEBUILDER_ASSETS or setup-envtest on PATH, and kubectl)
export KUBEBUILDER_ASSETS=$(setup-envtest use 1.31.0 -p path)
go test ./test/verification/pr6292/ -run 'TestDescribeRuntimeAgainstEnvtest' -v
```

Results captured in `results/` (`{unit,integration}-{base,head}.json`, `go test -json`).

## Re-verification after future pushes

From a checkout of this branch:

```bash
bash docs/verification/pr6292-diagnose-runtime-name-codex/scripts/re-verify.sh
```

No argument needed: the manifest's `pr` URL is used to fetch the current PR head
(machine-independent). The script grafts the harness onto that ref, runs the unit +
integration layers, and prints per-finding Fixed / Still-broken / Harness-update.
It also prints the incremental review range from `.last-reviewed`; advance the marker
after each round.

## Handoff notes / resuming elsewhere

- Prerequisites: Go (repo toolchain), kubectl on PATH for the integration layer,
  `setup-envtest` (or a pre-set `KUBEBUILDER_ASSETS`) for envtest binaries.
- The unit layer is fully deterministic (stubs `kubectl` and `helm`, asserts on the
  recorded argv). The integration layer installs `config/crd/bases` into envtest and
  seeds `demo`+`decoy` runtimes per kind in `ns1`.
- Copy-paste kickoff: "Check out branch verify/pr6292-diagnose-runtime-name-codex, run
  docs/verification/pr6292-diagnose-runtime-name-codex/scripts/re-verify.sh, then review
  the printed `<last-reviewed>..head` delta."
