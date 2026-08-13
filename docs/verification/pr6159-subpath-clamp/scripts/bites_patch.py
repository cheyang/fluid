#!/usr/bin/env python3
"""Apply the *proposed* fixes for F1 and F2 so the harness-bites check can confirm
the harness flips from red to green. Reverted immediately afterwards by the caller."""
import io
import sys

CFD = "pkg/application/inject/fuse/poststart/check_fuse_default.go"
HARNESS = "pkg/application/inject/fuse/poststart/zz_verify_pr6159_test.go"

s = io.open(CFD, encoding="utf-8").read()
h = io.open(HARNESS, encoding="utf-8").read()

# --- F1 proposed fix: shell-quote the interpolated arguments -------------------
old_f1 = '\tcmd := []string{"bash", "-c", fmt.Sprintf("time %s %s %s %s", g.scriptMountPath, mountPath, mountType, subPath)}'
new_f1 = (
    '\tshq := func(v string) string { return "\'" + strings.ReplaceAll(v, "\'", `\'\\\'\'`) + "\'" }\n'
    '\tcmd := []string{"bash", "-c", fmt.Sprintf("time %s %s %s %s", g.scriptMountPath, shq(mountPath), shq(mountType), shq(subPath))}'
)

# --- F2 proposed fix: quoted, glob-safe existence probe ------------------------
old_f2 = "while [ ! -e  $ConditionPathIsMountPoint/*/$SubPath ]"
new_f2 = 'while ! ls -d "$ConditionPathIsMountPoint"/*/"$SubPath" >/dev/null 2>&1'

# The harness guards on the original line shape; relax it for this run only.
guard = 'if !strings.Contains(rendered, "while [ ! -e  $ConditionPathIsMountPoint/*/$SubPath ]") {'

# Validate every anchor BEFORE writing anything, so a mismatch cannot leave the
# tree half-patched (which is exactly what happened on the first attempt).
for name, needle, hay in (
    ("F1", old_f1, s),
    ("F2", old_f2, s),
    ("harness guard", guard, h),
):
    if needle not in hay:
        sys.exit("%s anchor not found; nothing written" % name)

io.open(CFD, "w", encoding="utf-8").write(s.replace(old_f1, new_f1).replace(old_f2, new_f2))
io.open(HARNESS, "w", encoding="utf-8").write(h.replace(guard, "if false {"))

print("PATCHED-OK")
