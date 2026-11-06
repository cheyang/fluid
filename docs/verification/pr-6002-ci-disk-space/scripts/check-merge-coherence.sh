#!/usr/bin/env bash
# check-merge-coherence.sh — L2 (git-level integration) check for PR #6002.
#
# The PR head under review is months stale relative to master. This check
# three-way-merges the *current checkout* (the code under review) into a freshly
# fetched upstream master with `git merge-tree --write-tree` and asserts:
#
#   merge_clean                     textual merge is conflict-free
#   merge_keeps_rmi                 merged build-all-images.sh still contains the
#                                   PR's `docker rmi` line
#   merge_keeps_master_features     master's independent hardening of the same
#                                   script (WITH_E2E_TEST_IMAGES opt-in, pre-load
#                                   builder/buildx prune) survives the merge
#   merge_keeps_free_disk_step      merged workflow still has the Free disk space step
#
# Must be run from inside a git worktree whose HEAD is the ref under review.
# Output: TAB-separated "name<TAB>pass|fail" lines on stdout.
set -uo pipefail

UPSTREAM_URL="${UPSTREAM_URL:-https://github.com/fluid-cloudnative/fluid.git}"
RESULTS_DIR="${1:-$(pwd)/results/merge}"

verdict() { printf '%s\t%s\n' "$1" "$2"; }

mkdir -p "$RESULTS_DIR"

if ! git rev-parse --git-dir >/dev/null 2>&1; then
  for t in merge_clean merge_keeps_rmi merge_keeps_master_features merge_keeps_free_disk_step; do verdict "$t" fail; done
  echo "check-merge-coherence: not a git repo" >&2
  exit 0
fi

HEAD_SHA="$(git rev-parse HEAD)"
if ! git fetch --quiet "$UPSTREAM_URL" master; then
  for t in merge_clean merge_keeps_rmi merge_keeps_master_features merge_keeps_free_disk_step; do verdict "$t" fail; done
  echo "check-merge-coherence: cannot fetch upstream master" >&2
  exit 0
fi
MASTER="$(git rev-parse FETCH_HEAD)"

MT_OUT="$(git merge-tree --write-tree "$MASTER" "$HEAD_SHA" 2>&1)"
MT_RC=$?
TREE="$(printf '%s\n' "$MT_OUT" | head -1 | awk '{print $1}')"
printf 'master=%s\nhead=%s\nmerge-tree rc=%d tree=%s\n' "$MASTER" "$HEAD_SHA" "$MT_RC" "$TREE" > "$RESULTS_DIR/merge-tree.txt"
printf '%s\n' "$MT_OUT" | tail -n +2 >> "$RESULTS_DIR/merge-tree.txt"

[ "$MT_RC" -eq 0 ] && verdict merge_clean pass || verdict merge_clean fail

if [ "$MT_RC" -ne 0 ]; then
  verdict merge_keeps_rmi fail
  verdict merge_keeps_master_features fail
  verdict merge_keeps_free_disk_step fail
  exit 0
fi

SCRIPT=".github/scripts/build-all-images.sh"
WF=".github/workflows/backward-compatibility-e2e.yml"
MS="$(git show "$TREE:$SCRIPT" 2>/dev/null || true)"
MW="$(git show "$TREE:$WF" 2>/dev/null || true)"

printf '%s\n' "$MS" > "$RESULTS_DIR/merged-build-all-images.sh"
printf '%s\n' "$MW" > "$RESULTS_DIR/merged-backward-compatibility-e2e.yml"

case "$MS" in
  *'docker rmi "${img}" || echo "Warning: Failed to remove image'*) verdict merge_keeps_rmi pass ;;
  *) verdict merge_keeps_rmi fail ;;
esac

# master-side features that must survive (present on master since 2026-07; if
# master drops them later this check adapts by testing master's own content)
ok=1
if git show "$MASTER:$SCRIPT" | grep -q 'WITH_E2E_TEST_IMAGES'; then
  printf '%s' "$MS" | grep -q 'WITH_E2E_TEST_IMAGES' || ok=0
fi
if git show "$MASTER:$SCRIPT" | grep -q 'docker builder prune'; then
  printf '%s' "$MS" | grep -q 'docker builder prune' || ok=0
fi
[ "$ok" -eq 1 ] && verdict merge_keeps_master_features pass || verdict merge_keeps_master_features fail

case "$MW" in
  *"Free disk space"*) verdict merge_keeps_free_disk_step pass ;;
  *) verdict merge_keeps_free_disk_step fail ;;
esac
exit 0
