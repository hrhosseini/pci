#!/usr/bin/env bash
set -euo pipefail

readonly root="$(cd "$(dirname "$0")/../.." && pwd)"
readonly temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/pci-phase6-safety.XXXXXX")"
trap 'rm -rf -- "$temp_dir"' EXIT
source "$root/scripts/integration/harness-lib.sh"

mkdir -p "$temp_dir/bin" "$temp_dir/tools"
cat > "$temp_dir/bin/kind" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf 'kind %s\n' "$*" >> "$PCI_SAFETY_LOG"
case "${1:-} ${2:-}" in
  "version ") echo "kind v0.33.0 go1.24.0 linux/amd64" ;;
  "get clusters")
    if [[ "${PCI_FAKE_CLUSTER:-}" == "present" ]]; then
      echo pci-phase6
    fi
    ;;
esac
SH
cat > "$temp_dir/tools/kubectl-v1.32.2" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf 'kubectl %s\n' "$*" >> "$PCI_SAFETY_LOG"
case "$*" in
  *"clusters[0].cluster.server"*) echo "https://127.0.0.1:12345" ;;
  *".data.ownerToken"*) echo "${PCI_FAKE_OWNER_TOKEN:-expected-token}" ;;
  *".data.owner}"*) echo "policychangeimpact-phase6" ;;
  *".data.cluster}"*) echo "pci-phase6" ;;
  *".data.kubernetes}"*) echo "v1.31.4" ;;
  *".data.calico}"*) echo "v3.30.0" ;;
  *"serverVersion.gitVersion"*) echo "v1.31.4" ;;
  *"clientVersion.gitVersion"*) echo "v1.32.2" ;;
  *"numberReady}"*) echo 1 ;;
  *"desiredNumberScheduled}"*) echo 1 ;;
  *"create -f -"*)
    if [[ "${PCI_FAKE_RESOURCE_CREATE_FAILURE:-}" == "1" ]]; then
      echo "fake API rejected test resource creation" >&2
      exit 1
    fi
    ;;
  *"get namespace"*) exit 1 ;;
  *) ;;
esac
SH
chmod 700 "$temp_dir/bin/kind" "$temp_dir/tools/kubectl-v1.32.2"
touch "$temp_dir/kubeconfig"
printf 'expected-token\n' > "$temp_dir/kubeconfig.owner-token"

export PATH="$temp_dir/bin:$PATH"
export PCI_LIVE_TOOLS_DIR="$temp_dir/tools"
export PCI_SAFETY_LOG="$temp_dir/commands.log"
export PCI_LIVE_KUBECONFIG="$temp_dir/kubeconfig"
export PCI_LIVE_RESULTS="$temp_dir/results.jsonl"
readonly log="$PCI_SAFETY_LOG"

classify_probe_result TCP 0 ""
[[ "$POLL_ATTEMPT_OUTCOME" == success ]]
classify_probe_result TCP 1 "nc: connection refused"
[[ "$POLL_ATTEMPT_OUTCOME" == connection_failure ]]
classify_probe_result TCP 124 ""
[[ "$POLL_ATTEMPT_OUTCOME" == timeout && "$POLL_ATTEMPT_ERROR_CLASS" == probe_timeout ]]
classify_probe_result UDP 0 ""
[[ "$POLL_ATTEMPT_OUTCOME" == unsupported && "$POLL_ATTEMPT_ERROR_CLASS" == unsupported_probe_protocol ]]
classify_probe_result TCP 127 "nc: not found"
[[ "$POLL_ATTEMPT_OUTCOME" == unsupported && "$POLL_ATTEMPT_ERROR_CLASS" == unsupported_probe_utility ]]
classify_probe_result TCP 1 "Error from server (Forbidden): pods/exec"
[[ "$POLL_ATTEMPT_OUTCOME" == incomplete_setup && "$POLL_ATTEMPT_ERROR_CLASS" == exec_permission_denied ]]
classify_probe_result TCP 1 "pod client-a not found"
[[ "$POLL_ATTEMPT_OUTCOME" == incomplete_setup ]]

sequence=()
sequence_index=0
fake_probe() {
  POLL_ATTEMPT_OUTCOME="${sequence[$sequence_index]}"
  POLL_ATTEMPT_ERROR_CLASS=""
  ((sequence_index += 1))
}

PROBE_POLL_INTERVAL_SECONDS=0
PROBE_POLL_MAX_ATTEMPTS=3
PROBE_POLL_STABLE_COUNT=2
sequence=(success success)
sequence_index=0
poll_probe allowed_by_model fake_probe
[[ "$POLL_OUTCOME" == success && "$POLL_AGREEMENT" == true && "$POLL_ATTEMPTS" -eq 2 ]]

sequence=(connection_failure connection_failure)
sequence_index=0
poll_probe denied_by_model fake_probe
[[ "$POLL_OUTCOME" == connection_failure && "$POLL_AGREEMENT" == true && "$POLL_ATTEMPTS" -eq 2 ]]

sequence=(success connection_failure success)
sequence_index=0
poll_probe allowed_by_model fake_probe
[[ "$POLL_OUTCOME" == inconclusive && "$POLL_AGREEMENT" == inconclusive &&
  "$POLL_ERROR_CLASS" == no_stable_observation && "$POLL_ATTEMPTS" -eq 3 &&
  "${POLL_OBSERVATIONS[*]}" == "success connection_failure success" ]]

fake_timeout() {
  POLL_ATTEMPT_OUTCOME=timeout
  POLL_ATTEMPT_ERROR_CLASS=probe_timeout
}
poll_probe allowed_by_model fake_timeout
[[ "$POLL_OUTCOME" == timeout && "$POLL_AGREEMENT" == inconclusive &&
  "${POLL_OBSERVATIONS[*]}" == timeout ]]

fake_unsupported() {
  POLL_ATTEMPT_OUTCOME=unsupported
  POLL_ATTEMPT_ERROR_CLASS=unsupported_probe_utility
}
poll_probe allowed_by_model fake_unsupported
[[ "$POLL_OUTCOME" == unsupported && "$POLL_ERROR_CLASS" == unsupported_probe_utility ]]

fake_unsupported_protocol() {
  POLL_ATTEMPT_OUTCOME=unsupported
  POLL_ATTEMPT_ERROR_CLASS=unsupported_probe_protocol
}
poll_probe allowed_by_model fake_unsupported_protocol
[[ "$POLL_OUTCOME" == unsupported && "$POLL_ERROR_CLASS" == unsupported_probe_protocol ]]

fake_incomplete_setup() {
  POLL_ATTEMPT_OUTCOME=incomplete_setup
  POLL_ATTEMPT_ERROR_CLASS=source_workload_unavailable
}
poll_probe allowed_by_model fake_incomplete_setup
[[ "$POLL_OUTCOME" == incomplete_setup && "$POLL_AGREEMENT" == inconclusive ]]

fake_cancelled_probe() {
  POLL_ATTEMPT_OUTCOME=success
  POLL_CANCEL_REQUESTED=1
}
POLL_CANCEL_REQUESTED=0
poll_probe allowed_by_model fake_cancelled_probe
[[ "$POLL_OUTCOME" == inconclusive && "$POLL_ERROR_CLASS" == cancelled && "$POLL_ATTEMPTS" -eq 1 ]]
POLL_CANCEL_REQUESTED=0

record_incomplete_setup "$temp_dir/setup-failure.jsonl" resource_creation api_create_failed \
  "Kubernetes rejected or could not create the labeled test resources."
python3 - "$temp_dir/setup-failure.jsonl" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    record = json.loads(source.readline())
assert record["outcome"] == "incomplete_setup"
assert record["modelObservationAgreement"] is None
assert record["policyCausation"] == "not-established"
PY
record_probe_result "$temp_dir/probe-result.jsonl" unstable-case allowed_by_model \
  allowed_by_model unchanged inconclusive inconclusive no_stable_observation 3 \
  "success connection_failure success" v1.31.4 v3.30.0 \
  "docker.io/library/busybox:1.36.1@sha256:busybox-test" \
  "docker.io/library/nginx:1.27.4-alpine@sha256:nginx-test" \
  kind-pci-phase6 pci-phase6-source/client-a 10.0.0.8 80 \
  2026-10-19T12:00:00Z 1010
record_incomplete_setup "$temp_dir/probe-result.jsonl" retry-case exec_permission_denied \
  "The source workload is unavailable."
python3 - "$temp_dir/probe-result.jsonl" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    records = [json.loads(line) for line in source]
record = records[0]
assert record["observed"] == "inconclusive"
assert record["modelObservationAgreement"] is None
assert record["policyCausation"] == "not-established-by-connectivity-probe"
assert record["probeAttemptOutcomes"] == ["success", "connection_failure", "success"]
assert record["probeAttempts"] == 3
assert record["environment"]["testImages"]["busybox"].endswith("sha256:busybox-test")
assert len(records) == 2
assert records[1]["outcome"] == "incomplete_setup"
assert records[1]["modelObservationAgreement"] is None
PY

expect_refusal() {
  local output="$1"
  shift
  if "$@" > "$output" 2>&1; then
    echo "Expected refusal: $*" >&2
    exit 1
  fi
}

assert_no_mutations() {
  if grep -Eqi '(^| )(create|delete|apply|exec)( |$)' "$log"; then
    echo "A safety check issued a cluster mutation or exec command:" >&2
    cat "$log" >&2
    exit 1
  fi
}

: > "$log"
expect_refusal "$temp_dir/setup-no-confirmation.txt" \
  env -u PCI_LIVE_TEST_CONFIRM "$root/scripts/integration/setup-kind.sh"
assert_no_mutations

: > "$log"
export PCI_FAKE_CLUSTER=present
export PCI_LIVE_TEST_CONFIRM=I_CONFIRM_PCI_PHASE6_DISPOSABLE_CLUSTER
expect_refusal "$temp_dir/setup-existing-cluster.txt" "$root/scripts/integration/setup-kind.sh"
assert_no_mutations

: > "$log"
expect_refusal "$temp_dir/run-no-confirmation.txt" \
  env -u PCI_LIVE_TEST_CONFIRM "$root/scripts/integration/run-live-tests.sh"
assert_no_mutations

: > "$log"
export PCI_FAKE_OWNER_TOKEN=wrong-token
export PCI_LIVE_TEST_CONFIRM=I_CONFIRM_PCI_PHASE6_DISPOSABLE_CLUSTER
expect_refusal "$temp_dir/run-owner-mismatch.txt" "$root/scripts/integration/run-live-tests.sh"
assert_no_mutations
[[ ! -e "$PCI_LIVE_RESULTS" ]] || {
  echo "Runner created a report before verifying cluster ownership." >&2
  exit 1
}

: > "$log"
printf 'preserve-existing-report\n' > "$temp_dir/existing-results.jsonl"
export PCI_LIVE_RESULTS="$temp_dir/existing-results.jsonl"
expect_refusal "$temp_dir/run-existing-report.txt" "$root/scripts/integration/run-live-tests.sh"
[[ "$(cat "$PCI_LIVE_RESULTS")" == "preserve-existing-report" ]] || {
  echo "Runner altered a pre-existing JSONL report." >&2
  exit 1
}
[[ ! -s "$log" ]] || {
  echo "Runner contacted a cluster before rejecting the existing report path." >&2
  cat "$log" >&2
  exit 1
}

: > "$log"
export PCI_FAKE_OWNER_TOKEN=expected-token
export PCI_FAKE_RESOURCE_CREATE_FAILURE=1
export PCI_LIVE_RESULTS="$temp_dir/partial-setup.jsonl"
expect_refusal "$temp_dir/partial-setup.txt" "$root/scripts/integration/run-live-tests.sh"
python3 - "$PCI_LIVE_RESULTS" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    record = json.loads(source.readline())
assert record["outcome"] == "incomplete_setup"
assert record["stage"] == "setup"
assert record["modelObservationAgreement"] is None
PY

: > "$log"
expect_refusal "$temp_dir/teardown-no-confirmation.txt" \
  env -u PCI_LIVE_DELETE_CONFIRM "$root/scripts/integration/teardown-kind.sh"
assert_no_mutations

: > "$log"
export PCI_FAKE_OWNER_TOKEN=wrong-token
export PCI_LIVE_DELETE_CONFIRM=I_CONFIRM_DELETE_PCI_PHASE6_CLUSTER
expect_refusal "$temp_dir/teardown-owner-mismatch.txt" "$root/scripts/integration/teardown-kind.sh"
assert_no_mutations

echo "Integration harness safety guards passed (all cluster commands were faked)."
