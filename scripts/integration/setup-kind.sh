#!/usr/bin/env bash
set -euo pipefail
umask 077

readonly cluster_name="pci-phase6"
readonly context_name="kind-pci-phase6"
readonly kubernetes_version="v1.31.4"
readonly kubectl_version="v1.32.2"
readonly calico_version="v3.30.0"
readonly confirmation="I_CONFIRM_PCI_PHASE6_DISPOSABLE_CLUSTER"

if [[ "$(kind version | awk 'NR == 1 { print $2 }')" != "v0.33.0" ]]; then
  echo "This harness is pinned to kind v0.33.0." >&2
  exit 2
fi
if [[ "${PCI_LIVE_TEST_CONFIRM:-}" != "$confirmation" ]]; then
  echo "Refusing to create a cluster. Set PCI_LIVE_TEST_CONFIRM=$confirmation after reviewing the integration-test documentation." >&2
  exit 2
fi
if [[ -z "${PCI_LIVE_KUBECONFIG:-}" || "$PCI_LIVE_KUBECONFIG" != /* ]]; then
  echo "Set PCI_LIVE_KUBECONFIG to a new absolute path; the file must not already exist." >&2
  exit 2
fi
if [[ -e "$PCI_LIVE_KUBECONFIG" ]]; then
  echo "Refusing to overwrite existing kubeconfig: $PCI_LIVE_KUBECONFIG" >&2
  exit 2
fi
if [[ -L "$PCI_LIVE_KUBECONFIG" ]]; then
  echo "Refusing to use a symbolic-link kubeconfig path." >&2
  exit 2
fi
readonly owner_token_file="${PCI_LIVE_KUBECONFIG}.owner-token"
if [[ -e "$owner_token_file" || -L "$owner_token_file" ]]; then
  echo "Refusing to overwrite existing owner-token file: $owner_token_file" >&2
  exit 2
fi
if kind get clusters | grep -Fxq "$cluster_name"; then
  echo "Refusing to reuse existing kind cluster '$cluster_name'; inspect it and remove it explicitly before setup." >&2
  exit 2
fi
if ! command -v curl >/dev/null || ! command -v sha256sum >/dev/null; then
  echo "curl and sha256sum are required." >&2
  exit 2
fi

readonly tools_dir="${PCI_LIVE_TOOLS_DIR:-${TMPDIR:-/tmp}/pci-phase6-tools}"
install -d -m 700 "$tools_dir"
readonly kubectl_path="$tools_dir/kubectl-$kubectl_version"
if [[ ! -x "$kubectl_path" ]]; then
  archive="$(mktemp "$tools_dir/kubectl.XXXXXX")"
  checksum="$(mktemp "$tools_dir/kubectl.sha256.XXXXXX")"
  trap 'rm -f -- "$archive" "$checksum"' EXIT
  curl --fail --silent --show-error --location \
    "https://dl.k8s.io/release/${kubectl_version}/bin/linux/amd64/kubectl" -o "$archive"
  curl --fail --silent --show-error --location \
    "https://dl.k8s.io/release/${kubectl_version}/bin/linux/amd64/kubectl.sha256" -o "$checksum"
  printf '%s  %s\n' "$(tr -d '[:space:]' < "$checksum")" "$archive" | sha256sum --check --status
  install -m 700 "$archive" "$kubectl_path"
  rm -f -- "$archive" "$checksum"
  trap - EXIT
fi
checksum="$(mktemp "$tools_dir/kubectl.sha256.XXXXXX")"
trap 'rm -f -- "$checksum"' EXIT
curl --fail --silent --show-error --location \
  "https://dl.k8s.io/release/${kubectl_version}/bin/linux/amd64/kubectl.sha256" -o "$checksum"
printf '%s  %s\n' "$(tr -d '[:space:]' < "$checksum")" "$kubectl_path" | sha256sum --check --status
rm -f -- "$checksum"
trap - EXIT
if [[ "$("$kubectl_path" version --client -o jsonpath='{.clientVersion.gitVersion}')" != "$kubectl_version" ]]; then
  echo "Downloaded kubectl version does not match the pinned release." >&2
  exit 2
fi

mkdir -p "$(dirname "$PCI_LIVE_KUBECONFIG")"
owner_token="$(od -An -N32 -tx1 /dev/urandom | tr -d '[:space:]')"
(set -o noclobber; : > "$owner_token_file")
chmod 600 "$owner_token_file"
printf '%s\n' "$owner_token" > "$owner_token_file"
cluster_creation_started=0
cleanup_setup() {
  local status=$?
  if [[ -n "${calico_manifest:-}" ]]; then
    rm -f -- "$calico_manifest"
  fi
  if [[ "$status" -ne 0 && "$cluster_creation_started" -eq 1 ]]; then
    if setup_cluster_is_owned; then
      if kind delete cluster --name "$cluster_name" --kubeconfig "$PCI_LIVE_KUBECONFIG"; then
        rm -f -- "$PCI_LIVE_KUBECONFIG" "$owner_token_file"
      else
        echo "Automatic cleanup failed; retain kubeconfig and ownership token for explicit teardown." >&2
      fi
    else
      echo "Refusing automatic cleanup because cluster ownership could not be verified. Retain the kubeconfig/token and inspect the dedicated kind cluster." >&2
    fi
  fi
}
setup_cluster_is_owned() {
  [[ -f "$PCI_LIVE_KUBECONFIG" && ! -L "$PCI_LIVE_KUBECONFIG" ]] || return 1
  kind get clusters | grep -Fxq "$cluster_name" || return 1
  local server owner recorded_cluster recorded_token expected_token
  server="$("$kubectl_path" --kubeconfig "$PCI_LIVE_KUBECONFIG" --context "$context_name" \
    config view --minify -o jsonpath='{.clusters[0].cluster.server}')" || return 1
  case "$server" in
    https://127.0.0.1:*|https://\[::1\]:*) ;;
    *) return 1 ;;
  esac
  owner="$("$kubectl_path" --kubeconfig "$PCI_LIVE_KUBECONFIG" --context "$context_name" \
    -n kube-system get configmap pci-phase6-owner -o jsonpath='{.data.owner}')" || return 1
  recorded_cluster="$("$kubectl_path" --kubeconfig "$PCI_LIVE_KUBECONFIG" --context "$context_name" \
    -n kube-system get configmap pci-phase6-owner -o jsonpath='{.data.cluster}')" || return 1
  recorded_token="$("$kubectl_path" --kubeconfig "$PCI_LIVE_KUBECONFIG" --context "$context_name" \
    -n kube-system get configmap pci-phase6-owner -o jsonpath='{.data.ownerToken}')" || return 1
  expected_token="$(cat "$owner_token_file")" || return 1
  [[ "$owner" == "policychangeimpact-phase6" && "$recorded_cluster" == "$cluster_name" &&
    -n "$expected_token" && "$recorded_token" == "$expected_token" ]]
}
trap cleanup_setup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cluster_creation_started=1
kind create cluster \
  --name "$cluster_name" \
  --image "kindest/node:${kubernetes_version}" \
  --config "$(dirname "$0")/kind-no-default-cni.yaml" \
  --kubeconfig "$PCI_LIVE_KUBECONFIG" \
  --wait 180s

server_url="$("$kubectl_path" --kubeconfig "$PCI_LIVE_KUBECONFIG" --context "$context_name" \
  config view --minify -o jsonpath='{.clusters[0].cluster.server}')"
case "$server_url" in
  https://127.0.0.1:*|https://\[::1\]:*) ;;
  *)
    echo "New kind kubeconfig did not resolve to a loopback API server." >&2
    exit 1
    ;;
esac
if kind get clusters | grep -Fxq "$cluster_name"; then
  :
else
  echo "The newly created kind cluster is not present under its exact name." >&2
  exit 1
fi
if [[ "$("$kubectl_path" --kubeconfig "$PCI_LIVE_KUBECONFIG" --context "$context_name" \
  version -o jsonpath='{.serverVersion.gitVersion}')" != "$kubernetes_version" ]]; then
  echo "The new cluster server version does not match the pinned release." >&2
  exit 1
fi
"$kubectl_path" --kubeconfig "$PCI_LIVE_KUBECONFIG" --context "$context_name" \
  -n kube-system create configmap pci-phase6-owner \
  --from-literal=owner=policychangeimpact-phase6 \
  --from-literal=ownerToken="$owner_token" \
  --from-literal=cluster="$cluster_name" \
  --from-literal=kubernetes="$kubernetes_version" \
  --from-literal=calico="$calico_version"
calico_manifest="$(mktemp "${TMPDIR:-/tmp}/pci-calico-${calico_version}.XXXXXX.yaml")"
curl --fail --silent --show-error --location \
  "https://raw.githubusercontent.com/projectcalico/calico/${calico_version}/manifests/calico.yaml" \
  -o "$calico_manifest"
setup_cluster_is_owned || {
  echo "Cluster identity or ownership changed before Calico installation." >&2
  exit 1
}
"$kubectl_path" --kubeconfig "$PCI_LIVE_KUBECONFIG" --context "$context_name" apply -f "$calico_manifest"
setup_cluster_is_owned || {
  echo "Cluster identity or ownership changed before Calico readiness checks." >&2
  exit 1
}
"$kubectl_path" --kubeconfig "$PCI_LIVE_KUBECONFIG" --context "$context_name" \
  -n kube-system rollout status daemonset/calico-node --timeout=240s
"$kubectl_path" --kubeconfig "$PCI_LIVE_KUBECONFIG" --context "$context_name" \
  -n kube-system rollout status deployment/calico-kube-controllers --timeout=240s
echo "Disposable cluster ready."
echo "  kubeconfig: $PCI_LIVE_KUBECONFIG"
echo "  kubectl:    $kubectl_path"
echo "  context:    $context_name"
echo "  ownership token: $owner_token_file (keep this file with the kubeconfig)"
echo "  Kubernetes: $kubernetes_version"
echo "  Calico:     $calico_version (NetworkPolicy-enforcing CNI)"
echo "Do not use this cluster for workloads outside the PCI integration tests."
