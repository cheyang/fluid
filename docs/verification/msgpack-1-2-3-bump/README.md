# msgpack 1.2.2 -> 1.2.3 bump (fluid#6226) — verification

Reproducible evidence for the review of
<https://github.com/fluid-cloudnative/fluid/pull/6226> (Dependabot: bump msgpack
in `test/gha-e2e/mooncake/image/requirements.txt`).

Layers run against the **code under review** (head `0960c0d6fab76b1097535ea9352f3fbd6131b617`,
base `96a3765eaae0f23140aef9b30152538396ed7ec2`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | hash integrity of the lock entry vs PyPI; image-wheel coverage; mooncake version constraint | `bash docs/verification/msgpack-1-2-3-bump/scripts/run_unit.sh` |
| 2. Integration | real msgpack wheels from PyPI: leak repro (P0), guard/round-trip on the pinned version; `pip download --require-hashes --only-binary=:all:` for the image platform (cp312/manylinux x86_64) | `bash docs/verification/msgpack-1-2-3-bump/scripts/run_integration.sh` |
| 3. Live | none needed — the PR's own `kind-e2e-test` CI jobs build this exact image and run the mooncake e2e suite on 5 Kubernetes versions | see `results/ci-status.txt` |

> Polarity: every test is a **contract** test (pass = intended behavior). There are no
> bug-canaries in this harness: the premise test installs msgpack 1.2.2 explicitly and
> asserts the leak *reproduces* there, which remains true regardless of the PR revision.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | Dependabot security update: "Fix memory leaks in the C extension when map key validation or container hooks fail" (GHSA-j586-36cw-2gc2 / CVE-2026-114613; advisory: affected `< 1.2.3`, patched `1.2.3`, severity high) |
| Linked issue | none (security-driven dependency bump; advisory is the claim) |
| Reported component | msgpack C extension, consumed by `mooncake-transfer-engine-non-cuda` in the mooncake e2e image |
| Patched component | `test/gha-e2e/mooncake/image/requirements.txt` (msgpack==1.2.2 -> 1.2.3) |
| Component match | **Yes** — the image installs msgpack from this file; base pin 1.2.2 is in the advisory's affected range |
| **Verdict** | **Confirmed** |
| Evidence | 25 failed `strict_map_key` parses: 1.2.2 RSS +102404 KiB (4096 KiB/parse = the full 4 MiB map value leaks per parse, matching the advisory exactly); 1.2.3 RSS +0 KiB (`results/leak-1.2.2.json`, `results/leak-1.2.3.json`) |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | 1.2.2 leaks, 1.2.3 fixes (premise) | 2 | Confirmed | observed vs expected above; `results/integration.jsonl` |
| V1 | PR hash entry is genuine PyPI material, image wheel covered, every pin hash-locked | 1 | Confirmed | 91 hashes == PyPI's 91 published files for 1.2.3, 0 unknown; `results/unit.jsonl` |
| V2 | lock file resolves + hash-verifies for the image platform | 2 | Confirmed | 17 wheels downloaded under `--require-hashes --only-binary=:all:` for cp312/manylinux x86_64; `results/pip-download.log` |
| V3 | `mooncake-transfer-engine-non-cuda==0.3.12.post1` admits msgpack 1.2.3 | 1 | Confirmed | its requirement is unbounded (`msgpack` with no specifier); `results/unit.jsonl` |
| V4 | 1.2.3 preserves behavior the image relies on | 2 | Confirmed | strict_map_key still raises ValueError; pack/unpack round-trips pass; `results/integration.jsonl` |

Harness-bites check: the same layers run against the **base** requirements
(`results/base-requirements.txt`) flip `TestPinnedMsgpackDoesNotLeak` to **fail**
(leak present on 1.2.2) while installability/hash checks stay green — the harness
discriminates the fix, not just the environment (`results/integration-base.jsonl`,
`results/unit-base.jsonl`).

## Per-finding detail

- **P0 / TestPremiseLeakReproducesOn122 + TestPinnedMsgpackDoesNotLeak** — mechanism per the
  advisory: in `msgpack/unpack.h`, `unpack_callback_map_item()` rejects a non-str/bytes key
  *after* the map value was constructed, and the error path never releases the value. The
  worker unpacks `{42: <4 MiB bytes>}` under default `strict_map_key=True`; each failure leaks
  the whole 4 MiB value on 1.2.2, nothing on 1.2.3.
- **V1** — `scripts/check_hashes.py` compares the `msgpack==` entry's sha256 set against the
  live PyPI JSON (falls back to committed snapshots under `snapshots/` when offline).
- **V2** — `scripts/pip_download_check.sh` mirrors the Dockerfile's install flags, targeting
  the image platform tags (`manylinux2014/2_17/2_28 x86_64`, cp312). Needs PyPI reachability
  (`PIP_INDEX_URL` honored).
- **V4** — the bump must not "fix" the leak by dropping the validation or breaking normal
  use: guard and round-trip tests run against the pinned version.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/msgpack-1-2-3-bump-codex` (fork `cheyang/fluid`), production
code untouched.

1. `git fetch https://github.com/cheyang/fluid.git verify/msgpack-1-2-3-bump-codex`
2. From any clone of the repo, on any revision of the PR:
   `bash docs/verification/msgpack-1-2-3-bump/scripts/re-verify.sh`
   (auto-fetches the current PR head from `manifest.pr`, grafts this harness, runs L1+L2).
3. Prereqs: `python3` 3.10–3.13 with network to PyPI (wheels for msgpack 1.2.2 and the pinned
   version must exist for the host interpreter; on 3.14+ the 1.2.2 install builds from source
   and needs a C compiler), `pip`, `jq`, `git`. No docker/kind needed.
4. All tests are contract polarity: green = good. If a future revision re-pins a vulnerable
   msgpack, `TestPinnedMsgpackDoesNotLeak` goes red.
5. If the PR bumps msgpack again (e.g. 1.2.4), the unit layer fetches the new version's PyPI
   metadata live and self-adapts; with no network it needs a new snapshot (Harness-update).

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/msgpack-1-2-3-bump-codex of
https://github.com/cheyang/fluid.git. Background: review of fluid-cloudnative/fluid PR #6226
(Dependabot msgpack 1.2.2 -> 1.2.3 in test/gha-e2e/mooncake/image/requirements.txt). Premise
(GHSA-j586-36cw-2gc2 leak) was confirmed and the harness is green on head 0960c0d. Read
docs/verification/msgpack-1-2-3-bump/README.md and run scripts/re-verify.sh (no args; it
fetches the current PR head). All tests are contract polarity. Report observed vs expected.
```
