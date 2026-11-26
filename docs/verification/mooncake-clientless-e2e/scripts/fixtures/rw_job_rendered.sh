. "${FLUID_RUNTIME_CONFIG_PATH_MOONCAKE_DEMO:?missing injected runtime config}"
exec python3 - <<'PY'
import hashlib
import os
import sys

from mooncake.store import MooncakeDistributedStore

MASTER = f"{os.environ['MASTER_NAME']}-0.{os.environ['MASTER_SERVICE_NAME']}"

store = MooncakeDistributedStore()
rc = store.setup(
    local_hostname=os.environ["POD_IP"],
    metadata_server="http://%s:8080/metadata" % MASTER,
    master_server_addr="%s:50051" % MASTER,
    global_segment_size=0,
    local_buffer_size=128 * 1024 * 1024,
    protocol="tcp",
    rdma_devices="",
)
if rc != 0:
    sys.exit("setup failed, rc=%s" % rc)

# Mooncake's put silently skips a key that already exists (it does
# not overwrite the old value and still returns 0). With a fixed
# key, a job retry would read back the previous run's data and
# report an md5 mismatch, producing a false failure, so use a
# random key on every run.
key = "e2e-" + os.urandom(8).hex()
payload = os.urandom(4 * 1024 * 1024)

rc = store.put(key, payload)
if rc != 0:
    sys.exit("put failed, rc=%s" % rc)

got = store.get(key)
if got is None:
    sys.exit("get returned None for key %s" % key)
if len(got) != len(payload):
    sys.exit("length mismatch: got %d, want %d" % (len(got), len(payload)))

want = hashlib.md5(payload).hexdigest()
have = hashlib.md5(got).hexdigest()
print("key=%s put_md5=%s get_md5=%s" % (key, want, have))
if want != have:
    sys.exit("md5 mismatch: got %s, want %s" % (have, want))

print("mooncake put/get verified")
PY
