#!/usr/bin/env bash
# verify-manifests.sh — F2: does `make manifests`-equivalent regeneration at the
# PR head leave the tree dirty (the PR-caused lint CI failure)?
#
# Mirrors the CI lint step: controller-gen with the repo's pinned options
# (v0.19.0, `crd webhook paths=./... output:crd:...`), then `git status
# --porcelain`. The CI wraps this in "CRD validation failed. Please use
# 'make update-crd' to keep CRDs latest" and exits 1 when the tree is dirty.
#
# Usage: bash verify-manifests.sh [<repo-dir>]   (default: repo root of this branch)
set -euo pipefail

REPO="${1:-$(git rev-parse --show-toplevel)}"
cd "$REPO"

if [ ! -x "${CG:-}" ] && ! command -v controller-gen >/dev/null; then
  CG="$(mktemp -d)/controller-gen"
  GOBIN="$(dirname "$CG")" go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0
  CG="$CG"
else
  CG="${CG:-controller-gen}"
fi

BEFORE="$(git status --porcelain)"
"$CG" crd webhook paths="./pkg/..." output:crd:artifacts:config=config/crd/bases
AFTER="$(git status --porcelain)"

# restore the tree so this script is side-effect free
git checkout -- config/ 2>/dev/null || true

# Only MODIFIED tracked files count (untracked files are this harness, not the regeneration)
TRACKED_AFTER="$(printf '%s' "$AFTER" | grep -E '^[ MARC][MD]' || true)"
echo "dirty files before regeneration: [${BEFORE}]"
echo "dirty files after regeneration:  [${AFTER}]"
echo "modified tracked files:          [${TRACKED_AFTER}]"
if [ -n "$TRACKED_AFTER" ]; then
  echo "RESULT: regeneration produces a diff -> the CI lint gate ('make update-crd') fails on this PR."
  exit 1
fi
echo "RESULT: regeneration is a no-op -> lint gate would pass."
