#!/usr/bin/env python3
"""Static integration cross-checks for PR #6175 (test-only PR).

Verifies that the names, labels, ports, keys and strings the mooncake e2e
manifests and scripts hardcode actually match what the Fluid code base
produces, so the test is wired to real behavior rather than to a guess:

  M1  all five manifests parse as YAML
  M2  CacheRuntimeClass/Dataset/CacheRuntime name+namespace wiring
  M3  reportSummary execution entry (command, `timeout` key) matches the
      CacheRuntimeClass CRD json tag consumed by GetCacheStates
  M4  job's hardcoded master address matches the component/service naming
      produced by the engine (GetComponentServiceName / GetCacheComponentName)
  M5  job declares no volumes/volumeMounts, automountServiceAccountToken false
  M6  bad_mount_pod's claimName matches the PVC name Fluid creates (runtime
      name), and the PV name checked by test.sh matches GetPersistentVolumeName
  M7  reportSummary.sh emits exactly the json tags CacheRuntimeReportSummary
      unmarshals
  M8  ports in the class match the ports the entrypoint/metrics scripts use
  M9  the CSI FailedMount wording the test greps for exists in
      pkg/utils/mount.go, and the kubectl label selector / container name
      the test uses exist in the helm chart

Run: python3 -I manifest_checks.py <repo-root>
Exit 0 iff all checks pass. Results are printed, one line per check.
"""

import re
import sys

import yaml


FAILURES = []


def check(name, ok, detail=""):
    print("%s  %s%s" % ("PASS" if ok else "FAIL", name, (" -- " + detail) if detail else ""))
    if not ok:
        FAILURES.append(name)


def main(repo):
    tdir = repo + "/test/gha-e2e/mooncake"
    docs = {}
    for f in ("cacheruntimeclass", "dataset", "cacheruntime", "rw_job", "bad_mount_pod"):
        with open("%s/%s.yaml" % (tdir, f)) as fh:
            docs[f] = list(yaml.safe_load_all(fh))
        check("M1 yaml parse: %s.yaml" % f, True)

    crc = docs["cacheruntimeclass"][0]
    ds = docs["dataset"][0]
    crt = docs["cacheruntime"][0]
    job = docs["rw_job"][0]
    pod = docs["bad_mount_pod"][0]

    name = "mooncake-demo"

    # M2: name + namespace wiring
    check("M2 class name == dataset name == runtime name == %s" % name,
          crc["metadata"]["name"] == ds["metadata"]["name"] == crt["metadata"]["name"] == name)
    check("M2 dataset/runtime namespace default",
          ds["metadata"].get("namespace") == "default" and crt["metadata"].get("namespace") == "default")
    check("M2 runtimeClassName references the class",
          crt["spec"]["runtimeClassName"] == crc["metadata"]["name"])
    check("M2 topology declares master+worker and no client",
          "master" in crc["topology"] and "worker" in crc["topology"] and "client" not in crc["topology"])
    check("M2 dataset mountPoint scheme matches fileSystemType",
          crc["fileSystemType"] == "mooncakefs"
          and ds["spec"]["mounts"][0]["mountPoint"].startswith("mooncakefs://"))
    check("M2 worker replicas 1, tieredStore emptyDir quota 1Gi",
          crt["spec"]["worker"]["replicas"] == 1
          and crt["spec"]["worker"]["tieredStore"]["levels"][0]["emptyDir"]["quota"] == "1Gi")

    # M3: reportSummary execution entry wiring
    entry = crc["topology"]["master"]["executionEntries"]["reportSummary"]
    check("M3 reportSummary command is bash -c /reportSummary.sh",
          entry["command"] == ["bash", "-c", "/reportSummary.sh"])
    with open(repo + "/api/v1alpha1/cacheruntimeclass_types.go") as fh:
        types_src = fh.read()
    check("M3 `timeout` yaml key matches ExecutionCommonEntry json tag",
          re.search(r'TimeoutSeconds int32 `json:"timeout,omitempty"`', types_src) is not None
          and entry.get("timeout") == 30)
    with open(repo + "/api/v1alpha1/common.go") as fh:
        common_src = fh.read()
    struct = re.search(r"type CacheRuntimeReportSummary struct \{(.*?)\n\}", common_src, re.S).group(1)
    json_tags = set(re.findall(r'json:"([a-zA-Z]+)', struct))
    expected = {"cached", "cachedPercentage", "cacheCapacity", "cacheHitRatio", "fileNum", "ufsTotal"}
    check("M7 reportSummary.sh json keys == CacheRuntimeReportSummary tags",
          json_tags == expected, "go tags: %s" % sorted(json_tags))

    # M4: master address wiring vs engine naming
    with open(repo + "/pkg/ddc/cache/engine/util.go") as fh:
        util_src = fh.read()
    check("M4 engine service name format is svc-<runtime>-<component>",
          re.search(r'GetComponentServiceName\(runtimeName string.*?return fmt\.Sprintf\("svc-%s-%s", runtimeName, componentType\)',
                    util_src, re.S) is not None)
    with open(repo + "/pkg/common/cacheruntime.go") as fh:
        comp_src = fh.read()
    check("M4 component name format is <runtime>-<componentType>",
          re.search(r'GetCacheComponentName\(runtimeName string.*?return fmt\.Sprintf\("%s-%s", runtimeName, componentType\)',
                    comp_src, re.S) is not None)
    args = job["spec"]["template"]["spec"]["containers"][0]["args"][0]
    check("M4 job MASTER == <component>-0.svc-<runtime>-master",
          'MASTER = "mooncake-demo-master-0.svc-mooncake-demo-master"' in args)

    # M5: the data path goes through the client library, not the PVC
    spec = job["spec"]["template"]["spec"]
    check("M5 job has no volumes/volumeMounts and no SA token",
          "volumes" not in spec and "volumeMounts" not in spec["containers"][0]
          and spec.get("automountServiceAccountToken") is False)
    check("M5 job image is the mooncake e2e image",
          spec["containers"][0]["image"] == "fluidcloudnative/mooncake:e2e")

    # M6: PVC/PV wiring
    check("M6 bad_mount_pod claims the dataset's PVC by runtime name",
          pod["spec"]["volumes"][0]["persistentVolumeClaim"]["claimName"] == name)
    with open(repo + "/pkg/utils/dataset/volume/create.go") as fh:
        create_src = fh.read()
    check("M6 PVC name == runtime name (engine sets Name: runtime.GetName())",
          re.search(r"Name:\s+runtime\.GetName\(\)", create_src) is not None)
    with open(repo + "/pkg/ddc/base/pv.go") as fh:
        pv_src = fh.read()
    check("M6 PV name format <namespace>-<name> (test.sh checks default-%s)" % name,
          re.search(r'fmt\.Sprintf\("%s-%s", info\.GetNamespace\(\), info\.GetName\(\)\)', pv_src) is not None)
    check("M6 dataset accessModes ReadWriteMany + placement Shared",
          ds["spec"]["accessModes"] == ["ReadWriteMany"] and ds["spec"]["placement"] == "Shared")

    # M8: ports
    master_ports = {p["containerPort"] for p in crc["topology"]["master"]["template"]["spec"]["containers"][0]["ports"]}
    worker_ports = {p["containerPort"] for p in crc["topology"]["worker"]["template"]["spec"]["containers"][0]["ports"]}
    check("M8 master ports {50051,8080,9003} / worker ports {50052,9300}",
          master_ports == {50051, 8080, 9003} and worker_ports == {50052, 9300})
    with open(tdir + "/image/custom-entrypoint.sh") as fh:
        entry_src = fh.read()
    with open(tdir + "/image/reportSummary.sh") as fh:
        rs_src = fh.read()
    check("M8 entrypoint/summary ports match the declared ports",
          "50051" in entry_src and "8080" in entry_src
          and "50052" in entry_src and "9300" in entry_src
          and "localhost:9003/metrics/summary" in rs_src)
    check("M8 readiness probes tcp 50051 (master) / 50052 (worker)",
          crc["topology"]["master"]["template"]["spec"]["containers"][0]["readinessProbe"]["tcpSocket"]["port"] == 50051
          and crc["topology"]["worker"]["template"]["spec"]["containers"][0]["readinessProbe"]["tcpSocket"]["port"] == 50052)

    # M9: strings the test greps / selectors it uses
    with open(repo + "/pkg/utils/mount.go") as fh:
        mount_src = fh.read()
    check("M9 CSI wording 'timeout waiting for FUSE mount point to be ready' exists in pkg/utils/mount.go",
          "timeout waiting for FUSE mount point to be ready" in mount_src)
    with open(repo + "/charts/fluid/fluid/templates/controller/cacheruntime_controller.yaml") as fh:
        chart_src = fh.read()
    check("M9 controller pod label control-plane=cacheruntime-controller and container name manager",
          "control-plane: cacheruntime-controller" in chart_src and "name: manager" in chart_src)
    with open(repo + "/pkg/common/label.go") as fh:
        label_src = fh.read()
    check("M9 label keys cacheruntime.fluid.io/name and component-name exist in pkg/common/label.go",
          'CacheRuntimeLabelAnnotationPrefix + "name"' in label_src
          and 'CacheRuntimeLabelAnnotationPrefix + "component-name"' in label_src)

    print("---")
    if FAILURES:
        print("FAILED: %s" % ", ".join(FAILURES))
        return 1
    print("All checks passed.")
    return 0


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("usage: manifest_checks.py <repo-root>")
        sys.exit(2)
    sys.exit(main(sys.argv[1]))
