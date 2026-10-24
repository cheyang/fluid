#!/bin/bash
# F1 contract check: the default Alluxio runtime image shipped by the fluid
# helm chart must contain `alluxio fsadmin decommissionWorker`, which first
# shipped in Alluxio v2.9.4 (see docs/en/operation/Admin-CLI.md at tags
# v2.9.0..v2.9.3: command absent; v2.9.4: present; alluxio/alluxio-dev:2.9.0
# on Docker Hub was last pushed 2023-01-11, before the decommission commits
# landed 2023-03-30).
#
# Contract (fails while the finding is unfixed): the default
# runtime.alluxio.runtime.imageTag in charts/fluid/fluid/values.yaml must be
# a version >= 2.9.4, otherwise the GracefulWorkerScaleDown feature can never
# succeed on a default deployment: every decommission attempt fails with an
# unrecognized-command error and the scale-down waits the full
# defaultWorkerDecommissionDeadline (10m) before an ungraceful forced
# proceed.
set -u
VALUES="charts/fluid/fluid/values.yaml"
tag=$(awk '/^  alluxio:/{f=1} f && /runtime:/{r=1} r && /imageTag:/{print $2; exit}' "$VALUES")
echo "default alluxio runtime imageTag: ${tag:-<none>}"
if [ -z "${tag:-}" ]; then
  echo "FAIL: could not parse runtime.alluxio.runtime.imageTag from $VALUES"
  exit 2
fi
# numeric compare on X.Y.Z
ver_ge() { [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -1)" = "$2" ]; }
if ver_ge "$tag" "2.9.4"; then
  echo "PASS: default image $tag >= 2.9.4 (decommissionWorker available)"
  exit 0
else
  echo "FAIL: default image $tag < 2.9.4: 'alluxio fsadmin decommissionWorker' does not exist in this image"
  exit 1
fi
