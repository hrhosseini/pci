#!/usr/bin/env bash
set -euo pipefail

readonly cluster_name="pci-phase6"
readonly context_name="kind-pci-phase6"
readonly confirmation="I_CONFIRM_DELETE_PCI_PHASE6_CLUSTER"
readonly expected_owner="policychangeimpact-phase6"

if [[ "${PCI_LIVE_DELETE_CONFIRM:-}" != "$confirmation" ]]; then
  echo "Refusing cluster deletion. Set PCI_LIVE_DELETE_CONFIRM=$confirmation only after verifying this is the PCI disposable cluster." >&2
  exit 2
fi
if [[ -z "${PCI_LIVE_KUBECONFIG:-}" || ! -f "$PCI_LIVE_KUBECONFIG" || -L "$PCI_LIVE_KUBECONFIG" ]]; then
  echo "Set PCI_LIVE_KUBECONFIG to the dedicated PCI kubeconfig." >&2
  exit 2
fi
if [[ ! -f "${PCI_LIVE_KUBECONFIG}.owner-token" || -L "${PCI_LIVE_KUBECONFIG}.owner-token" ]]; then
  echo "The setup ownership token is missing; refusing to delete this cluster." >&2
  exit 2
fi
readonly tools_dir="${PCI_LIVE_TOOLS_DIR:-${TMPDIR:-/tmp}/pci-phase6-tools}"
readonly kubectl_path="$tools_dir/kubectl-v1.32.2"
if [[ ! -x "$kubectl_path" ]]; then
  echo "Pinned kubectl not found." >&2
  exit 2
fi
verify_owned_target() {
  local owner server_url recorded_cluster recorded_owner_token expected_owner_token
  owner="$("$kubectl_path" --kubeconfig "$PCI_LIVE_KUBECONFIG" --context "$context_name" \
    -n kube-system get configmap pci-phase6-owner -o jsonpath='{.data.owner}')" || return 1
  server_url="$("$kubectl_path" --kubeconfig "$PCI_LIVE_KUBECONFIG" --context "$context_name" \
    config view --minify -o jsonpath='{.clusters[0].cluster.server}')" || return 1
  recorded_cluster="$("$kubectl_path" --kubeconfig "$PCI_LIVE_KUBECONFIG" --context "$context_name" \
    -n kube-system get configmap pci-phase6-owner -o jsonpath='{.data.cluster}')" || return 1
  recorded_owner_token="$("$kubectl_path" --kubeconfig "$PCI_LIVE_KUBECONFIG" --context "$context_name" \
    -n kube-system get configmap pci-phase6-owner -o jsonpath='{.data.ownerToken}')" || return 1
  expected_owner_token="$(cat "${PCI_LIVE_KUBECONFIG}.owner-token")" || return 1
  case "$server_url" in
    https://127.0.0.1:*|https://\[::1\]:*) ;;
    *)
      echo "The dedicated kubeconfig does not point to a loopback kind API server." >&2
      return 1
      ;;
  esac
  if [[ "$owner" != "$expected_owner" || "$recorded_cluster" != "$cluster_name" ||
    -z "$expected_owner_token" || "$recorded_owner_token" != "$expected_owner_token" ]]; then
    echo "Ownership marker mismatch; refusing deletion." >&2
    return 1
  fi
  kind get clusters | grep -Fxq "$cluster_name" || {
    echo "The marked cluster '$cluster_name' was not found." >&2
    return 1
  }
}
verify_owned_target || exit 2
verify_owned_target || {
  echo "Target identity changed before deletion; refusing teardown." >&2
  exit 2
}
kind delete cluster --name "$cluster_name" --kubeconfig "$PCI_LIVE_KUBECONFIG"
rm -f -- "$PCI_LIVE_KUBECONFIG"
rm -f -- "${PCI_LIVE_KUBECONFIG}.owner-token"
echo "Deleted only the PCI-created '$cluster_name' disposable cluster and its kubeconfig."
