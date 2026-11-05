# gomonkey v2.13.0 → v2.14.4 bump — verification harness (PR #6228)

Reviewer: Claude (Reviewer A). Branch `verify/gomonkey-bump-claude`, based on PR head
`a6aa6afdd6c1c4062b9f12458aa82d4ceb033ed7` (base `30d488bb7ecd601fb8f338387d9b1fec1c4a2a2a`).
Production code is untouched — the only additions are this harness
(`pkg/utils/gomonkeycompat/` + this directory).

## Premise (P0) — verdict: NOT-REPRODUCED (linux/amd64), not refuted

The PR is a Dependabot bump of `github.com/agiledragon/gomonkey/v2` 2.13.0 → 2.14.4
(test-only dependency: 88 fluid test files import it; zero non-test imports). Upstream
release notes claim 2.13.x crashes on amd64 when CPU profiling/unwinding samples a
patched entry (`invalid runtime symbol table`), clobbers R10 on arm64 (11th argument
slot), and SIGBUSes on darwin/arm64.

Reproduced the *symptom* claim on **base** (2.13.0), linux/amd64, with both go1.27.0 and
the CI-pinned go1.25.12: 1 second of 100 Hz CPU profiling over a patched function plus a
goroutine-dump unwind completes cleanly (`results/unit-harness-base.log`). So the amd64
claim does not reproduce in fluid's environment — **not-reproduced**, which per policy is
**not** a blocker (Go-version/unwind dependent; arm64 not runnable on this x86 host).
What matters for merge is the reverse question — *does the bump regress anything?* — and
that answer is a clean no (see below).

## What the harness proves (observed vs expected)

| claim | layer | base (2.13.0) | head (2.14.4) | verdict |
| --- | --- | --- | --- | --- |
| gomonkey semantics fluid relies on (ApplyFunc/ApplyMethod/ApplyMethodFunc/ApplyPrivateMethod/Seq/Return/Origin/Reset, re-patch, 1000 patch-reset cycles, 256 live patches) | unit | pass | pass | **no regression** (GOMKEY-SEMANTICS) |
| CPU profile + stack unwind over a patched entry | unit | pass | pass | **safe both ways** (GOMKEY-PROFILING); upstream's crash not reproduced on base |
| 300 simultaneously-live distinct patches | unit | all 300 ok (uncapped) | panic at #257: `no reachable amd64 patch trampoline available` | **behavior change, recorded** (GOMKEY-POOL-CAP canary) |
| full fluid unit sweep (`make test` equivalent, `-gcflags=all=-N -l`) | project | 69 ok / 1 env-contention FAIL (see below) | 70 ok / 0 FAIL | **no regression** |
| race detector on gomonkey-heavy packages | project | pass | pass | **no regression** |
| cross-compile gomonkey-using test binaries: linux/arm64, darwin/amd64, darwin/arm64, windows/amd64, linux/386 | build | (not run) | all OK | new `.s`/build-tag files compile on every CI-relevant platform |
| vendor consistency | build | n/a | `go mod vendor` idempotent; go.sum delta is gomonkey-only | **clean bump** |

The single base-sweep FAIL (`pkg/utils`, `panic: listen tcp 127.0.0.1:6060: bind:
address already in use`) was caused by running the base and head sweeps in parallel on
one machine — fluid's `NewPprofServer` test binds a fixed port. Re-run serially, base
`pkg/utils` passes (112 s). Pre-existing test-infra quirk, unrelated to this PR; recorded
as finding PPROF-PORT-6060 (non-blocking).

## On linux/amd64, what actually changed in 2.14.4

`ApplyCore` (all public function/method patches) no longer writes a 12-byte
`MOV rdx, double; JMP [rdx]` at the function entry. It now writes a 5-byte
`JMP rel32` into a 256-slot, 16-byte-per-slot trampoline pool inside gomonkey's own text
(`NOSPLIT|NOFRAME`, zero stack delta), each slot holding the old 12-byte jump to the
double. `Reset()` releases slots. Private-method patches
(`ApplyPrivateMethod`, 86 uses in fluid) keep the old direct-write path on linux.
Consequences verified above: a 5-byte entry write is strictly safer than the old 12-byte
write for short functions, and unwinding through the entry is now metadata-clean — but
live patches are capped at 256 per process (panic with a clear message, not corruption).

## How to re-run

```bash
go test -count=1 -gcflags="all=-N -l" ./pkg/utils/gomonkeycompat/   # inline disabling is mandatory (fluid Makefile does the same)
bash docs/verification/gomonkey-bump/scripts/re-verify.sh           # grafts harness onto current PR head, applies polarity
```

Full differential evidence: `results/` (unit harness on head+base, full sweep head+base,
race, CI-toolchain go1.25.12, cross-compile matrix).

## Continuing after the fix / next round

If the PR advances (e.g. another gomonkey version or a revert), run
`scripts/re-verify.sh` with no argument — it fetches the current PR head from the
manifest `pr` URL, grafts this harness onto it, and prints per finding:

- `GOMKEY-SEMANTICS` (contract) — FIXED when all listed tests pass.
- `GOMKEY-POOL-CAP` (canary) — FIXED only when it *flips to fail*, i.e. the 256-slot
  cap is lifted or the panic becomes silent corruption; then invert the assertion.
- `GOMKEY-PROFILING` (contract) — must keep passing; a red here means a patched entry
  breaks profiling/unwinding again.

Requirements: git, go ≥ 1.25, jq. Linux or macOS; on darwin/arm64 the canary exercises
the darwin trampoline path instead.

Kickoff prompt for a fresh agent on any machine:

> You are resuming the review of fluid PR #6228 (gomonkey 2.13.0→2.14.4). Checkout
> branch `verify/gomonkey-bump-claude` from the reviewer fork, read
> `docs/verification/gomonkey-bump/README.md`, run
> `bash docs/verification/gomonkey-bump/scripts/re-verify.sh`, report the per-finding
> verdict table honoring canary polarity, then review the `.last-reviewed..head` delta.
