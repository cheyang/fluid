#!/usr/bin/env bash
# F2 context check — REFUTED as a CI-relevant finding (introducedByPR=false either way).
#
# What this documents: `gofmt` output for ./pkg ./cmd ./api depends on the gofmt version.
#   - CI pins GO_VERSION (go1.24.12 in .github/workflows/project-check.yml): under that
#     gofmt, the merge-base AND current master are CLEAN — only PR #6021's base_test.go
#     is flagged (that is finding F1). This matches reality: master's Project Check
#     workflow is green while the PR's lint job fails at "Code fmt check".
#   - A newer gofmt (e.g. go1.27) additionally flags pkg/ddc/thin/transform_fuse.go
#     (mis-indented composite literal from upstream commit ea2351bd7). That is a
#     forward-compatibility note for whenever CI bumps GO_VERSION past that formatting
#     change — NOT a failure under the current CI contract, and not this PR's problem.
#
# Usage: bash check-gofmt-master-context.sh [repo-root]
set -u
ROOT="${1:-$(cd "$(dirname "$0")/../../../../.." && pwd)}"

GO_VERSION="$(sed -n 's/^.*GO_VERSION:[[:space:]]*//p' "${ROOT}/.github/workflows/project-check.yml" 2>/dev/null | head -1 | tr -d '[:space:]')"
GO_VERSION="${GO_VERSION:-1.24.12}"
case "${GO_VERSION}" in go*) ;; *) GO_VERSION="go${GO_VERSION}" ;; esac
GOFMT="$(GOTOOLCHAIN="${GO_VERSION}" go env GOROOT 2>/dev/null)/bin/gofmt"
if [ -x "${GOFMT}" ]; then
  echo "== F2 (context): ${GOFMT} -l ./pkg ./cmd ./api  [${GO_VERSION}, CI-pinned] =="
  OUT="$("${GOFMT}" -l "${ROOT}/pkg" "${ROOT}/cmd" "${ROOT}/api" || true)"
  echo "${OUT:-<clean>}"
fi
if command -v gofmt >/dev/null 2>&1; then
  echo "== F2 (context): PATH gofmt ($(go version 2>/dev/null)) -l ./pkg ./cmd ./api =="
  (cd "${ROOT}" && gofmt -l ./pkg ./cmd ./api || true)
fi
echo "(informational; exit 0 — pre-existing upstream state, not a gate on PR #6021)"
exit 0
