#!/bin/bash
# L5 (polarity: finding-confirmation — demonstrates F10 vacuous GC selector).
# Creates an ASTS + headless SVC carrying the labels the controller ACTUALLY sets
# (getCommonLabelsFromComponent: cacheruntime.fluid.io/{name,component-name}),
# then runs the selector wait_runtime_deleted uses (fluid.io/managed-by=fluid).
set -uo pipefail
NS=verify-6175-gc
kubectl create namespace $NS >/dev/null 2>&1
cat <<YAML | kubectl apply -f - >/dev/null
apiVersion: workload.fluid.io/v1alpha1
kind: AdvancedStatefulSet
metadata: {name: mooncake-demo-master, namespace: $NS, labels: {cacheruntime.fluid.io/name: mooncake-demo, cacheruntime.fluid.io/component-name: mooncake-demo-master}}
spec:
  replicas: 0
  serviceName: svc-mooncake-demo-master
  selector: {matchLabels: {cacheruntime.fluid.io/name: mooncake-demo, cacheruntime.fluid.io/component-name: mooncake-demo-master}}
  template:
    metadata: {labels: {cacheruntime.fluid.io/name: mooncake-demo, cacheruntime.fluid.io/component-name: mooncake-demo-master}}
    spec: {containers: [{name: master, image: busybox:1.36, command: ["sleep","3600"]}]}
---
apiVersion: v1
kind: Service
metadata: {name: svc-mooncake-demo-master, namespace: $NS, labels: {cacheruntime.fluid.io/name: mooncake-demo, cacheruntime.fluid.io/component-name: mooncake-demo-master}}
spec: {clusterIP: None, selector: {cacheruntime.fluid.io/name: mooncake-demo, cacheruntime.fluid.io/component-name: mooncake-demo-master}, ports: [{port: 50051}]}
YAML
echo "resources that exist:"; kubectl get advancedstatefulset,svc -n $NS -oname
echo "TEST selector  -l fluid.io/managed-by=fluid      => [$(kubectl get advancedstatefulset,daemonset,svc -l fluid.io/managed-by=fluid -n $NS -oname | tr "\n" " ")]"
echo "RIGHT selector -l cacheruntime.fluid.io/name=... => [$(kubectl get advancedstatefulset,daemonset,svc -l cacheruntime.fluid.io/name=mooncake-demo -n $NS -oname | tr "\n" " ")]"
kubectl delete namespace $NS --wait=false >/dev/null 2>&1
echo "F10 CONFIRMED if the TEST selector is empty while resources exist."
