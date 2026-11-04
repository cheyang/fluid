#!/usr/bin/env bash
# C6 (contract): pin the Go 1.26 `urlstrictcolons` GODEBUG delta and bound its
# blast radius in this repo.
#  (a) with the repo's current `go` directive, url.Parse must REJECT a host with
#      extra colons (new behavior the PR discloses);
#  (b) the repo's url.Parse call sites must stay confined to
#      pkg/ddc/juicefs/data_migrate.go (jfs:// names cannot contain colons, and
#      user-supplied ExternalStorage URIs propagate the parse error). If a
#      future revision adds call sites, their colon exposure must be re-reviewed.
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"
TC="$(awk '/^toolchain /{print $2; exit}' go.mod)"
GODIR="$(awk '/^go /{print $2; exit}' go.mod)"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/t" && cd "$TMP/t"
cat > main.go <<'GO'
package main

import (
	"fmt"
	"net/url"
	"os"
)

func main() {
	_, err := url.Parse("http://localhost:80:80/")
	fmt.Printf("err=%v\n", err)
	if err == nil {
		os.Exit(1) // multi-colon host accepted: urlstrictcolons not in effect
	}
}
GO
echo "module urlstrict" > go.mod
echo "go $GODIR" >> go.mod
GOTOOLCHAIN="$TC" go run .
echo "PASS(a): url.Parse rejects multi-colon host under go $GODIR"
SITES="$(cd "$ROOT" && grep -rl 'url\.Parse(' pkg/ cmd/ api/ --include='*.go' | grep -v _test.go | sort)"
if [ "$SITES" != "pkg/ddc/juicefs/data_migrate.go" ]; then
  echo "FAIL(b): url.Parse call sites changed:"; echo "$SITES"; exit 1
fi
echo "PASS(b): url.Parse call sites confined to pkg/ddc/juicefs/data_migrate.go"
