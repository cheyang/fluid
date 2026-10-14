#!/usr/bin/env bash
set -u
ROOT=/home/ecs-user/.pr-debate/homes/codex/repos/fluid-cloudnative/fluid
V=$ROOT/docs/verification/bump-kind-action-1-15-1-codex
T=$(mktemp -d /tmp/bites-6208-XXXX)
mkdir -p "$T/repo/.github/workflows"
cp "$ROOT/.github/workflows/kind-e2e.yml" "$ROOT/.github/workflows/backward-compatibility-e2e.yml" "$T/repo/.github/workflows/"
echo "=== case 1: wrong sha (old v1.15.0 sha, comment claims v1.15.1) ==="
sed -i 's/1544676c07bb3570fcb70166beaa07c4528fd4aa # v1.15.1/06c1ae10762d3b9c1644e7fe69596ae519e015a2 # v1.15.1/' "$T/repo/.github/workflows/kind-e2e.yml"
bash "$V/scripts/10-verify-pin.sh" "$T/repo"
echo "(script exit=$?)"
echo
echo "=== case 2: drop kubectl_version, add cloud_provider ==="
python3 - "$T/repo/.github/workflows/backward-compatibility-e2e.yml" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
s = s.replace("          kubectl_version: ${{ matrix.kubernetes-version }}\n", "")
s = s.replace("          cluster_name: ${{ env.KIND_CLUSTER }}",
              "          cluster_name: ${{ env.KIND_CLUSTER }}\n          cloud_provider: \"true\"")
open(p, "w").write(s)
print("tampered", p)
PY
python3 "$V/scripts/20-verify-inputs.py" "$T/repo"
echo "(script exit=$?)"
echo "tamper workspace left at $T"
