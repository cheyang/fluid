#!/usr/bin/env bash
# L1 (deterministic, mock) harness for PR fluid-cloudnative/fluid#6002
#
# Contract under test (F1): after each successful `kind load docker-image <img>`
# in .github/scripts/build-all-images.sh, the local copy of <img> must be
# removed from the host docker daemon (`docker rmi <img>`), so host disk is
# freed progressively during the load loop instead of only at the end.
#
# Contract under test (F2): a failing `docker rmi` must NOT abort the script
# (set -e safety): the loop continues, a warning is printed, exit code stays 0.
#
# The real `docker`, `kind` and `make` binaries are replaced by PATH stubs that
# log every invocation. `git` and `grep` run for real (the script reads
# ./Makefile VERSION and `git rev-parse --short HEAD`), so run this from a
# real checkout of the fluid repo.
#
# Usage:
#   l1_mock_harness.sh <repo-root> [--script <path-to-build-all-images.sh>]
#                       [--rmi-fail]   # make `docker rmi` exit 1 (F2 scenario)
#
# Exit 0 iff all assertions pass.

set -u

REPO_ROOT=""
SCRIPT_OVERRIDE=""
RMI_FAIL=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --script) SCRIPT_OVERRIDE="$2"; shift 2 ;;
        --rmi-fail) RMI_FAIL=1; shift ;;
        *) REPO_ROOT="$1"; shift ;;
    esac
done
[[ -z "${REPO_ROOT}" ]] && { echo "usage: $0 <repo-root> [--script <sh>] [--rmi-fail]" >&2; exit 2; }
[[ -d "${REPO_ROOT}/.git" || -f "${REPO_ROOT}/.git" ]] || { echo "not a git checkout: ${REPO_ROOT}" >&2; exit 2; }

TARGET_SCRIPT="${SCRIPT_OVERRIDE:-${REPO_ROOT}/.github/scripts/build-all-images.sh}"
[[ -f "${TARGET_SCRIPT}" ]] || { echo "missing script: ${TARGET_SCRIPT}" >&2; exit 2; }

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT
LOG="${WORK}/calls.log"

mkdir -p "${WORK}/bin"

# --- stubs -----------------------------------------------------------------
cat > "${WORK}/bin/kind" <<EOF
#!/usr/bin/env bash
echo "kind \$*" >> "${LOG}"
exit 0
EOF

cat > "${WORK}/bin/make" <<EOF
#!/usr/bin/env bash
echo "make \$*" >> "${LOG}"
exit 0
EOF

cat > "${WORK}/bin/docker" <<EOF
#!/usr/bin/env bash
echo "docker \$*" >> "${LOG}"
if [[ "${RMI_FAIL}" == "1" && "\$1" == "rmi" ]]; then
    exit 1
fi
exit 0
EOF
chmod +x "${WORK}/bin/kind" "${WORK}/bin/make" "${WORK}/bin/docker"

# --- run the script under test --------------------------------------------
cd "${REPO_ROOT}"
set +e
PATH="${WORK}/bin:${PATH}" \
    IMG_REPO=fluidcloudnative \
    KIND_CLUSTER=fluid-cluster \
    bash "${TARGET_SCRIPT}" > "${WORK}/stdout.log" 2> "${WORK}/stderr.log"
RC=$?
set -e

echo "script=${TARGET_SCRIPT}"
echo "exit_code=${RC}"
echo "rmi_calls=$(grep -c '^docker rmi ' "${LOG}" || true)"
echo "kind_load_calls=$(grep -c '^kind load docker-image ' "${LOG}" || true)"
echo "prune_calls=$(grep -cE '^docker (image|builder|buildx|container) prune' "${LOG}" || true)"
echo "warnings=$(grep -c 'Warning: Failed to remove image' "${WORK}/stdout.log" || true)"
echo

FAIL=0
fail() { echo "FAIL: $1"; FAIL=1; }
ok()   { echo "ok:   $1"; }

# A1: script must succeed end to end (with stubbed externals)
[[ ${RC} -eq 0 ]] && ok "script exits 0" || fail "script exited ${RC}"

# A2: every loaded image is subsequently removed from local docker storage
IMAGES=(
    dataset-controller application-controller alluxioruntime-controller
    jindoruntime-controller juicefsruntime-controller cacheruntime-controller
    thinruntime-controller fluid-csi fluid-webhook fluid-crd-upgrader
)
for img in "${IMAGES[@]}"; do
    # image tag is dynamic (VERSION-gitsha); match on the repository/name part
    load_line=$(grep -n "^kind load docker-image .*/${img}:" "${LOG}" | head -1 | cut -d: -f1)
    rmi_line=$(grep -n "^docker rmi .*/${img}:" "${LOG}" | head -1 | cut -d: -f1)
    if [[ -z "${load_line}" ]]; then
        fail "kind load docker-image for ${img} never invoked"
        continue
    fi
    if [[ -z "${rmi_line}" ]]; then
        fail "docker rmi for ${img} never invoked after kind load (host copy not freed)"
        continue
    fi
    if (( rmi_line > load_line )); then
        ok "${img}: docker rmi follows kind load"
    else
        fail "${img}: docker rmi (line ${rmi_line}) does not follow kind load (line ${load_line})"
    fi
done

# A3: exactly one rmi per image (no spurious removals of other images)
rmi_count=$(grep -c '^docker rmi ' "${LOG}" || true)
if (( rmi_count == 10 )); then
    ok "exactly 10 docker rmi calls (one per image)"
else
    fail "expected 10 docker rmi calls, observed ${rmi_count}"
fi

# A4: pre-existing cleanup_docker_caches behavior is untouched
if grep -q '^docker image prune -a -f' "${LOG}" && grep -q '^docker builder prune -a -f' "${LOG}"; then
    ok "cleanup_docker_caches prunes unchanged"
else
    fail "cleanup_docker_caches prune calls missing"
fi

# A5 (F2): rmi failure is tolerated — script must still exit 0 with warnings
if [[ "${RMI_FAIL}" == "1" ]]; then
    if [[ ${RC} -eq 0 ]]; then
        ok "docker rmi failure does not abort the script (exit 0)"
    else
        fail "docker rmi failure aborted the script (exit ${RC})"
    fi
    warn_count=$(grep -c 'Warning: Failed to remove image' "${WORK}/stdout.log" || true)
    if (( warn_count == 10 )); then
        ok "warning printed for every failed rmi (${warn_count})"
    else
        fail "expected 10 warnings, observed ${warn_count}"
    fi
fi

echo
if [[ ${FAIL} -eq 0 ]]; then
    echo "L1-VERDICT: PASS"
    exit 0
else
    echo "L1-VERDICT: FAIL"
    exit 1
fi
