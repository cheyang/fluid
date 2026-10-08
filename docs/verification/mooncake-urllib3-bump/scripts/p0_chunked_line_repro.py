#!/usr/bin/env python3
"""P0 premise repro — GHSA-vxq7-64xx-v4gw against the urllib3 version given on argv[1].

Claim (from the PR body / urllib3 2.8.0 release notes):
  "HTTPResponse.stream() and read_chunked() could buffer a chunk-size line of
   unbounded length in memory" — fixed in 2.8.0 by bounding the chunk-size line
   at _MAX_CHUNK_LINE_LENGTH = 2**16 and raising ProtocolError.

Harness: a local raw-socket server answers with Transfer-Encoding: chunked and a
chunk-size line of LINE_BYTES 'f' characters (no early CRLF), then a terminating
chunk, then closes. The client streams the response.

Contract polarity (asserts intended 2.8.0 behavior):
  PASS  = ProtocolError("...exceeded maximum allowed length") raised with peak
          traced memory far below LINE_BYTES (read was bounded).
  FAIL  = the whole over-long line was buffered (peak ~ LINE_BYTES) — the
          vulnerable behavior, expected on the base pin 2.7.0.

Run under an interpreter whose urllib3 is the version under test; argv[1] is the
expected version string, asserted against urllib3.__version__.
"""
import socket
import sys
import threading
import tracemalloc

LINE_BYTES = 4 * 1024 * 1024  # 4 MiB chunk-size line
BOUND = 2**16 + 1             # _MAX_CHUNK_LINE_LENGTH + 1 in 2.8.0


def serve(port_holder, ready, payload):
    srv = socket.socket()
    srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    srv.bind(("127.0.0.1", 0))
    srv.listen(1)
    port_holder.append(srv.getsockname()[1])
    ready.set()
    conn, _ = srv.accept()
    data = b""
    while b"\r\n\r\n" not in data:
        chunk = conn.recv(4096)
        if not chunk:
            break
        data += chunk
    # payload was built before tracemalloc.start() so the server side does not
    # pollute the client measurement; the client may reset early (2.8.0 does).
    try:
        conn.sendall(b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\nContent-Type: text/plain\r\n\r\n")
        conn.sendall(payload)
        conn.sendall(b"0\r\n\r\n")
    except OSError:
        pass
    conn.close()
    srv.close()


def main():
    expected_version = sys.argv[1]
    import urllib3
    assert urllib3.__version__ == expected_version, "urllib3 %s, expected %s" % (
        urllib3.__version__, expected_version)

    payload = b"f" * LINE_BYTES + b"\r\n"  # allocated pre-tracing on purpose
    port_holder, ready = [], threading.Event()
    t = threading.Thread(target=serve, args=(port_holder, ready, payload), daemon=True)
    t.start()
    ready.wait(5)

    http = urllib3.PoolManager()
    tracemalloc.start()
    outcome = None
    try:
        resp = http.request("GET", "http://127.0.0.1:%d/" % port_holder[0], preload_content=False)
        for _ in resp.stream():
            pass
        outcome = "completed-without-error"
    except Exception as e:  # noqa: BLE001 - we want the exception, whatever it is
        outcome = "%s: %s" % (type(e).__name__, e)
    _, peak = tracemalloc.get_traced_memory()
    tracemalloc.stop()
    t.join(5)

    print("urllib3=%s outcome=%r peak_traced_bytes=%d line_bytes=%d"
          % (urllib3.__version__, outcome, peak, LINE_BYTES))

    bounded = ("ProtocolError" in outcome and "exceeded maximum allowed length" in outcome
               and peak < LINE_BYTES // 4)
    unbounded = peak >= LINE_BYTES  # whole line (or more) buffered at once

    if bounded:
        print("P0 RESULT: FIXED — chunk-size line bounded at ~64KiB, ProtocolError raised")
        sys.exit(0)
    if unbounded:
        print("P0 RESULT: VULNERABLE — over-long chunk-size line fully buffered in memory")
        sys.exit(1)
    print("P0 RESULT: INCONCLUSIVE — neither bounded rejection nor unbounded buffering observed")
    sys.exit(2)


if __name__ == "__main__":
    main()
