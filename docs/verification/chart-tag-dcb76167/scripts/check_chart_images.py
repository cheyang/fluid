#!/usr/bin/env python3
"""Verify that every fluid image referenced by charts/fluid/fluid resolves on Docker Hub.

Layer: L1/L2 (chart render + registry API). The cheapest layer that decides the
PR's premise: a rendered image reference whose tag 404s on the registry is exactly
the precondition for ImagePullBackOff on a fresh `helm install`.

Usage:
    python3 check_chart_images.py --repo <git-repo> --ref <sha|ref> [--ref <more>...]

For each --ref the script materializes charts/fluid/fluid via `git archive`,
renders it with `helm template`, extracts every fluidcloudnative image whose tag
contains the version under test, and queries the Docker Hub v2 tags API.

Exit code: 0 iff at least one ref was checked AND every image tag at every ref
resolved (HTTP 200) on the registry.
"""
import argparse
import json
import re
import shutil
import subprocess
import sys
import tempfile
import urllib.request

CHART_DIR = "charts/fluid/fluid"
IMG_PREFIX = "fluidcloudnative/"
# images pinned to third-party versions (registrar, kubectl, init images, ...) are
# out of scope: only images tagged with the chart's own version anchor matter
TAG_RE = re.compile(r"^(?:-\s+)?image:\s*(\S+)\s*$")


def run(cmd, cwd=None):
    return subprocess.check_output(cmd, cwd=cwd).decode()


def materialize_chart(repo, ref, dest):
    subprocess.check_call(["git", "-C", repo, "archive", ref, CHART_DIR],
                          stdout=subprocess.DEVNULL)
    # `git archive <ref> <path>` writes a tar of <path> rooted at charts/...;
    # extract into dest then move the chart dir up for helm.
    tmp = tempfile.mkdtemp(dir=dest)
    tar = subprocess.Popen(["git", "-C", repo, "archive", ref, CHART_DIR],
                           stdout=subprocess.PIPE)
    subprocess.check_call(["tar", "-x", "-C", tmp], stdin=tar.stdout)
    tar.wait()
    shutil.move("%s/%s" % (tmp, CHART_DIR), "%s/chart" % dest)
    shutil.rmtree(tmp, ignore_errors=True)


def render_images(repo, ref):
    work = tempfile.mkdtemp(prefix="chart-verify-")
    try:
        materialize_chart(repo, ref, work)
        out = run(["helm", "template", "fluid", "chart"], cwd=work)
        images = []
        for line in out.splitlines():
            m = TAG_RE.match(line.strip())
            if m:
                images.append(m.group(1))
        return sorted(set(images))
    finally:
        shutil.rmtree(work, ignore_errors=True)


def registry_status(image):
    name, _, tag = image.partition(":")
    repo = name[len(IMG_PREFIX):] if name.startswith(IMG_PREFIX) else name
    url = "https://hub.docker.com/v2/repositories/%s%s/tags/%s" % (
        IMG_PREFIX, repo, tag)
    req = urllib.request.Request(url, method="GET")
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.status
    except urllib.error.HTTPError as e:
        return e.code
    except Exception as e:  # network error: record, never silently pass
        return "ERR:%s" % e


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", required=True)
    ap.add_argument("--ref", action="append", required=True)
    ap.add_argument("--tag-fragment", default="",
                    help="only check images whose tag contains this fragment")
    ap.add_argument("--out", default=None, help="write results JSON here")
    args = ap.parse_args()

    results = {}
    all_ok = True
    for ref in args.ref:
        sha = run(["git", "-C", args.repo, "rev-parse", ref]).strip()
        images = render_images(args.repo, sha)
        if args.tag_fragment:
            images = [i for i in images if args.tag_fragment in i]
        per_image = {}
        for img in images:
            if not img.startswith(IMG_PREFIX):
                continue
            status = registry_status(img)
            per_image[img] = status
            if status != 200:
                all_ok = False
        results[ref] = {"sha": sha, "images": per_image}
        print("== ref %s (sha %s) ==" % (ref, sha))
        for img, st in sorted(per_image.items()):
            mark = "OK " if st == 200 else "BAD"
            print("  [%s] %-60s registry=%s" % (mark, img, st))

    if args.out:
        with open(args.out, "w") as f:
            json.dump(results, f, indent=2, sort_keys=True)
        print("results written to %s" % args.out)

    print("VERDICT: %s" % ("PASS - every referenced fluid image tag exists on the registry"
                           if all_ok else
                           "FAIL - at least one referenced image tag is not on the registry"))
    return 0 if all_ok else 1


if __name__ == "__main__":
    sys.exit(main())
