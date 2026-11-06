#!/usr/bin/env bash
# F2 (CANARY — polarity matters): asserts the CURRENT observed placement of the
# new comment block in pkg/ddc/alluxio/api_gateway_test.go.
#
# The PR adds the comment above `func TestGetAPIGatewayStatus` but leaves a
# blank line between the comment block and the func, so Go tooling does not
# treat it as the function's doc comment (compare the sibling
# TestQueryAPIGatewayEndpoint, whose comment is directly adjacent).
#
# While the blank line is present this canary PASSES (documents the defect).
# If the author removes the blank line (the fix), it FLIPS TO FAIL and must be
# inverted into a contract test before the next round.
set -u
f="pkg/ddc/alluxio/api_gateway_test.go"
[ -f "$f" ] || { echo "HARNESS-UPDATE: $f not found at this ref"; exit 3; }

# Line number of the first comment line of the block and of the func decl.
comment_line=$(grep -n '^// TestGetAPIGatewayStatus tests whether' "$f" | head -1 | cut -d: -f1)
func_line=$(grep -n '^func TestGetAPIGatewayStatus(' "$f" | head -1 | cut -d: -f1)
last_comment_line=$(grep -n '^// The test cases cover both' "$f" | head -1 | cut -d: -f1)

if [ -z "$comment_line" ] || [ -z "$func_line" ] || [ -z "$last_comment_line" ]; then
  echo "HARNESS-UPDATE: comment block or func moved/renamed at this ref"
  exit 3
fi

gap=$(( func_line - last_comment_line - 1 ))
if [ "$gap" -ge 1 ]; then
  echo "PASS (canary: defect still present): $gap blank line(s) between comment (ends line $last_comment_line) and func (line $func_line) — comment is NOT attached as a doc comment"
  exit 0
else
  echo "FAIL (canary flipped: comment now attached directly above func) — invert this into a contract test"
  exit 1
fi
