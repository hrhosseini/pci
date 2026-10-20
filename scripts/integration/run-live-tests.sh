#!/usr/bin/env bash
set -euo pipefail

readonly cluster_name="pci-phase6"
readonly context_name="kind-pci-phase6"
readonly confirmation="I_CONFIRM_PCI_PHASE6_DISPOSABLE_CLUSTER"
readonly kubernetes_version="v1.31.4"
readonly calico_version="v3.30.0"
readonly kubectl_version="v1.32.2"
readonly busybox_image="docker.io/library/busybox:1.36.1@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662"
readonly nginx_image="docker.io/library/nginx:1.27.4-alpine@sha256:4ff102c5d78d254a6f0da062b3cf39eaf07f01eec0927fd21e219d0af8bc0591"
readonly owner_label="pci-phase6-owner"
readonly namespace_source="pci-phase6-source"
readonly namespace_destination="pci-phase6-destination"
readonly namespace_unrelated="pci-phase6-unrelated"
readonly root="$(cd "$(dirname "$0")/../.." && pwd)"
source "$(dirname "$0")/harness-lib.sh"

if [[ "${PCI_LIVE_TEST_CONFIRM:-}" != "$confirmation" ]]; then
  echo "Refusing live tests. Set PCI_LIVE_TEST_CONFIRM=$confirmation after reviewing the integration-test documentation." >&2
  exit 2
fi
if [[ -z "${PCI_LIVE_KUBECONFIG:-}" || ! -f "$PCI_LIVE_KUBECONFIG" || -L "$PCI_LIVE_KUBECONFIG" ]]; then
  echo "Set PCI_LIVE_KUBECONFIG to the kubeconfig created by setup-kind.sh." >&2
  exit 2
fi
if [[ ! -f "${PCI_LIVE_KUBECONFIG}.owner-token" || -L "${PCI_LIVE_KUBECONFIG}.owner-token" ]]; then
  echo "The setup ownership token is missing; refusing to use this cluster." >&2
  exit 2
fi
if [[ -z "${PCI_LIVE_RESULTS:-}" ]]; then
  echo "Set PCI_LIVE_RESULTS to an output file outside the repository." >&2
  exit 2
fi
if [[ "$PCI_LIVE_RESULTS" != /* || -e "$PCI_LIVE_RESULTS" || -L "$PCI_LIVE_RESULTS" ]]; then
  echo "PCI_LIVE_RESULTS must be a new absolute path so existing output is never overwritten." >&2
  exit 2
fi
case "$PCI_LIVE_RESULTS" in
  "$root"/*)
    echo "PCI_LIVE_RESULTS must be outside the repository." >&2
    exit 2
    ;;
esac
readonly tools_dir="${PCI_LIVE_TOOLS_DIR:-${TMPDIR:-/tmp}/pci-phase6-tools}"
readonly kubectl_path="$tools_dir/kubectl-$kubectl_version"
if [[ ! -x "$kubectl_path" ]]; then
  echo "Pinned kubectl not found; run setup-kind.sh first." >&2
  exit 2
fi
readonly kubectl=("$kubectl_path" --kubeconfig "$PCI_LIVE_KUBECONFIG" --context "$context_name")
readonly run_id="phase6-$(date -u +%Y%m%d%H%M%S)-$$"

assert_target() {
  kind get clusters | grep -Fxq "$cluster_name" || {
    echo "The dedicated PCI kind cluster '$cluster_name' does not exist." >&2
    return 1
  }
  local server owner recorded_cluster recorded_token expected_token
  server="$("${kubectl[@]}" config view --minify -o jsonpath='{.clusters[0].cluster.server}')" || return 1
  case "$server" in
    https://127.0.0.1:*|https://\[::1\]:*) ;;
    *)
      echo "The dedicated kubeconfig does not point to a loopback kind API server." >&2
      return 1
      ;;
  esac
  owner="$("${kubectl[@]}" -n kube-system get configmap pci-phase6-owner -o jsonpath='{.data.owner}')" || return 1
  recorded_cluster="$("${kubectl[@]}" -n kube-system get configmap pci-phase6-owner -o jsonpath='{.data.cluster}')" || return 1
  recorded_kubernetes="$("${kubectl[@]}" -n kube-system get configmap pci-phase6-owner -o jsonpath='{.data.kubernetes}')" || return 1
  recorded_calico="$("${kubectl[@]}" -n kube-system get configmap pci-phase6-owner -o jsonpath='{.data.calico}')" || return 1
  recorded_token="$("${kubectl[@]}" -n kube-system get configmap pci-phase6-owner -o jsonpath='{.data.ownerToken}')" || return 1
  expected_token="$(cat "${PCI_LIVE_KUBECONFIG}.owner-token")" || return 1
  if [[ "$owner" != "policychangeimpact-phase6" || "$recorded_cluster" != "$cluster_name" ||
    "$recorded_kubernetes" != "$kubernetes_version" || "$recorded_calico" != "$calico_version" ||
    -z "$expected_token" || "$recorded_token" != "$expected_token" ]]; then
    echo "Cluster ownership/version marker does not match; refusing the operation." >&2
    return 1
  fi
  [[ "$("${kubectl[@]}" version -o jsonpath='{.serverVersion.gitVersion}')" == "$kubernetes_version" ]] || {
    echo "Kubernetes server version does not match the pinned test version." >&2
    return 1
  }
}
assert_target || exit 2
if [[ "$("${kubectl[@]}" version --client -o jsonpath='{.clientVersion.gitVersion}')" != "$kubectl_version" ]]; then
  echo "kubectl client version does not match the pinned test version." >&2
  exit 2
fi
calico_ready="$("${kubectl[@]}" -n kube-system get daemonset calico-node -o jsonpath='{.status.numberReady}')"
calico_desired="$("${kubectl[@]}" -n kube-system get daemonset calico-node -o jsonpath='{.status.desiredNumberScheduled}')"
if [[ "$calico_ready" -lt 1 || "$calico_ready" != "$calico_desired" ]]; then
  echo "Calico is not ready; NetworkPolicy enforcement cannot be assumed." >&2
  exit 2
fi
for namespace in "$namespace_source" "$namespace_destination" "$namespace_unrelated"; do
  if "${kubectl[@]}" get namespace "$namespace" >/dev/null 2>&1; then
    echo "Refusing to reuse pre-existing namespace '$namespace'." >&2
    exit 2
  fi
done

readonly temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/pci-phase6-live.XXXXXX")"
mkdir -p "$(dirname "$PCI_LIVE_RESULTS")"
if ! (set -o noclobber; : > "$PCI_LIVE_RESULTS"); then
  echo "Could not exclusively create results file: $PCI_LIVE_RESULTS" >&2
  exit 2
fi
cleanup() {
  if ! assert_target; then
    echo "Refusing namespace cleanup because target identity/ownership is no longer verified." >&2
    rm -rf -- "$temp_dir"
    return
  fi
  for namespace in "$namespace_source" "$namespace_destination" "$namespace_unrelated"; do
    if "${kubectl[@]}" get namespace "$namespace" -l "${owner_label}=${run_id}" -o name 2>/dev/null | grep -q .; then
      if ! assert_target; then
        echo "Target identity changed during cleanup; leaving remaining harness resources untouched." >&2
        break
      fi
      if ! "${kubectl[@]}" delete namespace "$namespace" -l "${owner_label}=${run_id}" --wait=true --timeout=90s >/dev/null; then
        echo "Could not remove owned test namespace '$namespace'; inspect it manually." >&2
      fi
    fi
  done
  rm -rf -- "$temp_dir"
}
trap cleanup EXIT
trap 'POLL_CANCEL_REQUESTED=1' INT TERM

assert_target || exit 2
if ! cat <<EOF | "${kubectl[@]}" create -f -
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace_source}
  labels:
    ${owner_label}: ${run_id}
    pci-phase6-role: source
---
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace_destination}
  labels:
    ${owner_label}: ${run_id}
    pci-phase6-role: destination
---
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace_unrelated}
  labels:
    ${owner_label}: ${run_id}
    pci-phase6-role: unrelated
---
apiVersion: v1
kind: Pod
metadata:
  name: client-a
  namespace: ${namespace_source}
  labels: {app: client, role: client-a}
spec:
  restartPolicy: Never
  containers:
    - name: probe
      image: ${busybox_image}
      command: ["sh", "-c", "sleep 3600"]
---
apiVersion: v1
kind: Pod
metadata:
  name: client-b
  namespace: ${namespace_source}
  labels: {app: client, role: client-b}
spec:
  restartPolicy: Never
  containers:
    - name: probe
      image: ${busybox_image}
      command: ["sh", "-c", "sleep 3600"]
---
apiVersion: v1
kind: Pod
metadata:
  name: outsider
  namespace: ${namespace_unrelated}
  labels: {app: client, role: client-a}
spec:
  restartPolicy: Never
  containers:
    - name: probe
      image: ${busybox_image}
      command: ["sh", "-c", "sleep 3600"]
---
apiVersion: v1
kind: Pod
metadata:
  name: server
  namespace: ${namespace_destination}
  labels: {app: server}
spec:
  restartPolicy: Never
  containers:
    - name: web
      image: ${nginx_image}
      ports:
        - {name: http, containerPort: 80, protocol: TCP}
EOF
then
  record_incomplete_setup "$PCI_LIVE_RESULTS" resource_creation api_create_failed \
    "Kubernetes rejected or could not create the labeled test resources."
  exit 1
fi

for namespace in "$namespace_source" "$namespace_destination" "$namespace_unrelated"; do
  if ! "${kubectl[@]}" -n "$namespace" wait --for=condition=Ready pod --all --timeout=240s; then
    if (( POLL_CANCEL_REQUESTED )); then
      record_incomplete_setup "$PCI_LIVE_RESULTS" "pod_readiness:$namespace" setup_cancelled \
        "Setup was cancelled while waiting for the test Pods to become Ready."
    else
      record_incomplete_setup "$PCI_LIVE_RESULTS" "pod_readiness:$namespace" pod_readiness_timeout \
        "Test Pods did not all become Ready before the bounded 240-second deadline."
    fi
    exit 1
  fi
done
if ! destination_ip="$("${kubectl[@]}" -n "$namespace_destination" get pod server -o jsonpath='{.status.podIP}')"; then
  record_incomplete_setup "$PCI_LIVE_RESULTS" destination_identity destination_lookup_failed \
    "The destination Pod IP could not be retrieved; no probe was attempted."
  exit 1
fi
if [[ -z "$destination_ip" ]]; then
  echo "Destination Pod has no IP; no probe was attempted." >&2
  record_incomplete_setup "$PCI_LIVE_RESULTS" destination_identity destination_ip_missing \
    "The destination Pod has no assigned IP; no probe was attempted."
  exit 1
fi

query_for() {
  local source_namespace="$1" source_pod="$2" role="$3" port="$4"
  python3 - "$source_namespace" "$source_pod" "$role" "$port" > "$temp_dir/query.json" <<'PY'
import json
import sys

namespace, pod, role, port = sys.argv[1:]
source_role = "unrelated" if namespace.endswith("unrelated") else "source"
print(json.dumps({
    "source": {
        "namespace": namespace, "name": pod,
        "labels": {"app": "client", "role": role},
        "labelsComplete": True,
        "namespaceLabels": {"pci-phase6-role": source_role},
        "namespaceLabelsKnown": True,
        "namedPortsComplete": True,
    },
    "destination": {
        "namespace": "pci-phase6-destination", "name": "server",
        "labels": {"app": "server"}, "labelsComplete": True,
        "namespaceLabels": {"pci-phase6-role": "destination"},
        "namespaceLabelsKnown": True, "namedPortsComplete": True,
    },
    "protocol": "TCP", "port": int(port), "policyInventoryComplete": True,
}))
PY
}

run_case() {
  local name="$1" baseline="$2" manifest="$3" source_namespace="$4" source_pod="$5" role="$6" port="$7"
  local expected_before="$8" expected_after="$9" expected_change="${10}"
  local probe_outcome model_before model_after model_change agrees model_info owned_manifest
  local probe_started_at probe_started_ns probe_duration_ms
  if ! assert_target ||
    ! "${kubectl[@]}" -n "$namespace_source" delete networkpolicy -l "pci-phase6-owner=${run_id}" --ignore-not-found >/dev/null; then
    echo "$name: could not clear policies from the harness source namespace." >&2
    return 1
  fi
  if ! assert_target ||
    ! "${kubectl[@]}" -n "$namespace_destination" delete networkpolicy -l "pci-phase6-owner=${run_id}" --ignore-not-found >/dev/null; then
    echo "$name: could not clear policies from the harness destination namespace." >&2
    return 1
  fi
  owned_manifest="$temp_dir/$name-owned.yaml"
  sed "s/PCI_RUN_ID/${run_id}/g" "$manifest" > "$owned_manifest"
  if ! assert_target || ! "${kubectl[@]}" create -f "$owned_manifest" >/dev/null; then
    echo "$name: Kubernetes rejected the scenario policy manifest." >&2
    record_incomplete_setup "$PCI_LIVE_RESULTS" "$name" policy_creation_failed \
      "The policy object was not confirmed by the API; no probe was attempted."
    return 1
  fi
  if ! query_for "$source_namespace" "$source_pod" "$role" "$port" ||
    ! (cd "$root" && go run ./cmd/pci connectivity-diff \
      --baseline "$baseline" --proposed "$owned_manifest" --query "$temp_dir/query.json") > "$temp_dir/model-diff.json"; then
    echo "$name: the shared analyzer could not evaluate the scenario." >&2
    return 1
  fi
  if ! model_info="$(python3 - "$temp_dir/model-diff.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    result = json.load(source)
print(result["before"], result["after"], result["change"])
PY
  )"; then
    echo "$name: the analyzer returned an invalid connectivity diff." >&2
    return 1
  fi
  read -r model_before model_after model_change <<< "$model_info"
  if [[ "$model_before" != "$expected_before" || "$model_after" != "$expected_after" ||
    "$model_change" != "$expected_change" ]]; then
    echo "$name: analyzer diff was $model_before -> $model_after ($model_change), expected $expected_before -> $expected_after ($expected_change)." >&2
    return 1
  fi
  probe_started_at="$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)"
  probe_started_ns="$(date +%s%N)"
  local probe_error_file="$temp_dir/$name-probe-error.txt"
  probe_attempt() {
    local status error_sample protocol="${PROBE_PROTOCOL:-TCP}"
    if [[ "$protocol" != "TCP" ]]; then
      classify_probe_result "$protocol" 0 ""
      return
    fi
    if ! assert_target; then
      POLL_ATTEMPT_OUTCOME="incomplete_setup"
      POLL_ATTEMPT_ERROR_CLASS="target_identity_changed"
      return
    fi
    if timeout --foreground 4s "${kubectl[@]}" -n "$source_namespace" exec "$source_pod" -- nc -z -w 2 "$destination_ip" "$port" >/dev/null 2>"$probe_error_file"; then
      status=0
    else
      status=$?
    fi
    error_sample="$(head -c 2048 "$probe_error_file" | tr '\n' ' ')"
    classify_probe_result "$protocol" "$status" "$error_sample"
  }
  poll_probe "$model_after" probe_attempt
  probe_outcome="$POLL_OUTCOME"
  agrees="$POLL_AGREEMENT"
  probe_error_class="$POLL_ERROR_CLASS"
  probe_attempts="$POLL_ATTEMPTS"
  probe_attempt_outcomes="${POLL_OBSERVATIONS[*]}"
  probe_duration_ms="$((($(date +%s%N) - probe_started_ns) / 1000000))"
  if ! record_probe_result "$PCI_LIVE_RESULTS" "$name" "$model_before" "$model_after" "$model_change" "$probe_outcome" "$agrees" "$probe_error_class" "$probe_attempts" "$probe_attempt_outcomes" \
    "$recorded_kubernetes" "$recorded_calico" "$busybox_image" "$nginx_image" "$context_name" "$source_namespace/$source_pod" \
    "$destination_ip" "$port" "$probe_started_at" "$probe_duration_ms"; then
    echo "$name: failed to write the results record." >&2
    return 1
  fi
  echo "$name: modeledChange=$model_change after=$model_after observed=$probe_outcome attempts=$probe_attempts agreement=$agrees"
  if (( POLL_CANCEL_REQUESTED )); then
    echo "Probe polling was cancelled; stopping before any further scenario." >&2
    exit 130
  fi
  if ! assert_target ||
    ! "${kubectl[@]}" -n "$namespace_source" delete networkpolicy -l "pci-phase6-owner=${run_id}" --ignore-not-found >/dev/null; then
    echo "$name: failed to remove owned source policies." >&2
    return 1
  fi
  if ! assert_target ||
    ! "${kubectl[@]}" -n "$namespace_destination" delete networkpolicy -l "pci-phase6-owner=${run_id}" --ignore-not-found >/dev/null; then
    echo "$name: failed to remove owned destination policies." >&2
    return 1
  fi
  [[ "$agrees" == "true" ]]
}

combine_policies() {
  local output="$1"
  shift
  : > "$output"
  local policy
  for policy in "$@"; do
    if [[ -s "$output" ]]; then
      printf '\n---\n' >> "$output"
    fi
    cat "$policy" >> "$output"
  done
}

readonly policies="$root/tests/live/policies"

overall=0
run_case baseline-no-isolation "$policies/baseline-no-isolation.yaml" "$policies/baseline-no-isolation.yaml" "$namespace_source" client-a client-a 80 allowed_by_model allowed_by_model unchanged || overall=1
run_case ingress-only-isolation "$policies/baseline-no-isolation.yaml" "$policies/ingress-deny.yaml" "$namespace_source" client-a client-a 80 allowed_by_model denied_by_model newly_denied || overall=1
combine_policies "$temp_dir/ingress-additive.yaml" "$policies/ingress-deny.yaml" "$policies/ingress-allow-a.yaml"
run_case ingress-allow-by-combined-selectors "$policies/ingress-deny.yaml" "$temp_dir/ingress-additive.yaml" "$namespace_source" client-a client-a 80 denied_by_model allowed_by_model newly_allowed || overall=1
combine_policies "$temp_dir/ingress-multiple-additive.yaml" "$policies/ingress-deny.yaml" "$policies/ingress-allow-a.yaml" "$policies/ingress-allow-b.yaml"
run_case multiple-policies-additive-client-a "$policies/ingress-deny.yaml" "$temp_dir/ingress-multiple-additive.yaml" "$namespace_source" client-a client-a 80 denied_by_model allowed_by_model newly_allowed || overall=1
run_case multiple-policies-additive-client-b "$policies/ingress-deny.yaml" "$temp_dir/ingress-multiple-additive.yaml" "$namespace_source" client-b client-b 80 denied_by_model allowed_by_model newly_allowed || overall=1
run_case namespace-selector-boundary "$policies/baseline-no-isolation.yaml" "$temp_dir/ingress-additive.yaml" "$namespace_unrelated" outsider client-a 80 allowed_by_model denied_by_model newly_denied || overall=1
run_case egress-only-isolation "$policies/baseline-no-isolation.yaml" "$policies/egress-deny.yaml" "$namespace_source" client-a client-a 80 allowed_by_model denied_by_model newly_denied || overall=1
run_case egress-allow-by-combined-selectors "$policies/baseline-no-isolation.yaml" "$policies/egress-allow.yaml" "$namespace_source" client-a client-a 80 allowed_by_model allowed_by_model unchanged || overall=1
combine_policies "$temp_dir/both-directions-allowed.yaml" "$policies/ingress-deny.yaml" "$policies/ingress-allow-a.yaml" "$policies/egress-deny.yaml" "$policies/egress-allow.yaml"
run_case ingress-and-egress-both-required "$policies/baseline-no-isolation.yaml" "$temp_dir/both-directions-allowed.yaml" "$namespace_source" client-a client-a 80 allowed_by_model allowed_by_model unchanged || overall=1
run_case protocol-port-mismatch "$policies/baseline-no-isolation.yaml" "$policies/ingress-port-mismatch.yaml" "$namespace_source" client-a client-a 80 allowed_by_model denied_by_model newly_denied || overall=1
run_case protocol-mismatch "$policies/baseline-no-isolation.yaml" "$policies/ingress-protocol-mismatch.yaml" "$namespace_source" client-a client-a 80 allowed_by_model denied_by_model newly_denied || overall=1

echo "Results: $PCI_LIVE_RESULTS"
if [[ "$overall" -ne 0 ]]; then
  echo "One or more model/observation outcomes disagreed. Inspect alternatives; no NetworkPolicy causation is asserted." >&2
  exit 1
fi
echo "All executed observations matched the modeled state. This does not establish policy causation or full conformance."
