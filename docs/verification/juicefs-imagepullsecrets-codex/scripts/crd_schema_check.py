#!/usr/bin/env python3
"""CRD schema check for PR #6245: the regenerated JuiceFSRuntime CRD must
expose imagePullSecrets at spec / spec.worker / spec.fuse (and, via the shared
JuiceFSCompTemplateSpec, at spec.master / spec.jobWorker), and a CR using the
new fields must validate against the openAPIV3Schema."""
import sys, yaml, jsonschema

REPO = sys.argv[1] if len(sys.argv) > 1 else "."
CRD_PATH = REPO + "/config/crd/bases/data.fluid.io_juicefsruntimes.yaml"

crd = yaml.safe_load(open(CRD_PATH))
schema = next(v["schema"]["openAPIV3Schema"] for v in crd["spec"]["versions"] if v["name"] == "v1alpha1")
spec_props = schema["properties"]["spec"]["properties"]

failures = []
def check(cond, msg):
    print(("PASS" if cond else "FAIL") + ": " + msg)
    if not cond:
        failures.append(msg)

for path, props in [
    ("spec", spec_props),
    ("spec.worker", spec_props["worker"]["properties"]),
    ("spec.master", spec_props["master"]["properties"]),
    ("spec.jobWorker", spec_props["jobWorker"]["properties"]),
    ("spec.fuse", spec_props["fuse"]["properties"]),
]:
    check("imagePullSecrets" in props, f"{path}.imagePullSecrets present in CRD schema")

sample = {
    "apiVersion": "data.fluid.io/v1alpha1",
    "kind": "JuiceFSRuntime",
    "metadata": {"name": "hbase"},
    "spec": {
        "replicas": 2,
        "imagePullSecrets": [{"name": "test-1"}, {"name": "test-2"}],
        "worker": {"imagePullSecrets": [{"name": "worker-1"}]},
        "fuse": {"imagePullSecrets": [{"name": "fuse-1"}]},
        "tieredstore": {"levels": [{"mediumtype": "MEM", "path": "/dev/shm", "quota": "2Gi",
                                    "high": "0.95", "low": "0.7"}]},
    },
}
try:
    jsonschema.validate(sample, schema)
    print("PASS: CR with runtime/worker/fuse imagePullSecrets validates against CRD schema")
except jsonschema.ValidationError as e:
    failures.append("sample CR rejected")
    print("FAIL: sample CR rejected:", e.message)

# legacy CR (shape from before the PR) must still validate
legacy = {"apiVersion": "data.fluid.io/v1alpha1", "kind": "JuiceFSRuntime",
          "metadata": {"name": "hbase"}, "spec": {"replicas": 2}}
try:
    jsonschema.validate(legacy, schema)
    print("PASS: legacy CR still validates")
except jsonschema.ValidationError as e:
    failures.append("legacy CR rejected")
    print("FAIL: legacy CR rejected:", e.message)

sys.exit(1 if failures else 0)
