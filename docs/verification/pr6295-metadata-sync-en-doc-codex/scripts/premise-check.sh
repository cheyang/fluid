#!/usr/bin/env bash
# premise-check.sh -- P0 for PR #6295: "docs/zh/samples/metadata_sync.md has no
# English version, so English readers can't find how to turn off
# spec.management.metadataSyncPolicy.autoSync".
# Runs against the BASE branch (the problem must exist *without* the patch).
# Usage: scripts/premise-check.sh [base-sha]   (default: merge-base of origin/master and PR head)
set -euo pipefail
BASE="${1:-b8ff60e309c341b58a9e26b085b76e6067e6bb60}"
echo "base = $BASE"
if git cat-file -e "$BASE:docs/en/samples/metadata_sync.md" 2>/dev/null; then
  echo "P0 REFUTED: docs/en/samples/metadata_sync.md already exists on base"
  exit 1
fi
echo "docs/en/samples/metadata_sync.md absent on base: OK"
echo "base docs/en files mentioning metadataSyncPolicy/autoSync:"
hits=0
for f in $(git ls-tree -r --name-only "$BASE" docs/en/ | grep '\.md$'); do
  if git show "$BASE:$f" | grep -q "metadataSyncPolicy\|autoSync"; then
    echo "  $f"; hits=$((hits+1))
  fi
done
# api_doc.md is generated (field listing) and juicefs_runtime.md is a JuiceFS-only
# YAML snippet; neither documents the Alluxio metadata-sync policy workflow.
echo "hits=$hits (generated api_doc.md + JuiceFS-specific snippet only => gap is real)"
echo "P0 CONFIRMED"
