"""Worker: runs inside a subprocess with PYTHONPATH pointing at a specific
msgpack install. Reproduces GHSA-j586-36cw-2gc3 (strict_map_key value leak)
and emits one JSON result line on stdout.

usage: leak_worker.py <iterations>
"""
import json
import os
import sys


def rss_kb():
    with open("/proc/self/status") as f:
        for line in f:
            if line.startswith("VmRSS"):
                return int(line.split()[1])
    return -1


def main():
    n = int(sys.argv[1]) if len(sys.argv) > 1 else 25
    import msgpack

    c_ext = hasattr(msgpack, "_cmsgpack") and msgpack.unpackb is msgpack._cmsgpack.unpackb
    payload = msgpack.packb({42: b"A" * (4 * 1024 * 1024)}, use_bin_type=True)

    def once():
        try:
            msgpack.unpackb(payload)  # strict_map_key=True is the default
            return "no-raise"
        except ValueError:
            return "value-error"
        except Exception as e:  # noqa: BLE001
            return f"other:{type(e).__name__}"

    guard = once()
    before = rss_kb()
    for _ in range(n):
        once()
    after = rss_kb()

    ok_roundtrip = True
    try:
        for o in ({"k": "v", "n": 42, "l": [1, 2]}, {"b": b"\x00\x01"}, ["s", 1.5, -7]):
            if msgpack.unpackb(msgpack.packb(o, use_bin_type=True), raw=False) != o:
                ok_roundtrip = False
    except Exception:  # noqa: BLE001
        ok_roundtrip = False

    print(json.dumps({
        "msgpack": msgpack.__version__,
        "c_ext": c_ext,
        "guard": guard,
        "iters": n,
        "rss_before_kb": before,
        "rss_after_kb": after,
        "delta_kb": after - before,
        "roundtrip_ok": ok_roundtrip,
    }))


if __name__ == "__main__":
    main()
