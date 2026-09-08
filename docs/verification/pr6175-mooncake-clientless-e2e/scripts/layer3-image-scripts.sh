#!/bin/bash
# L3 (polarity: mixed). Builds the e2e image and exercises the two scripts Fluid
# invokes plus the rw_job data path against a REAL mooncake_master/worker in docker.
#   - reportSummary.sh: positive WITH a worker segment; F1 = crashes (exit 1) master-only.
#   - custom-entrypoint.sh worker jq parsing (F2) vs a representative runtime.json.
#   - rw_job python put/get md5 round-trip (F4).
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
IMG=fluidcloudnative/mooncake:e2e
docker build -t $IMG test/gha-e2e/mooncake/image >/tmp/l3-build.log 2>&1 && echo "L2 image build: OK ($(docker images --format "{{.Size}}" $IMG))" || { echo "L2 image build FAILED"; tail -20 /tmp/l3-build.log; exit 1; }
docker rm -f mc-master mc-worker >/dev/null 2>&1; docker network create mc-net >/dev/null 2>&1
docker run -d --name mc-master --network mc-net -e POD_NAME=mc-master $IMG /custom-entrypoint.sh master start >/dev/null
sleep 12
echo "== F1: reportSummary master-only (capacity 0B, no percentage) =="
docker exec mc-master bash -c "/reportSummary.sh; echo EXIT=\$?" 2>&1 | tail -3
docker run -d --name mc-worker --network mc-net -e POD_NAME=mc-worker $IMG \
  mooncake_client --host=mc-worker --port=50052 --global_segment_size=1GB \
  --master_server_address=mc-master:50051 --metadata_server=http://mc-master:8080/metadata \
  --protocol=tcp --enable_http_server=true --http_port=9300 >/dev/null
sleep 15
echo "== reportSummary WITH worker (expect cached/cacheCapacity/fileNum/ufsTotal) =="
docker exec mc-master /reportSummary.sh 2>&1
echo "== F2: worker entrypoint jq parsing vs representative runtime.json =="
cat > /tmp/runtime.json <<"JSON"
{"master":{"service":{"name":"svc-mooncake-demo-master"}},"worker":{"service":{"name":"svc-mooncake-demo-worker"},"tieredStoreLevels":[{"quotas":["1Gi"]}]}}
JSON
docker cp /tmp/runtime.json mc-master:/tmp/runtime.json >/dev/null
docker exec -e FLUID_RUNTIME_CONFIG_PATH=/tmp/runtime.json -e FLUID_DATASET_NAMESPACE=default -e POD_NAME=mooncake-demo-worker-0 mc-master bash -c \
  'C=$(cat $FLUID_RUNTIME_CONFIG_PATH); M=$(jq -r .master.service.name <<<"$C"); W=$(jq -r .worker.service.name <<<"$C"); Q=$(jq -r ".worker.tieredStoreLevels[0].quotas[0] // \"1GiB\"" <<<"$C"); echo "MASTER_SVC=$M WORKER_SVC=$W QUOTA=$Q SEGMENT=$(sed "s/Gi$/GB/;s/Mi$/MB/" <<<"$Q") WORKER_HOST=$POD_NAME.$W.$FLUID_DATASET_NAMESPACE.svc.cluster.local"'
echo "== F4: rw_job python put/get md5 =="
sed -n "/import hashlib/,/put\/get verified/p" test/gha-e2e/mooncake/rw_job.yaml > /tmp/rw.py 2>/dev/null
docker run --rm --network mc-net -v /tmp/rw.py:/tmp/rw.py $IMG bash -c "sed -i s/mooncake-demo-master-0.svc-mooncake-demo-master/mc-master/ /tmp/rw.py; POD_IP=\$(hostname -i) python3 -c \"\$(cat /tmp/rw.py)\"" 2>&1 | grep -E "put_md5|verified|Error|error" | tail -3
docker rm -f mc-master mc-worker >/dev/null 2>&1; docker network rm mc-net >/dev/null 2>&1
