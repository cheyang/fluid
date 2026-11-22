#!/usr/bin/env bash
# docs_antecedent_check.sh - contract check for F-docs-antecedent-ambiguous
# (PR #6245 round 6 finding, fixed by the round-7 docs delta e7a93e775).
#
# The sample-docs closing paragraph declares spec.master.imagePullSecrets and
# spec.jobWorker.imagePullSecrets inert, then in the next sentence used the
# bare demonstrative "Changing these fields on an existing runtime ..." (en) /
# "修改已有 runtime 的这些字段 ..." (zh). The nearest antecedent of "these
# fields" is the inert master/jobWorker pair, so the literal reading
# contradicts the disclosure (the sync path derives worker/fuse desired
# secrets from spec.imagePullSecrets + worker/fuse overrides + the env default
# only; master/jobWorker secrets are never consumed). The fix disambiguates
# the referent: "Changing the imagePullSecrets fields above ..." /
# "修改已有 runtime 的上述 imagePullSecrets 字段 ...".
#
# Contract (both languages): the bare-demonstrative form must be ABSENT and a
# disambiguated referent must be PRESENT in the closing paragraph.
# Exits 1 while the ambiguous phrasing is present in either language sample.
# Usage: docs_antecedent_check.sh <repo-root>
set -euo pipefail
ROOT="${1:-$(git rev-parse --show-toplevel)}"
EN="$ROOT/docs/en/samples/image_pull_secrets.md"
ZH="$ROOT/docs/zh/samples/image_pull_secrets.md"
rc=0

if grep -q "Changing these fields on an existing runtime" "$EN"; then
  echo "AMBIGUOUS: docs/en/samples/image_pull_secrets.md: 'Changing these fields' reads as referring to the inert master/jobWorker fields declared one sentence earlier"
  rc=1
fi
if ! grep -q "Changing the imagePullSecrets fields above on an existing runtime" "$EN"; then
  echo "MISSING: docs/en/samples/image_pull_secrets.md: expected the disambiguated referent 'Changing the imagePullSecrets fields above on an existing runtime'"
  rc=1
fi
if grep -q "修改已有 runtime 的这些字段" "$ZH"; then
  echo "AMBIGUOUS: docs/zh/samples/image_pull_secrets.md: '这些字段' reads as referring to the inert master/jobWorker fields declared one sentence earlier"
  rc=1
fi
if ! grep -q "修改已有 runtime 的上述 imagePullSecrets 字段" "$ZH"; then
  echo "MISSING: docs/zh/samples/image_pull_secrets.md: expected the disambiguated referent '修改已有 runtime 的上述 imagePullSecrets 字段'"
  rc=1
fi
if [ "$rc" -eq 0 ]; then
  echo "OK: closing-paragraph referent is disambiguated in both language samples"
fi
exit "$rc"
