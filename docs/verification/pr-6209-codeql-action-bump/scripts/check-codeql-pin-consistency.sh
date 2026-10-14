#!/usr/bin/env bash
# Layer 1 (unit, offline): every github/codeql-action/* usage in .github/workflows/
# must be pinned to a full-length commit SHA, and all usages must share ONE sha.
# Rationale (quoted from .github/dependabot.yml):
#   "Bump every github/codeql-action/* step (init, analyze, autobuild,
#    upload-sarif) together. They must run on the same version, otherwise
#    analysis fails with 'Loaded a configuration file for version X, but
#    running version Y'."
# Emits go-test -json style lines so scripts/re-verify.sh can parse results.
set -u
cd "$(git rev-parse --show-toplevel 2>/dev/null || echo .)"

emit() { # name, pass|fail, detail
  printf '{"Test":"%s","Action":"%s"}\n' "$1" "$2"
  [ "$2" = "fail" ] && printf '  %s\n' "$3" >&2
  return 0
}

uses_lines="$(grep -RhoE 'uses: github/codeql-action/[a-z-]+@[0-9a-f]+.*' .github/workflows/ 2>/dev/null || true)"
if [ -z "$uses_lines" ]; then
  emit "GroupUsesSingleSHA" "fail" "no github/codeql-action usages found under .github/workflows/"
  exit 1
fi

# Contract 1: pins are full 40-hex SHAs (OSSF Pinned-Dependencies)
bad_short=0
while IFS= read -r line; do
  sha="$(printf '%s' "$line" | sed -E 's/.*@([0-9a-f]+).*/\1/')"
  if ! printf '%s' "$sha" | grep -qE '^[0-9a-f]{40}$'; then
    emit "PinnedToFullLengthSHA" "fail" "not a full 40-hex SHA: $line"
    bad_short=1
  fi
done <<< "$uses_lines"
[ "$bad_short" = 0 ] && emit "PinnedToFullLengthSHA" "pass" ""

# Contract 2: exactly one distinct SHA across all codeql-action usages
distinct="$(printf '%s\n' "$uses_lines" | sed -E 's/.*@([0-9a-f]+).*/\1/' | sort -u)"
n="$(printf '%s\n' "$distinct" | grep -c .)"
if [ "$n" -eq 1 ]; then
  emit "GroupUsesSingleSHA" "pass" ""
  exit 0
else
  emit "GroupUsesSingleSHA" "fail" "distinct SHAs: $(printf '%s' "$distinct" | tr '\n' ' ')"
  exit 1
fi
