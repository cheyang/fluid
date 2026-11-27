#!/usr/bin/env bash
# run_contracts.sh — emit "name<TAB>pass|fail" lines for every contract of the
# PR #6294 docs-broken-links verification harness.
#
# Usage: run_contracts.sh [repo-root]   (default: repo root derived from CWD)
#
# Contract polarity: every Fix* / NoNewBrokenLinks test is a CONTRACT test:
# it PASSES on code where the fix is present and FAILS on the base branch.
# HdfsLinksStillBroken is a bug-CANARY for finding F1: it passes while the
# known-remaining broken links are still broken and FLIPS to fail once fixed.
set -u
ROOT="${1:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
cd "$ROOT" || exit 2

pass() { printf '%s\tpass\n' "$1"; }
fail() { printf '%s\tfail\n' "$1"; }

# check_link <name> <src-md-relative> <link-substring>
# pass iff src exists AND at least one markdown link in src matches the
# substring AND every matching link's target resolves relative to src's dir.
# (Fails on base either because the matching link is broken, or because the
# fixed link form is absent entirely.)
check_link() {
  local name="$1" src="$2" want="$3"
  [ -f "$src" ] || { fail "$name"; return; }
  if python3 - "$src" "$want" <<'PY'
import os, re, sys
src, want = sys.argv[1], sys.argv[2]
LINK_RE = re.compile(r'\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)')
matched = 0
with open(src, encoding='utf-8', errors='replace') as f:
    for line in f:
        for m in LINK_RE.finditer(line):
            link = m.group(1)
            if want not in link:
                continue
            matched += 1
            target = link.split('#', 1)[0]
            if not os.path.exists(os.path.normpath(os.path.join(os.path.dirname(src), target))):
                sys.exit(1)  # a matching link is broken
sys.exit(0 if matched > 0 else 1)
PY
  then pass "$name"; else fail "$name"; fi
}

# --- The 14 links fixed by PR #6294 (contract tests) ---
check_link Fix01_EnCoreConcepts_TocEn docs/en/core-concepts/introduction.md  'en/TOC.md'
check_link Fix02_EnCoreConcepts_TocZh docs/en/core-concepts/introduction.md  'zh/TOC.md'
check_link Fix03_ZhCoreConcepts_TocEn docs/zh/core-concepts/introduction.md  'en/TOC.md'
check_link Fix04_ZhCoreConcepts_TocZh docs/zh/core-concepts/introduction.md  'zh/TOC.md'
check_link Fix05_EnUserguide_TocEn    docs/en/userguide/overview.md          'en/TOC.md'
check_link Fix06_EnUserguide_TocZh    docs/en/userguide/overview.md          'zh/TOC.md'
check_link Fix07_ZhUserguide_TocEn    docs/zh/userguide/overview.md          'en/TOC.md'
check_link Fix08_ZhUserguide_TocZh    docs/zh/userguide/overview.md          'zh/TOC.md'
# substring ends at 'preprocess-data.' so the base's broken '.y' variant also matches
check_link Fix09_EnVineyard_PreprocessPy docs/en/samples/vineyard/accelerate_kubeflow_pipelines.md 'samples/vineyard/preprocess-data/preprocess-data.'
check_link Fix10_EnVineyard_InstallMd    docs/en/samples/vineyard/accelerate_kubeflow_pipelines.md 'userguide/install.md'
check_link Fix11_ZhVineyard_PreprocessPy docs/zh/samples/vineyard/accelerate_kubeflow_pipelines.md 'samples/vineyard/preprocess-data/preprocess-data.'
check_link Fix12_ZhVineyard_InstallMd    docs/zh/samples/vineyard/accelerate_kubeflow_pipelines.md 'userguide/install.md'
check_link Fix13_ZhAppController_Juicefs docs/zh/samples/application_controller.md 'juicefs/juicefs_runtime.md'
check_link Fix14_ZhFuseRecover_Juicefs   docs/zh/samples/fuse_recover.md           'juicefs/juicefs_runtime.md'

# --- Global contract: no broken relative links other than the known set. ---
# Known-remaining (finding F1, disclosed by author): the two samples/hdfs links.
# Known scanner artifacts: 4 regex-cell misparses in generated api_doc.md.
scan="$(python3 docs/verification/docs-broken-links/scripts/check_doc_links.py "$ROOT" 2>/dev/null | grep -v '^---' | grep -v '^TOTAL' | grep -v '^BROKEN' || true)"
unexpected="$(printf '%s\n' "$scan" | grep -v 'samples/hdfs$' | grep -v '\\+\\|-$' | grep -v '^$' || true)"
if [ -z "$unexpected" ]; then pass NoNewBrokenLinks; else fail NoNewBrokenLinks; printf '%s\n' "$unexpected" >&2; fi

# --- Canary for F1: the two samples/hdfs links are still broken (disclosed). ---
hdfs_count="$(printf '%s\n' "$scan" | grep -c 'samples/hdfs$' || true)"
if [ "$hdfs_count" -eq 2 ]; then pass HdfsLinksStillBroken; else fail HdfsLinksStillBroken; fi
