#!/bin/bash
# Evidence capture (not a gate): fetch the pinned upstream Alluxio sources
# this review's upstream-behavior findings rely on, into results/.
set -u
OUT="$(dirname "$0")/../results"
mkdir -p "$OUT"
fetch() { # name url
  if curl -sfL "$2" -o "$OUT/$1"; then echo "captured $1"; else echo "WARN: failed to fetch $1"; fi
}
fetch DecommissionWorkerCommand-v2.9.4.java "https://raw.githubusercontent.com/Alluxio/alluxio/v2.9.4/shell/src/main/java/alluxio/cli/fsadmin/command/DecommissionWorkerCommand.java"
fetch DefaultBlockMaster-v2.9.4.java "https://raw.githubusercontent.com/Alluxio/alluxio/v2.9.4/core/server/master/src/main/java/alluxio/master/block/DefaultBlockMaster.java"
fetch ReplicationChecker-v2.9.4.java "https://raw.githubusercontent.com/Alluxio/alluxio/v2.9.4/core/server/master/src/main/java/alluxio/master/file/replication/ReplicationChecker.java"
fetch TimeoutRetry-branch-2.9.java "https://raw.githubusercontent.com/Alluxio/alluxio/branch-2.9/core/common/src/main/java/alluxio/retry/TimeoutRetry.java"
fetch LocalFirstPolicy-branch-2.9.java "https://raw.githubusercontent.com/Alluxio/alluxio/branch-2.9/core/client/fs/src/main/java/alluxio/client/block/policy/LocalFirstPolicy.java"
fetch AdminCLI-v2.9.0.md "https://raw.githubusercontent.com/Alluxio/alluxio/v2.9.0/docs/en/operation/Admin-CLI.md"
fetch AdminCLI-v2.9.4.md "https://raw.githubusercontent.com/Alluxio/alluxio/v2.9.4/docs/en/operation/Admin-CLI.md"
