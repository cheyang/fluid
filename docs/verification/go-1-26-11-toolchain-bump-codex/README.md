# Verification — PR #6255: chore(deps) bump Go 1.26.10 → 1.26.11 (GO-2026-6859)

Reviewer: Codex (Reviewer B). Round: initial.
Head: `e2d5e04e7da4bc46bce314a44d991f3f62d2e0c0` — Base: `f432bdc87ac35941763b74e8e87683c7a84de5d9`.

## Premise (P0)

The PR claims: go1.26.10 is affected by GO-2026-6859 / CVE-2026-97054 ("HTTP/1
server crash due to a `MaxBytesReader` header race in `net/http`"), the finding
is reachable in Fluid at symbol level, and go1.26.11 carries the fix.

**Verdict: CONFIRMED.**

- vuln.go.dev record: `GO-2026-6859`, aliases `CVE-2026-97054`, published
  2026-11-11; affected ranges `[0,1.26.11)` and `[1.27.0,1.27.4)` — so 1.26.10
  is affected and 1.26.11 is the fix on the 1.26 line (`results/vuln-db-record.txt`).
- govulncheck v1.9.0 (DB 2026-11-11) differential on this tree
  (`results/govulncheck-differential.txt`):
  - `GOTOOLCHAIN=go1.26.10` → `Vulnerability #1: GO-2026-6859` (stdlib),
    5 findings total;
  - `GOTOOLCHAIN=go1.26.11` → GO-2026-6859 gone; only the 4 pre-existing
    `k8s.io/kubernetes` module advisories remain (out of scope, unchanged).
- Component match: the report names the Go stdlib / toolchain; the PR touches
  exactly the toolchain pin (`go.mod`), the image builder pins (12 Dockerfiles)
  and the CI Go version pins. Match.

## Claims / acceptance criteria and results

| ID | claim (contract) | layer | head | base (control) | evidence |
|----|------------------|-------|------|----------------|----------|
| C1 | go.mod `toolchain` is outside GO-2026-6859 vulnerable ranges | unit (gotest) | PASS (go1.26.11) | FAIL (go1.26.10 flagged vulnerable) | `results/unit-head.txt`, `results/unit-base-bites.txt` |
| C2 | every `FROM golang:` builder is pinned `tag@sha256` on a fixed version, one shared digest across all 12 Dockerfiles | unit (gotest) | PASS (12/12, digest `dc40a13b…`) | FAIL (1.26.10 pins flagged) | same |
| C3 | no stale `1.26.10` / old-digest references in CI configs, go.mod, Dockerfiles | unit (gotest) | PASS | FAIL (stale refs flagged) | same |
| C4 | pinned digest == Docker Hub multi-arch index digest of `golang:1.26.11-bookworm` | script (network) | PASS (`verify-registry-digest.sh` exit 0; index covers linux/amd64, arm64/v8, arm/v7, 386, ppc64le) | n/a | `results/registry-digest.txt`, `results/image-index-platforms.txt` |
| C5 | govulncheck stdlib differential (head clean, baseline flags GO-2026-6859) | script (network) | PASS | n/a | `results/govulncheck-differential.txt` |
| C6 | project builds + vets + unit tests pass under go1.26.11 | unit/build | PASS (`go build ./...`, `go vet`, sample suites with documented `-gcflags="all=-N -l"` all green) | n/a | `results/build-vet-head.txt`, `results/unit-sample-head.txt` |
| C7 | PR CI green at head | external | PASS (24 checks incl. kind-e2e ×5 k8s versions, backward-compat ×5, build amd64+arm, unittest, lint, staticcheck, CodeQL, DCO) | n/a | `results/ci-status.txt` |

Harness-bites check: the gotest layer was grafted onto the base commit and all
three contract tests FAIL there (`results/unit-base-bites.txt`) — the red is
for the right reason (vulnerable versions + stale refs detected), so the green
on head is meaningful.

## How to re-run

Unit layer (offline, fast):

```bash
GOTOOLCHAIN=go1.26.11 GOFLAGS=-mod=vendor go test -count=1 \
  ./docs/verification/go-1-26-11-toolchain-bump-codex/harness/
```

Script layers (network):

```bash
bash docs/verification/go-1-26-11-toolchain-bump-codex/scripts/verify-registry-digest.sh
PATH=$PATH:$(go env GOPATH)/bin bash docs/verification/go-1-26-11-toolchain-bump-codex/scripts/verify-govulncheck.sh
```

Full loop after a new PR revision: `bash docs/verification/go-1-26-11-toolchain-bump-codex/scripts/re-verify.sh`
from a checkout of this branch (auto-fetches the current PR head, grafts the
harness, runs the unit layer).

## Caveats / notes

- Local dev machines on go1.27.0–1.27.3 still build Fluid with a stdlib
  affected by GO-2026-6859 (fixed only in 1.27.4), because `GOTOOLCHAIN=auto`
  prefers a newer local toolchain over the `toolchain go1.26.11` pin. Shipped
  artifacts (images, CI) are all pinned to 1.26.11 and covered. Informational.
- Whether to backport the bump to active `release-*` branches is a maintainer
  decision; those branches still pin 1.26.10.
- During verification, `pkg/utils` `TestCheckMountPointBroken` and
  `pkg/utils/helm` `TestHelm` failed **only** when run without the documented
  `-gcflags="all=-N -l"` (gomonkey monkey-patching needs inlining disabled).
  Same-commit differential with go1.26.10 failed identically → harness
  artifact, not a PR regression. With the documented flags both pass.
