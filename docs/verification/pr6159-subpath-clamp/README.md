# Verification: PR #6159 — subpath escape handling

Reviewer-private verification harness for
[fluid-cloudnative/fluid#6159](https://github.com/fluid-cloudnative/fluid/pull/6159).

- base / merge-base: `05f06659`
- round 1 reviewed head: `70d75f12` ("sanitize and clamp subpaths")
- round 2 reviewed head: `076f0ba9` (via `6432fdfe`) — current
- **production code is untouched**; this branch adds test files and this directory only.

Runs on the Linux sandbox (`root@43.99.38.217`, go 1.24.1). Linux is required:
`NodePublishVolume` reads `/proc/mounts`, and two tests execute a real `/bin/bash`.

## The PR changed design between rounds

Round 1 **clamped** untrusted subpaths with a new `utils.CleanSubPath`. Round 2 **deletes**
that function and instead **rejects** non-local subpaths with `filepath.IsLocal` at four
boundaries — `GetPhysicalDatasetSubPath`, `checkDatasetMountSupport`, `getMountInfo`, and
`NodePublishVolume` — and adds `checkPathUnderMountRoot` plus `checkSymlinkFile`.

That is a better design and it resolves two round-1 findings outright (F6, F4). The harness was
updated accordingly; `pkg/utils/zz_verify_pr6159_test.go` was retired because its subject,
`CleanSubPath`, no longer exists.

---

## Premise (claim P0) — CONFIRMED

The PR body is an unfilled template (`fixes #XXXX`, all sections blank), so the claim was taken
from the title and commit message: *"prevent escaping mount roots"*. It reproduces on **base**:

```
base 05f06659:
  published symlink: .../alluxio-fuse/../../../../../hostonly
  content visible in the pod: "HOST-ONLY-CONTENT"     <-- escape
  --- FAIL

head 70d75f12 (clamp):   PASS — path clamped inside the root
head 076f0ba9 (reject):  PASS — InvalidArgument, request refused
```

**This PR fixes a real host-filesystem escape and should land.** Everything below is about gaps
in the fix, not an argument against it.

Reachability (`F7`): that the value is tenant-controllable was established by reading code, not
by a live cluster run. `GetPhysicalDatasetSubPath` is the only producer of the PV attribute,
`Dataset.spec.mounts[].mountPoint` carries only `MinLength=5`, and there is no validating webhook
on Dataset. Stated at that confidence deliberately.

### How the escape is observed without a cluster

`NodePublishVolume` has a symlink publish method, so the mount source the plugin computed is
readable with `os.Readlink` — no FUSE mount, no privileged bind mount:

- `common.NodePublishMethod = symlink` → `utils.CreateSymlink(targetPath, mountPath)`
- `AnnotationSkipCheckMountReadyTarget = MountPod` → routes past `check_mount.sh` (present only
  in the CSI image) into `checkMountPathExists`

Assertions read real file content *through* the published path, so a red result demonstrates
exposure rather than inferring it.

---

## Findings at head `076f0ba9`

| id | claim | verdict | introduced by PR? | severity |
|----|-------|---------|-------------------|----------|
| P0 | untrusted subpath escapes the FUSE root | base FAIL → head PASS | — | premise **confirmed** |
| N1 | the new symlink guard covers intermediate path components | **STILL BROKEN** | yes (new guard) | blocker |
| F3 | an already-persisted non-local subpath keeps working | **STILL BROKEN** (broader) | yes | blocker |
| F1 | subpath cannot inject shell commands via the postStart hook | **STILL BROKEN** | no | major |
| F2 | the postStart subpath gate reports a missing subpath as missing | **STILL BROKEN** | no | major |
| F5 | `NodePublishVolume` has coverage for the new behavior | mostly FIXED | — | minor |
| F6 | reject-vs-clamp policy is consistent across call sites | **FIXED** in round 2 | — | resolved |
| F4 | `CleanSubPath`'s doc-comment guarantee holds | **MOOT** — function deleted | — | resolved |
| N2 | `MOUNT_ROOT` is a new hard dependency of NodePublishVolume | informational | yes | minor |
| N3 | duplicated/incorrect error message | by inspection | yes | nit |
| N4 | PR title and commit messages no longer describe the change | by inspection | yes | nit |

### N1 — the new symlink guard only checks the final component (blocker)

`checkSymlinkFile` (`nodeserver.go:177`, helper at `:433`) does `os.Lstat(mountPath)`, which
inspects only the **last** component. When the subpath has more than one component and an
**earlier** component is a symlink, `Lstat` follows it, stats the real target, reports "not a
symlink", and the mount proceeds against a path outside the FUSE root:

```
subPath "evil"       (1 component)  -> REJECTED
    "reject mounting path /tmp/fl61594105652368/alluxio-fuse/evil because it is a symlink"

subPath "evil/inner" (2 components) -> ESCAPES
    published symlink: /tmp/fl6159416984240/alluxio-fuse/evil/inner
    resolves to      : /tmp/fl6159579890907/inner
    content visible in the pod: "HOST-ONLY-CONTENT"
```

Both pass `filepath.IsLocal`, so the earlier gate does not help. The new test added in this PR
("should reject a mount path that is a symlink") exercises only the single-component case that
works, which is why the gap survived.

Fix: resolve the whole path beneath the root and check containment — `filepath.EvalSymlinks` on
`mountPath` followed by an `IsSubPath(fluidPath, resolved)` check, or reject if any component is
a symlink. Note both are still check-then-use; `openat2` with `RESOLVE_BENEATH` is the only
race-free option, though the TOCTOU window matters much less than the current gap.

### F3 — upgrade regression, now broader (blocker)

A non-local `fluid_sub_path` is reachable on base: `GetPhysicalDatasetSubPath` uses
`strings.SplitAfterN(datasetPath, "/", 3)`, so `dataset://ns/ds//sub-c` (double slash) yields
`"/sub-c"`, written verbatim into the PV attribute. On base the extra slash collapses in POSIX
and the volume mounts:

```
base    : PASS  — published .../alluxio-fuse//sub-c
head 70d75f12: FAIL — InvalidArgument (filepath.IsAbs)
head 076f0ba9: FAIL — InvalidArgument (filepath.IsLocal)
    "fluid_sub_path must be a relative path that does not escape the mount point, but got \"/sub-c\""
```

Round 2 widens the blast radius: the same value is now rejected at `getMountInfo`
(`runtime_helper.go:112`) for the sidecar path and at `GetPhysicalDatasetSubPath`
(`dataset.go:77`) via `checkDatasetMountSupport`, so an existing **Dataset** also stops
reconciling — not just the CSI mount. `referencedataset/volume.go` writes the PV attribute only
at creation (`if !found`), so upgrading never rewrites persisted values.

Suggested handling: normalize legacy values rather than refusing them (`filepath.Clean` a leading
slash away before validating), or gate the strict rejection to newly created objects and log a
warning for existing ones.

### F1 — the shell sink remains uncovered (major, pre-existing)

`mutator_default.go:162` no longer sanitizes, and `:337` still hands
`FuseMountInfo.SubPath` to `GetPostStartCommand`, which builds a `bash -c` string with
`fmt.Sprintf` (`check_fuse_default.go:132`). `filepath.IsLocal` rejects traversal but accepts
every shell metacharacter:

```
raw subPath        : "x; touch /tmp/.../pr6159-injected"
passes IsLocal gate: true  (reaches the sink unchanged)
postStart argv     : []string{"bash", "-c",
                      "time /check-mount.sh /runtime-mnt/... alluxio x; touch /tmp/.../pr6159-injected"}
bash output        : bash: /check-mount.sh: No such file or directory
--- FAIL: INJECTION: the postStart hook executed an attacker-supplied command
```

The primary command fails and the injected one runs anyway. The hook runs in the injected FUSE
sidecar, privileged under the default mutator, and is reached from both `defaultPrepareMutation`
and `unprivilegedPrepareMutation` via the shared `prepareFuseContainerPostStartScript`.

### F2 — the postStart subpath gate is bypassable (major, pre-existing)

`check_fuse_default.go:90`, unquoted and containing a literal glob:

```bash
while [ ! -e  $ConditionPathIsMountPoint/*/$SubPath ]
```

A subpath with a space makes `[` receive too many operands and return 2, so the `while` condition
is false, the loop never runs, and the script exits 0 — reporting a subpath as present without
checking it:

```
control ("pr6159-definitely-missing") : exit=2  "timed out checking sub path"    <-- gate works
crafted ("pr6159 definitely missing") : exit=0  "[: too many arguments"
                                                "succeed in checking mount point" <-- bypassed
```

The control passing is the bites proof. `csi/shell/check_mount.sh:48`, the CSI-side copy of the
same check, was already hardened to `test -e "$Cond/$SubPath"` with `grep -F` and argv passing —
the two copies have diverged and only the unhardened one is in this PR's blast radius.

### F5 — coverage, mostly addressed (minor)

Round 2 adds three specs with real assertions (`Expect(status.Code(err)).To(Equal(codes.InvalidArgument))`),
which resolves the substance of the original point. Remaining gaps: the pre-existing
"should append subpath to fluid path" spec still discards both results (`_ = resp; _ = err`),
nothing covers the F3 backward-compatibility case, and the symlink spec covers only the
single-component case (see N1).

### N2 — `MOUNT_ROOT` is a new hard dependency (minor)

`NodePublishVolume` did not consult `MOUNT_ROOT` before `6432fdfe`. `checkPathUnderMountRoot`
now calls `GetMountRoot`, which **errors on an empty value**
(`validation.IsValidMountRoot`), turning a missing env var into `InvalidArgument` for every
volume. The shipped Helm chart defaults `runtime.mountRoot` to `/runtime-mnt`, so a default
install is fine. `config/fluid/bases/csi/daemonset.yaml` does not set it, but no Makefile target
deploys from there, so this is a robustness note rather than a break. Failing closed is the right
call for a security check; it is worth a release note.

Related: `IsValidMountRoot` enforces relaxed DNS-1123 **per path component, 63 chars max**, so an
operator with a long or unusual `MOUNT_ROOT` now fails all mounts where previously only
`GetMountRoot`'s other callers cared.

### N3 — duplicated error message (nit)

`dataset.go:56` and `:61` return the identical string `"should only have one mount"`, but `:61`
guards a different condition (the single mount is not a `dataset://` reference). The second
message should say so.

### N4 — stale title and misleading commit message (nit)

The PR is titled `optim(utils): sanitize and clamp subpaths to handle path escaping`, but at
`076f0ba9` nothing is clamped, and `pkg/utils` is net **-36 lines** — `CleanSubPath` and its
tests were removed. Also, commit `076f0ba9` is titled "fix comment" while it changes
`GetPhysicalDatasetSubPath`'s signature, adds validation, and updates four call sites. Both make
the history harder to follow later.

---

## How to run

```bash
# all findings except F5/N3/N4 (inspection-only); ~90s, F2 spends 30s in the script poll loop
go test ./pkg/csi/plugins/ ./pkg/application/inject/fuse/poststart/ \
  -run 'TestVerifyPR6159' -count=1 -timeout 400s -v

# premise, against BASE (P0 + F3 only)
git worktree add -f /tmp/base6159 05f06659
cp pkg/csi/plugins/zz_verify_pr6159_test.go /tmp/base6159/pkg/csi/plugins/
cd /tmp/base6159 && go test ./pkg/csi/plugins/ -run 'TestVerifyPR6159' -count=1 -v
```

Next round: `bash docs/verification/pr6159-subpath-clamp/scripts/re-verify.sh --layers unit`

`--layers unit` is required — the manifest has no `integration` layer and the default
`unit,integration` would emit a spurious `SKIPPED` and exit 1.

## Continuing after the fix

All tests are **contract** tests; there are no canaries, so nothing needs inverting.

| test | goes green when |
|---|---|
| `..._P0_...` | already green; guards against regressing containment |
| `..._N1_...` | the symlink check covers intermediate components |
| `..._N1guard_...` | must stay green — it is the bites guard for N1 |
| `..._F3_...` | a legacy non-local persisted subpath is normalized rather than refused |
| `..._F1_...` | the subpath reaches the script as an argv element, or is shell-quoted |
| `..._F2_.../crafted...` | the gate's `[ ! -e ]` is quoted and glob-safe |
| `..._F2_.../control...` | must stay green — bites guard for F2 |
| `..._N2_...` | informational; green either way |

Harness-bites was run in round 1: with `GetPostStartCommand` shell-quoting its arguments and the
gate rewritten as `while ! ls -d "$Cond"/*/"$SubPath" >/dev/null 2>&1`, F1 and F2 both flip to
PASS and reverting restores an empty production diff — see `results/BITES_poststart_fixed.txt`
and `scripts/bites_patch.py`. N1 carries its own in-suite bites guard (`N1guard`), and P0/F3 bite
bidirectionally (base vs head), which is stronger than a synthetic fix.

### Kickoff prompt for a fresh agent

> Continue the review pipeline for https://github.com/fluid-cloudnative/fluid/pull/6159.
> The verification branch is `verify/pr6159-subpath-clamp` on the `cheyang/fluid` fork; state
> lives in `docs/verification/pr6159-subpath-clamp/`. Run
> `scripts/re-verify.sh --layers unit` on the Linux sandbox (needs `/proc/mounts` and a real
> bash), then review the `last-reviewed..head` delta. Note this PR has already been reworked
> once mid-review (clamp -> reject), so re-read the design before trusting the finding list.
