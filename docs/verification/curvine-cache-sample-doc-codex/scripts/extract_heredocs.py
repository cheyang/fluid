#!/usr/bin/env python3
"""Extract `cat<<...` heredoc blocks from a Fluid sample markdown doc into
runnable shell scripts, exactly as a user would copy-paste them.

Usage: extract_heredocs.py <doc.md> <outdir>

Writes <outdir>/<NN>_<targetfile>.sh for each block (preserving the original
quoting form of the heredoc opener) and <outdir>/blocks.json describing them.
"""
import json
import re
import sys
import os

# Matches the doc's prompt-prefixed heredoc openers, e.g.
#   $ cat<<EOF >minio.yaml
#   $ cat<<'EOF' >cacheruntimeclass.yaml
OPENER = re.compile(r"^\s*\$\s*cat\s*<<\s*(['\"]?)([A-Za-z_][A-Za-z0-9_]*)\1\s*>\s*(\S+)\s*$")


def main():
    doc_path, outdir = sys.argv[1], sys.argv[2]
    os.makedirs(outdir, exist_ok=True)
    with open(doc_path, encoding="utf-8") as f:
        lines = f.read().splitlines()

    blocks = []
    i = 0
    while i < len(lines):
        m = OPENER.match(lines[i])
        if not m:
            i += 1
            continue
        quote, delim, target = m.group(1), m.group(2), m.group(3)
        body = []
        j = i + 1
        while j < len(lines) and lines[j].strip() != delim:
            body.append(lines[j])
            j += 1
        if j >= len(lines):
            print("unterminated heredoc for %s" % target, file=sys.stderr)
            sys.exit(2)
        # Reproduce the exact command a reader would run: same quoting form.
        opener = "cat <<%s%s%s >%s" % (quote, delim, quote, target)
        idx = len(blocks)
        script_name = "%02d_%s.sh" % (idx, target)
        script_path = os.path.join(outdir, script_name)
        with open(script_path, "w", encoding="utf-8") as sf:
            sf.write(opener + "\n")
            sf.write("\n".join(body) + "\n")
            sf.write(delim + "\n")
        blocks.append({
            "index": idx,
            "target": target,
            "delimiter": delim,
            "quotedOpener": bool(quote),
            "script": script_path,
            "docLine": i + 1,
        })
        i = j + 1

    with open(os.path.join(outdir, "blocks.json"), "w", encoding="utf-8") as bf:
        json.dump(blocks, bf, indent=2)
    print("extracted %d heredoc blocks from %s" % (len(blocks), doc_path), file=sys.stderr)


if __name__ == "__main__":
    main()
