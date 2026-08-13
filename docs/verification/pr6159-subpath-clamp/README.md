# Verification: PR #6159 — sanitize and clamp subpaths

Reviewer-private verification harness for
[fluid-cloudnative/fluid#6159](https://github.com/fluid-cloudnative/fluid/pull/6159)
("optim(utils): sanitize and clamp subpaths to handle path escaping").

- reviewed head: `70d75f12e3fa4dd770ecc37f573db916d0792f85`
- base / merge-base: `05f06659bd19e72ff2a0c4ca0f4de2a5eb267baa`
- **production code is untouched** — this branch adds test files and this directory only.

Run on the Linux sandbox (`root@43.99.38.217`, go 1.24.1). The harness needs Linux:
`NodePublishVolume` reads `/proc/mounts`, and two tests execute a real `/bin/bash`.

---

## Premise (claim P0) — CONFIRMED

The PR body is an unfilled template (`fixes #XXXX`, all sections blank), so there is no linked
issue and no stated problem. The claim was taken from the title and commit message: *"sanitize
and clamp subpaths to prevent escaping mount roots"*.

That premise reproduces. On the **base** branch an untrusted `fluid_sub_path` walks straight out
of the FUSE mount root, and the pod reads host content:

```
base 05f06659:
  published symlink: .../my-dataset/alluxio-fuse/../../../../../hostonly
  resolves to      : /tmp/.../001/hostonly
  content visible in the pod: "HOST-ONLY-CONTENT"     <-- escape
  --- FAIL: TestVerifyPR6159_P0_SubPathTraversalIsContainedInFuseRoot

PR head 70d75f12:
  published symlink: .../my-dataset/alluxio-fuse/hostonly
  content visible in the pod: "DATASET-CONTENT"       <-- contained
  --- PASS: TestVerifyPR6159_P0_SubPathTraversalIsContainedInFuseRoot
```

**So this PR fixes a real host-filesystem escape and should land.** Everything below is about
gaps and one regression in the fix, not an argument against it.

Reachability note (`F7`): that the value is tenant-controllable was established by reading code,
not by a live cluster run. `GetPhysicalDatasetSubPath` is the only producer of the PV attribute
and applies no character or traversal validation; `Dataset.spec.mounts[].mountPoint` carries only
`MinLength=5` (`api/v1alpha1/dataset_types.go:82-84`); there is no validating webhook on Dataset.
Stated at that confidence on purpose.

### How the escape is observed without a cluster

`NodePublishVolume` has a symlink publish method, so the mount source the CSI plugin computed is
directly readable with `os.Readlink` — no FUSE mount and no privileged bind mount needed:

- `common.NodePublishMethod = symlink` → the code calls `utils.CreateSymlink(targetPath, mountPath)`
- `AnnotationSkipCheckMountReadyTarget = MountPod` → routes past `check_mount.sh`, which only
  exists inside the CSI image, into `checkMountPathExists` instead

The assertions then read real file content *through* the published path, so a red result is a
demonstration of exposure rather than an inference from a string.

---

## Findings

| id | claim | polarity | verdict on PR head | introduced by PR? | severity |
|----|-------|----------|--------------------|-------------------|----------|
| P0 | untrusted subpath escapes the FUSE root | contract | base FAIL → head PASS | — | premise **confirmed** |
| F3 | an already-persisted **absolute** subpath keeps working | contract | **FAIL** | **yes** | blocker |
| F1 | subpath cannot inject shell commands via the postStart hook | contract | **FAIL** | no | major |
| F2 | the postStart subpath gate reports a missing subpath as missing | contract | **FAIL** | no | major |
| F5 | `NodePublishVolume` has coverage for the new behavior | mutation | **not covered** | yes | major |
| F4 | `CleanSubPath`'s doc-comment containment guarantee holds | contract | **FAIL** | yes | minor |
| F6 | reject-vs-clamp policy is consistent across call sites | design | n/a | yes | minor |

### F3 — upgrade regression (blocker, introduced by this PR)

`nodeserver.go:139-141` now rejects an absolute `fluid_sub_path` outright. That value is
reachable on base: `GetPhysicalDatasetSubPath` uses `strings.SplitAfterN(datasetPath, "/", 3)`, so
`dataset://ns/ds//sub-c` (double slash) yields `"/sub-c"`, which
`referencedataset/volume.go:95` writes verbatim into the PV attribute. On base the extra slash
collapses in POSIX and the volume mounts; after this PR the same PV fails:

```
base    : PASS  — published symlink .../alluxio-fuse//sub-c
PR head : FAIL  — rpc error: code = InvalidArgument
                  desc = fluid_sub_path must be a relative path, but got "/sub-c"
```

`referencedataset/volume.go` sets the attribute only at PV creation (`if !found`), so upgrading
the CSI plugin does not rewrite already-persisted values. Existing workloads fail on the next pod
restart or reschedule.

Suggested fix: clamp instead of reject, which is what `CleanSubPath` already does for an absolute
input at the other three call sites (`CleanSubPath("/sub/path") == "sub/path"`), and is what makes
the four call sites agree.

### F1 — the shell sink the PR does not cover (major, pre-existing)

The PR sanitizes four paths but not `mutator_default.go:337`, which hands the same subpath to
`GetPostStartCommand`. That builds a `bash -c` string with `fmt.Sprintf`
(`check_fuse_default.go:132`). `CleanSubPath` collapses `..` but preserves every shell
metacharacter, so it does not make the value safe for this sink:

```
raw subPath       : "x; touch /tmp/.../pr6159-injected"
after CleanSubPath: "x; touch /tmp/.../pr6159-injected"     <-- unchanged
postStart argv    : []string{"bash", "-c",
                     "time /check-mount.sh /runtime-mnt/... alluxio x; touch /tmp/.../pr6159-injected"}
bash output       : bash: /check-mount.sh: No such file or directory
--- FAIL: INJECTION: the postStart hook executed an attacker-supplied command
```

The primary command fails and the injected one runs anyway. The hook executes in the injected
FUSE sidecar, which is privileged under the default mutator. Reached from **both** mutators —
`defaultPrepareMutation` and `unprivilegedPrepareMutation` share
`prepareFuseContainerPostStartScript`.

### F2 — the postStart subpath gate is bypassable (major, pre-existing)

`check_fuse_default.go:90`:

```bash
while [ ! -e  $ConditionPathIsMountPoint/*/$SubPath ]
```

Unquoted, and containing a literal glob. A subpath with a space makes `[` receive too many
operands and return 2, so the `while` condition is false, the loop never runs, and the script
exits 0 — reporting a subpath as present without ever checking it:

```
control ("pr6159-definitely-missing")     : exit=2  "timed out checking sub path"   <-- gate works
crafted ("pr6159 definitely missing")     : exit=0  "[: too many arguments"
                                                    "succeed in checking mount point"  <-- bypassed
```

The control case passing is the harness-bites proof: the gate works normally and is defeated only
by the crafted value.

Worth noting: `csi/shell/check_mount.sh:48`, the CSI-side copy of this same check, was already
hardened to `test -e "$ConditionPathIsMountPoint/$SubPath"` with `grep -F` and argv passing. The
two copies of the check have diverged, and only the unhardened one is in this PR's blast radius.

### F4 — the doc comment overclaims (minor, introduced by this PR)

`CleanSubPath`'s comment states the result *"can never escape the directory it is later joined
to"*. `filepath.Join`/`Clean` are lexical and documented not to consider symlinks, so:

```
subPath "link" -> CleanSubPath -> "link"
joined     : <fuse root>/link
resolves to: /tmp/.../001/hostonly                       <-- outside
content reachable via the sub path: "HOST-ONLY-CONTENT"
```

The code is a reasonable lexical clamp; the stated guarantee is what is wrong, and a future
maintainer will rely on it when adding a caller. Reword to say it collapses lexical `..`
traversal and does not resolve symlinks. Symlink-aware resolution at the mount sinks
(`openat2(RESOLVE_BENEATH)`-style, or rejecting symlink components) is a larger change worth its
own issue.

### F5 — no `NodePublishVolume` coverage (major)

`nodeserver_test.go:278-294` calls `NodePublishVolume` and discards both results:

```go
resp, err := ns.NodePublishVolume(context.Background(), req)
_ = resp
_ = err
```

So it passes whether or not absolute paths are rejected and whether or not `../` is clamped. This
is a mutation-layer finding: `re-verify.sh` reports it `SKIPPED` by design. The supporting
evidence is that F3 — a real regression at exactly that call site — ships with all 24 CI checks
green, and codecov reports 50% patch coverage on `nodeserver.go` (2 missing, 1 partial).

### F6 — reject vs. silently clamp (minor, design)

An absolute subpath is *rejected* at the CSI layer but *silently clamped* at the other three call
sites; traversal is silently clamped everywhere. So the same input gets two different treatments
depending on which path it takes, and `dataset://ns/ds/../../../etc` becomes `etc` with no error
and no log — the user mounts a different directory than they asked for and is never told. A single
validation point (a Dataset webhook, or the `dataset://` parse) that rejects and reports would
make F1, F2 and F4 unreachable rather than individually patched.

---

## How to run

```bash
# unit layer (all findings except F5); ~90s, F2 spends 2x30s in the script's poll loop
go test ./pkg/csi/plugins/ ./pkg/utils/ ./pkg/application/inject/fuse/poststart/ \
  -run 'TestVerifyPR6159' -count=1 -timeout 300s -v

# premise, against BASE (P0 + F3 only; CleanSubPath does not exist on base)
git worktree add -f /tmp/base6159 05f06659bd19e72ff2a0c4ca0f4de2a5eb267baa
cp pkg/csi/plugins/zz_verify_pr6159_test.go /tmp/base6159/pkg/csi/plugins/
cd /tmp/base6159 && go test ./pkg/csi/plugins/ -run 'TestVerifyPR6159' -count=1 -v
```

Next round:

```bash
bash docs/verification/pr6159-subpath-clamp/scripts/re-verify.sh --layers unit
```

`--layers unit` is required: the manifest has no `integration` layer, and the default
`unit,integration` would emit a spurious `SKIPPED` and exit 1.

## Continuing after the fix

Every test here is a **contract** test — there are no canaries, so nothing needs inverting.
After a fix, all of these should go green:

| test | goes green when |
|---|---|
| `..._P0_...` | already green on PR head; guards against regressing the clamp |
| `..._F3_...` | an absolute persisted subpath is clamped rather than rejected |
| `..._F1_...` | the subpath reaches the script as an argv element, or is shell-quoted |
| `..._F2_.../crafted...` | the gate's `[ ! -e ]` is quoted and glob-safe |
| `..._F2_.../control...` | must stay green — it is the bites guard |
| `..._F4_...` | the doc comment is corrected, or resolution becomes symlink-aware |

The harness-bites check has been run: with `GetPostStartCommand` shell-quoting its arguments and
the gate rewritten as `while ! ls -d "$Cond"/*/"$SubPath" >/dev/null 2>&1`, F1 and F2 both flip to
PASS, and reverting restores an empty production diff. See `results/BITES_poststart_fixed.txt`.

### Kickoff prompt for a fresh agent

> Continue the review pipeline for https://github.com/fluid-cloudnative/fluid/pull/6159.
> The verification branch is `verify/pr6159-subpath-clamp` on the `cheyang/fluid` fork; state
> lives in `docs/verification/pr6159-subpath-clamp/` (manifest, `.last-reviewed`, results).
> Run `scripts/re-verify.sh --layers unit` on the Linux sandbox (the harness needs `/proc/mounts`
> and a real bash), then review the `last-reviewed..head` delta.
