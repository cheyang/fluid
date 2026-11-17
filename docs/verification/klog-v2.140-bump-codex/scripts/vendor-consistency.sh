#!/usr/bin/env bash
# vendor-consistency.sh — prove that the vendored klog in this tree is bit-identical
# to the official module fetched from the Go module proxy (guards against a
# corrupt or tampered vendor/ in a dependency-bump PR), that go.sum hashes
# verify, and that the whole module builds with -mod=vendor.
#
# Run from the repository root. Exit 0 iff all checks pass.
set -euo pipefail

MOD="k8s.io/klog/v2"
VDIR="vendor/$MOD"

echo "== module version per go.mod =="
ver="$(go list -mod=vendor -m "$MOD")"
echo "$ver"
echo "$ver" | grep -q 'v2.140.0' || { echo "FAIL: expected klog v2.140.0 in go.mod"; exit 1; }

echo "== go mod verify =="
go mod verify

echo "== fetching pristine module bits from proxy =="
moddir="$(GOFLAGS= go mod download -x -modcacherw "$MOD" 2>/dev/null >/dev/null; go env GOMODCACHE)/$(echo "$ver" | tr ' ' '@')"
[ -d "$moddir" ] || { echo "FAIL: module dir $moddir not found after download"; exit 1; }

echo "== comparing every vendored file against pristine module =="
rc=0
while IFS= read -r f; do
  rel="${f#"$VDIR"/}"
  if [ ! -f "$moddir/$rel" ]; then
    echo "MISSING-IN-MODULE: $rel"; rc=1; continue
  fi
  if ! cmp -s "$f" "$moddir/$rel"; then
    echo "DIFFERS: $rel"; rc=1
  fi
done < <(find "$VDIR" -type f | sort)
[ "$rc" -eq 0 ] && echo "all vendored klog files are bit-identical to $ver"

echo "== modules.txt entry =="
grep -A1 "# $MOD " vendor/modules.txt

echo "== full vendored build =="
go build -mod=vendor ./...
echo "vendor-consistency: OK"
