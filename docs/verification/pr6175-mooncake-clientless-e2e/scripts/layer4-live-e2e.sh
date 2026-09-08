#!/bin/bash
# L4 (polarity: positive — the full PR test MUST pass on a Fluid WITH #6157).
# Runs test/gha-e2e/mooncake/test.sh against ACK with master cacheruntime+dataset
# controllers. Steps: build+push master controllers to Docker Hub (rolebasedgroup),
# swap both deployments (dataset-controller too: api/v1alpha1/common.go gained
# FileNum/UfsTotal after the deployed 36f0467), repoint manifests to the pushed
# mooncake image, run test.sh, then RESTORE the original controllers.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
REPO=rolebasedgroup
TAG=verify-6175
NS=fluid-system
ORIG_IMG=fluidcloudnative

echo "== [1/6] build master controllers from PR head =="
make docker-build-cacheruntime-controller docker-build-dataset-controller \
  IMG_REPO=$REPO IMG_TAG=$TAG >/tmp/l4-build.log 2>&1 \
  || { echo "controller build FAILED"; tail -30 /tmp/l4-build.log; exit 1; }

echo "== [2/6] push to Docker Hub ($REPO) =="
for c in cacheruntime-controller dataset-controller; do
  docker push $REPO/$c:$TAG >/tmp/l4-push-$c.log 2>&1 \
    || { echo "push $c FAILED"; tail -20 /tmp/l4-push-$c.log; exit 1; }
done

echo "== [3/6] record current controller images (for restore) =="
kubectl get deploy -n $NS -l control-plane=cacheruntime-controller \
  -ojsonpath="{.items[0].spec.template.spec.containers[0].image}" > /tmp/l4-orig-cr.img
kubectl get deploy -n $NS -l control-plane=dataset-controller \
  -ojsonpath="{.items[0].spec.template.spec.containers[0].image}" > /tmp/l4-orig-ds.img
echo "  cacheruntime: $(cat /tmp/l4-orig-cr.img)"
echo "  dataset:      $(cat /tmp/l4-orig-ds.img)"

echo "== [4/6] swap both deployments to master + repoint mooncake image =="
kubectl set image deploy -n $NS -l control-plane=cacheruntime-controller \
  manager=$REPO/cacheruntime-controller:$TAG
kubectl set image deploy -n $NS -l control-plane=dataset-controller \
  manager=$REPO/dataset-controller:$TAG
kubectl rollout status deploy -n $NS -l control-plane=cacheruntime-controller --timeout=180s
kubectl rollout status deploy -n $NS -l control-plane=dataset-controller --timeout=180s
mkdir -p /tmp/mc-live && cp test/gha-e2e/mooncake/*.yaml /tmp/mc-live/
sed -i "s#fluidcloudnative/mooncake:e2e#$REPO/mooncake:$TAG#g" /tmp/mc-live/*.yaml 2>/dev/null || true
grep -rl "$ORIG_IMG/mooncake" /tmp/mc-live | xargs -r sed -i "s#$ORIG_IMG/mooncake:e2e#$REPO/mooncake:e2e#g"

echo "== [5/6] run test.sh =="
TESTDIR=/tmp/mc-live bash test/gha-e2e/mooncake/test.sh 2>&1 | tee /tmp/l4-test.log
rc=${PIPESTATUS[0]}

echo "== [6/6] RESTORE original controllers =="
kubectl set image deploy -n $NS -l control-plane=cacheruntime-controller \
  manager=$(cat /tmp/l4-orig-cr.img)
kubectl set image deploy -n $NS -l control-plane=dataset-controller \
  manager=$(cat /tmp/l4-orig-ds.img)
kubectl rollout status deploy -n $NS -l control-plane=cacheruntime-controller --timeout=180s
kubectl rollout status deploy -n $NS -l control-plane=dataset-controller --timeout=180s

if [[ $rc -eq 0 ]]; then
  echo "L4 PASS: full test.sh succeeded on master Fluid (premise #6157 fix present)"
else
  echo "L4 FAIL (rc=$rc): see /tmp/l4-test.log"
fi
exit $rc
