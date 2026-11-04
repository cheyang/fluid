# lib.sh — shared helpers for the PR #6227 verification harness.
# All scripts are additive review artifacts; they never modify production code.
set -euo pipefail

repo_root() { git rev-parse --show-toplevel; }

# Resolve the toolchain the checked-out code pins, so the harness follows the
# ref under test (head pins go1.26.10, base pins go1.25.12).
repo_toolchain() {
  local root; root="$(repo_root)"
  awk '/^toolchain /{print $2; exit}' "$root/go.mod"
}

repo_go_directive() {
  local root; root="$(repo_root)"
  awk '/^go /{print $2; exit}' "$root/go.mod"
}
