#!/usr/bin/env bash
# checks.sh — L1 harness for PR #6286 (mooncake clientless runtime-config injection e2e).
#
# Every check exercises the REAL artifacts shipped by the PR (extracted at run time
# from test/gha-e2e/mooncake/{test.sh,rw_job.yaml,image/custom-entrypoint.sh}),
# not copies, so a change to the PR invalidates the extraction instead of silently
# testing stale text.
#
# Output: one line per check, "NAME<TAB>pass|fail", consumed by run_l1.sh.
set -u

HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FIXTURES="$HARNESS_DIR/fixtures"
REPO_ROOT="$(cd "$HARNESS_DIR/../../.." && pwd)"
TESTDIR="$REPO_ROOT/test/gha-e2e/mooncake"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

RESULTS="$WORKDIR/results.txt"
: > "$RESULTS"

note() { echo "    $*" >&2; }

check() { # check <name> <cmd...>; runs in a subshell, non-zero => fail
  local name="$1"; shift
  local out
  if out=$( "$@" 2>&1 ); then
    printf '%s\tpass\n' "$name" >> "$RESULTS"
    note "PASS $name"
  else
    printf '%s\tfail\n' "$name" >> "$RESULTS"
    note "FAIL $name: $out"
  fi
}

# ---------------------------------------------------------------- premise (P0)
# The PR claims to add coverage that does not exist on the base branch. The check
# runs against the BASE (merge-base with origin/master), per the premise rules.
premise_no_clientless_coverage_on_base() {
  local base
  base=$(git -C "$REPO_ROOT" merge-base HEAD origin/master 2>/dev/null) || return 1
  local hits=0
  for f in test.sh rw_job.yaml image/custom-entrypoint.sh; do
    hits=$((hits + $(git -C "$REPO_ROOT" show "$base:test/gha-e2e/mooncake/$f" 2>/dev/null | grep -c 'clientless.fluid.io' || true)))
  done
  [ "$hits" -eq 0 ] || { echo "base already mentions clientless $hits times"; return 1; }
  # and the PR head does add it
  grep -q 'clientless.fluid.io' "$TESTDIR/rw_job.yaml" || { echo "head lost the label"; return 1; }
}

# ------------------------------------------------------- extraction of artifacts
extract_rw_job_script() { # -> $WORKDIR/rw-job-script.sh
  python3 - "$TESTDIR/rw_job.yaml" "$WORKDIR/rw-job-script.sh" <<'PY'
import sys, yaml
doc = yaml.safe_load(open(sys.argv[1]))
containers = doc["spec"]["template"]["spec"]["containers"]
c = [c for c in containers if c["name"] == "rw"]
assert len(c) == 1, "expected exactly one 'rw' container"
c = c[0]
assert c["command"] == ["/bin/sh", "-ec"], "command changed: %r" % c["command"]
script = c["args"][0]
# the heredoc terminator must sit at column 0 after YAML strips the block indent
assert "\nPY" in script and script.splitlines()[-1] == "PY", "heredoc terminator not at line start"
open(sys.argv[2], "w").write(script)
PY
}

extract_jq_program() { # -> $WORKDIR/jq-program.jq  (the exact program shipped in test.sh)
  python3 - "$TESTDIR/test.sh" "$WORKDIR/jq-program.jq" <<'PY'
import re, sys
src = open(sys.argv[1]).read()
m = re.search(
    r"jq -e --arg volume \"fluid-runtime-config-\$dataset_name\" "
    r"--arg dir \"/etc/fluid/config/\$dataset_name\" '\n(.*?)\n\s*' <<< ",
    src, re.S)
assert m, "could not locate the injection jq program in test.sh"
open(sys.argv[2], "w").write(m.group(1))
PY
}

# ------------------------------------------------------------------ stub python3
make_python_stub() { # $1 = dir; dump file comes from $PYTHON_STUB_OUT at run time
  mkdir -p "$1"
  cat > "$1/python3" <<'STUB'
#!/bin/sh
# Stub for the rw job's 'exec python3 - <<PY'. Emulates the real script's
# os.environ[...] lookups: missing MASTER_NAME / MASTER_SERVICE_NAME / POD_IP
# behave like a KeyError (exit 1), otherwise record the environment.
# The dump path arrives via PYTHON_STUB_OUT.
cat > /dev/null
out="${PYTHON_STUB_OUT:?PYTHON_STUB_OUT not set}"
for v in MASTER_NAME MASTER_SERVICE_NAME POD_IP; do
  eval "val=\${$v:-}"
  if [ -z "$val" ]; then
    echo "KeyError: '$v'" >> "$out"
    exit 1
  fi
done
env | grep -E '^(MASTER_|WORKER_|POD_IP|FLUID_)' | sort > "$out"
exit 0
STUB
  chmod +x "$1/python3"
}

# ------------------------------------------------------------------ stub mooncake_client
make_worker_stub() { # $1 = dir; dump file comes from $WORKER_STUB_OUT at run time
  mkdir -p "$1"
  cat > "$1/mooncake_client" <<'STUB'
#!/bin/sh
printf '%s\n' "$@" > "${WORKER_STUB_OUT:?WORKER_STUB_OUT not set}"
exit 0
STUB
  chmod +x "$1/mooncake_client"
}

# ================================================================= T_rw_job_script
rw_job_sources_runtime_sh() (
  extract_rw_job_script || return 1
  make_python_stub "$WORKDIR/stubbin"
  PYTHON_STUB_OUT="$WORKDIR/python-env.txt" \
    FLUID_RUNTIME_CONFIG_PATH_MOONCAKE_DEMO="$FIXTURES/runtime.sh" \
    POD_IP=10.42.0.5 PATH="$WORKDIR/stubbin:$PATH" \
    /bin/sh -ec "$(cat "$WORKDIR/rw-job-script.sh")" || return 1
  grep -q '^MASTER_NAME=mooncake-demo-master$' "$WORKDIR/python-env.txt" || { echo "MASTER_NAME missing"; cat "$WORKDIR/python-env.txt"; return 1; }
  grep -q '^MASTER_SERVICE_NAME=svc-mooncake-demo-master$' "$WORKDIR/python-env.txt" || { echo "MASTER_SERVICE_NAME missing"; return 1; }
  grep -q '^POD_IP=10.42.0.5$' "$WORKDIR/python-env.txt" || { echo "POD_IP missing"; return 1; }
)

rw_job_fails_fast_without_injection() (
  extract_rw_job_script || return 1
  local out rc
  out=$(env -u FLUID_RUNTIME_CONFIG_PATH_MOONCAKE_DEMO /bin/sh -ec "$(cat "$WORKDIR/rw-job-script.sh")" 2>&1)
  rc=$?
  [ "$rc" -ne 0 ] || { echo "expected non-zero exit without injection, got 0"; return 1; }
  case "$out" in *"missing injected runtime config"*) return 0;; esac
  echo "expected the :? guard message, got: $out"; return 1
)

rw_job_bite_missing_master_name() ( # harness-bites: stub must fail without the export
  extract_rw_job_script || return 1
  make_python_stub "$WORKDIR/stubbin"
  PYTHON_STUB_OUT="$WORKDIR/python-env2.txt" \
    FLUID_RUNTIME_CONFIG_PATH_MOONCAKE_DEMO="$FIXTURES/runtime-no-master-name.sh" \
    POD_IP=10.42.0.5 PATH="$WORKDIR/stubbin:$PATH" \
    /bin/sh -ec "$(cat "$WORKDIR/rw-job-script.sh")" >/dev/null 2>&1 \
    && { echo "stub did not fail without MASTER_NAME export"; return 1; }
  grep -q "KeyError: 'MASTER_NAME'" "$WORKDIR/python-env2.txt" || { echo "stub recorded no KeyError"; return 1; }
  return 0
)

rw_job_yaml_parses() (
  extract_rw_job_script
)

# ================================================================= T_worker_entrypoint
worker_entrypoint_sources_runtime_sh() (
  make_worker_stub "$WORKDIR/wstubbin"
  mkdir -p "$WORKDIR/wconf"
  cp "$FIXTURES/runtime.sh" "$WORKDIR/wconf/runtime.sh"
  : > "$WORKDIR/wconf/runtime.json" # only its path is used
  FLUID_RUNTIME_CONFIG_PATH="$WORKDIR/wconf/runtime.json" \
    FLUID_DATASET_NAMESPACE=fl POD_NAME=mooncake-demo-worker-0 \
    WORKER_STUB_OUT="$WORKDIR/worker-args.txt" PATH="$WORKDIR/wstubbin:$PATH" \
    bash "$TESTDIR/image/custom-entrypoint.sh" worker start || return 1
  grep -q -- '--master_server_address=svc-mooncake-demo-master.fl.svc.cluster.local:50051' "$WORKDIR/worker-args.txt" || { echo "bad master addr"; cat "$WORKDIR/worker-args.txt"; return 1; }
  grep -q -- '--metadata_server=http://svc-mooncake-demo-master.fl.svc.cluster.local:8080/metadata' "$WORKDIR/worker-args.txt" || { echo "bad metadata addr"; return 1; }
  grep -q -- '--global_segment_size=1GB' "$WORKDIR/worker-args.txt" || { echo "quota 1Gi not converted to 1GB"; cat "$WORKDIR/worker-args.txt"; return 1; }
  grep -q -- '--host=mooncake-demo-worker-0.svc-mooncake-demo-worker.fl.svc.cluster.local' "$WORKDIR/worker-args.txt" || { echo "bad worker host"; return 1; }
)

worker_entrypoint_missing_path_fails() (
  make_worker_stub "$WORKDIR/wstubbin"
  local out rc
  out=$(env -u FLUID_RUNTIME_CONFIG_PATH WORKER_STUB_OUT="$WORKDIR/worker-args.txt" PATH="$WORKDIR/wstubbin:$PATH" \
    bash "$TESTDIR/image/custom-entrypoint.sh" worker start 2>&1); rc=$?
  [ "$rc" -ne 0 ] || { echo "expected failure with unset path"; return 1; }
  case "$out" in *"missing runtime config path"*) ;; *) echo "unexpected message: $out"; return 1;; esac
  out=$(FLUID_RUNTIME_CONFIG_PATH="" WORKER_STUB_OUT="$WORKDIR/worker-args.txt" PATH="$WORKDIR/wstubbin:$PATH" \
    bash "$TESTDIR/image/custom-entrypoint.sh" worker start 2>&1); rc=$?
  [ "$rc" -ne 0 ] || { echo "expected failure with empty path"; return 1; }
  return 0
)

worker_entrypoint_path_with_spaces() (
  make_worker_stub "$WORKDIR/wstubbin"
  mkdir -p "$WORKDIR/config dir"
  cp "$FIXTURES/runtime.sh" "$WORKDIR/config dir/runtime.sh"
  : > "$WORKDIR/config dir/runtime.json"
  FLUID_RUNTIME_CONFIG_PATH="$WORKDIR/config dir/runtime.json" \
    FLUID_DATASET_NAMESPACE=default POD_NAME=mooncake-demo-worker-0 \
    WORKER_STUB_OUT="$WORKDIR/worker-args.txt" PATH="$WORKDIR/wstubbin:$PATH" \
    bash "$TESTDIR/image/custom-entrypoint.sh" worker start || return 1
  grep -q -- '--master_server_address=svc-mooncake-demo-master.default.svc.cluster.local:50051' "$WORKDIR/worker-args.txt"
)

worker_entrypoint_bite_wrong_service_name() ( # harness-bites: the assertion must be discriminating
  make_worker_stub "$WORKDIR/wstubbin"
  mkdir -p "$WORKDIR/wconf"
  sed "s/svc-mooncake-demo-worker/svc-typo-demo-worker/" "$FIXTURES/runtime.sh" > "$WORKDIR/wconf/runtime.sh"
  : > "$WORKDIR/wconf/runtime.json"
  FLUID_RUNTIME_CONFIG_PATH="$WORKDIR/wconf/runtime.json" \
    FLUID_DATASET_NAMESPACE=default POD_NAME=mooncake-demo-worker-0 \
    WORKER_STUB_OUT="$WORKDIR/worker-args.txt" PATH="$WORKDIR/wstubbin:$PATH" \
    bash "$TESTDIR/image/custom-entrypoint.sh" worker start >/dev/null 2>&1 || { echo "entrypoint failed on typo fixture"; return 1; }
  if grep -q -- '--host=mooncake-demo-worker-0.svc-mooncake-demo-worker.default.svc.cluster.local' "$WORKDIR/worker-args.txt"; then
    echo "the --host assertion is not discriminating: it passed on a wrong service name"
    return 1
  fi
  return 0
)

# ================================================================= T_injection_jq
run_shipped_jq() { # $1 = pod-list json; exit code mirrors check_runtime_config_injection
  extract_jq_program || return 2
  jq -e --arg volume "fluid-runtime-config-mooncake-demo" \
         --arg dir "/etc/fluid/config/mooncake-demo" \
         --from-file "$WORKDIR/jq-program.jq" < "$1" >/dev/null 2>&1
}

injection_jq_injected_pod_passes() ( run_shipped_jq "$FIXTURES/pod-injected.json" )
injection_jq_bare_pod_fails()       ( ! run_shipped_jq "$FIXTURES/pod-bare.json" )
injection_jq_volume_without_env_fails() ( ! run_shipped_jq "$FIXTURES/pod-volume-only.json" )
injection_jq_wrong_env_value_fails()    ( ! run_shipped_jq "$FIXTURES/pod-env-wrong-value.json" )
injection_jq_mount_not_readonly_fails() ( ! run_shipped_jq "$FIXTURES/pod-mount-not-readonly.json" )
injection_jq_empty_pod_list_fails()     ( ! run_shipped_jq "$FIXTURES/pod-empty-list.json" )

# ================================================================= T_rejection_grep
# Replays the decision logic of check_missing_dataset_rejected (same substring
# patterns, same branching) against fixture kubectl outputs.
rejection_check_logic() { # $1 = kubectl exit code, $2 = combined output
  local result="$2"
  if [ "$1" -eq 0 ]; then
    echo "expected admission to reject the missing dataset" >&2
    return 1 # panic path in test.sh
  fi
  if [[ "$result" != *'admission webhook "clientless.fluid.io" denied the request'* ||
        "$result" != *"\"mooncake-missing\" not found"* ]]; then
    echo "unexpected admission error" >&2
    return 1
  fi
}

rejection_grep_denial_message_matched() (
  rejection_check_logic 1 "$(cat "$FIXTURES/kubectl-denial.txt")"
)

rejection_grep_fires_when_webhook_absent() (
  # kubectl succeeds (dry-run create admitted) exactly when the cluster has no
  # clientless.fluid.io webhook — the state of master (and of this PR's CI) until
  # #6197 merges. The check must fire (panic) in that state.
  if rejection_check_logic 0 "$(cat "$FIXTURES/kubectl-no-webhook.txt")" 2>/dev/null; then
    echo "check accepted a cluster without the webhook"; return 1
  fi
  return 0
)

# ================================================================= T_drift guards
drift_testsh_contains_rejection_patterns() (
  grep -q 'admission webhook "clientless.fluid.io" denied the request' "$TESTDIR/test.sh" &&
  grep -q '\\"\$missing_dataset\\" not found' "$TESTDIR/test.sh"
)

drift_shell_syntax() (
  bash -n "$TESTDIR/test.sh" && bash -n "$TESTDIR/image/custom-entrypoint.sh"
)

# ================================================================= run everything
check T_premise__no_clientless_coverage_on_base premise_no_clientless_coverage_on_base
check T_rw_job_script__yaml_parses rw_job_yaml_parses
check T_rw_job_script__sources_runtime_sh rw_job_sources_runtime_sh
check T_rw_job_script__fails_fast_without_injection rw_job_fails_fast_without_injection
check T_rw_job_script__bite_missing_master_name rw_job_bite_missing_master_name
check T_worker_entrypoint__sources_runtime_sh worker_entrypoint_sources_runtime_sh
check T_worker_entrypoint__missing_path_fails worker_entrypoint_missing_path_fails
check T_worker_entrypoint__path_with_spaces worker_entrypoint_path_with_spaces
check T_worker_entrypoint__bite_wrong_service_name worker_entrypoint_bite_wrong_service_name
check T_injection_jq__injected_pod_passes injection_jq_injected_pod_passes
check T_injection_jq__bare_pod_fails injection_jq_bare_pod_fails
check T_injection_jq__volume_without_env_fails injection_jq_volume_without_env_fails
check T_injection_jq__wrong_env_value_fails injection_jq_wrong_env_value_fails
check T_injection_jq__mount_not_readonly_fails injection_jq_mount_not_readonly_fails
check T_injection_jq__empty_pod_list_fails injection_jq_empty_pod_list_fails
check T_rejection_grep__denial_message_matched rejection_grep_denial_message_matched
check T_rejection_grep__fires_when_webhook_absent rejection_grep_fires_when_webhook_absent
check T_drift__testsh_contains_rejection_patterns drift_testsh_contains_rejection_patterns
check T_drift__shell_syntax drift_shell_syntax

cat "$RESULTS"
