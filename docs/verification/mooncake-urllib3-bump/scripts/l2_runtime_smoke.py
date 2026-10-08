#!/usr/bin/env python3
"""L2 runtime smoke: requests 2.34.2 on top of urllib3 2.8.0 serving a real HTTP
round-trip against a local http.server. Mirrors how the e2e image's packages
actually interact (mooncake's python code talks plain HTTP via requests).

Contract polarity: exit 0 = the pinned combo works; non-zero = incompatible.
Run inside a venv that has requests==2.34.2 and urllib3==2.8.0 installed.
"""
import http.server
import threading

import requests
import urllib3

assert urllib3.__version__ == "2.8.0", urllib3.__version__
assert requests.__version__ == "2.34.2", requests.__version__

srv = http.server.HTTPServer(("127.0.0.1", 0), http.server.SimpleHTTPRequestHandler)
threading.Thread(target=srv.serve_forever, daemon=True).start()
url = "http://127.0.0.1:%d/" % srv.server_address[1]

r = requests.get(url, timeout=10)
assert r.status_code == 200, r.status_code

# exercise a streamed read too (the GHSA-vxq7-64xx-v4gw code path)
n = sum(len(chunk) for chunk in r.iter_content(chunk_size=1024))
srv.shutdown()

# urllib3 2.8.0 must be the copy requests actually imported
from requests.packages import urllib3 as bundled  # noqa
assert bundled.__version__ == "2.8.0", bundled.__version__

print("L2b PASS: requests==%s + urllib3==%s, GET %s -> 200, streamed %d bytes"
      % (requests.__version__, urllib3.__version__, url, n))
