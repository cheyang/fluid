#!/usr/bin/env bash
# kubectl stub for scale_worker_and_verify: persists cacheruntime spec.worker.replicas
# in $STUB_STATE/spec_replicas; ASTS and scale views derive from it (ideal controller).
set -u
STATE="${STUB_STATE:?}"
args="$*"
case "$args" in
  "scale cacheruntime/"*" --replicas="*)
    n="${args##*--replicas=}"; echo "$n" > "$STATE/spec_replicas"
    echo "cacheruntime.data.fluid.io/${args#scale cacheruntime/} scaled" | sed 's/ --.*scaled/ scaled/';;
  "patch cacheruntime "*" --type merge -p "*)
    p="${args##* -p }"; n=$(echo "$p" | grep -o '"replicas":[0-9]*' | grep -o '[0-9]*$')
    echo "$n" > "$STATE/spec_replicas";;
  "get cacheruntime "*" -ojsonpath={@.spec.worker.replicas}")
    cat "$STATE/spec_replicas" 2>/dev/null || true;;
  "get cacheruntime "*" -ojsonpath={@.status.worker.readyReplicas}")
    cat "$STATE/spec_replicas" 2>/dev/null || true;;
  "get cacheruntime "*" -ojsonpath={@.status.worker.desiredReplicas}")
    cat "$STATE/spec_replicas" 2>/dev/null || true;;
  "get --raw /apis/data.fluid.io/v1alpha1/namespaces/default/cacheruntimes/"*"/scale")
    n=$(cat "$STATE/spec_replicas" 2>/dev/null || echo 0)
    s="${STUB_SCALE_SPEC_REPLICAS:-$n}"   # allow fault injection for the scale-GET assertion
    echo "{\"kind\":\"Scale\",\"spec\":{\"replicas\":$s},\"status\":{\"replicas\":$n}}";;
  "get advancedstatefulset "*" -ojsonpath={@.spec.replicas}")
    cat "$STATE/spec_replicas" 2>/dev/null || true;;
  "get advancedstatefulset "*" -ojsonpath={@.status.readyReplicas}")
    cat "$STATE/spec_replicas" 2>/dev/null || true;;
  "get nodes --no-headers")
    n="${STUB_NODES:-1}"; yes "node" 2>/dev/null | head -n "$n";;
  *) echo "kubectl-stub: unhandled: $args" >&2; exit 3;;
esac
exit 0
