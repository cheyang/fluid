#!/usr/bin/env python3
"""Verify the fluid-client-python b06de4e8 .github/build-requirements.txt hashes against PyPI."""
import json, re, subprocess, sys, time, urllib.request

txt = subprocess.run(['git', '-C', '/home/ecs-user/.pr-debate/homes/codex/repos/fluid-cloudnative/fluid-client-python',
                      'show', 'b06de4e8:.github/build-requirements.txt'],
                     capture_output=True, text=True).stdout
entries = []
for line in txt.splitlines():
    m = re.match(r'^([A-Za-z0-9_.\-]+)==([A-Za-z0-9_.!\-]+)', line)
    if m:
        entries.append([m.group(1), m.group(2), []])
    h = re.search(r'--hash=sha256:([0-9a-f]{64})', line)
    if h and entries:
        entries[-1][2].append(h.group(1))

def fetch(url, tries=4):
    for i in range(tries):
        try:
            with urllib.request.urlopen(url, timeout=30) as r:
                return json.load(r)
        except Exception as e:
            if i == tries - 1:
                raise
            time.sleep(2 * (i + 1))

bad = 0
for name, ver, hashes in entries:
    d = fetch("https://pypi.org/pypi/%s/%s/json" % (name, ver))
    artifacts = {u["digests"]["sha256"] for u in d["urls"]}
    unknown = [h for h in hashes if h not in artifacts]
    print(("FAIL" if unknown else "ok  "), name, ver, len(hashes), "hashes")
    bad += bool(unknown)
print("packages with unknown hashes:", bad)
sys.exit(1 if bad else 0)
