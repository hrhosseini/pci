# Opt-in live NetworkPolicy validation

The default Go test suite remains offline. The scripts in `scripts/integration`
are an explicitly opt-in experiment against a newly created, disposable `kind`
cluster; they never use the current `kubectl` context or an existing cluster.
The setup script refuses to reuse a cluster named `pci-phase6`, and it requires
both a dedicated, nonexistent kubeconfig path and an explicit confirmation
environment variable.

## Pinned test environment

The harness pins:

- kind `v0.33.0` (the version used when this harness was written)
- Kubernetes node image `kindest/node:v1.31.4`
- `kubectl v1.32.2` (downloaded and checksum-checked against the official
  versioned `dl.k8s.io` checksum endpoint)
- Calico `v3.30.0`, installed as the cluster CNI with the default kind CNI
  disabled. Calico 3.30 documents Kubernetes 1.31 as a tested version.
- BusyBox `1.36.1`, OCI index digest
  `sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662`
- nginx `1.27.4-alpine`, OCI index digest
  `sha256:4ff102c5d78d254a6f0da062b3cf39eaf07f01eec0927fd21e219d0af8bc0591`

The workload references use tag-plus-index-digest image identifiers. The
multi-platform OCI index digest avoids pinning the harness to only the current
host architecture. These digests were read from Docker Hub with
`docker buildx imagetools inspect` on 2026-10-19; they are immutable lock
values, not claims about image security.

To intentionally update either image, inspect its versioned tag with
`docker buildx imagetools inspect <image>:<version-tag>` and record the
top-level `Digest`. Update both the tag and digest in
`scripts/integration/run-live-tests.sh`; verify the digest is the OCI index,
not a single-platform child manifest, and confirm the required utility remains
available. Review and rerun the offline harness tests before any separately
authorized live run. Do not use a moving tag such as `latest`.

The harness verifies its cluster ownership marker, Kubernetes and kubectl
versions, and Calico DaemonSet readiness before any scenario runs. It
revalidates the exact cluster name, loopback API endpoint, version marker, and
owner token immediately before each cluster write, delete, or exec. Setup
creates the owner marker before installing Calico; failed setup only performs
automatic deletion if the marker and token can be verified. If verification
fails, it deliberately leaves the cluster and credentials for manual
inspection. Pin updates should be made deliberately and the pair retested; a
kind cluster without an enforcing CNI is not a valid test.

These pins are the harness's declared target, not versions validated by a live
run. No Kubernetes/CNI version has been tested by this workspace yet.

## Create, test, and remove the disposable cluster

Review the scripts first. Setup requires Docker access and cluster-admin
privileges in the new local cluster to install Calico. The setup path is
required to be new so no existing kubeconfig can be overwritten:

```bash
export PCI_LIVE_KUBECONFIG=/tmp/pci-phase6.kubeconfig
export PCI_LIVE_TEST_CONFIRM=I_CONFIRM_PCI_PHASE6_DISPOSABLE_CLUSTER
./scripts/integration/setup-kind.sh
```

Setup also writes a mode-`0600` owner-token sidecar next to the kubeconfig. Keep
both files together; the random token binds later test and teardown commands to
the cluster created by this setup run.

Run scenarios with a separate, new results file:

```bash
export PCI_LIVE_RESULTS=/tmp/pci-phase6-results.jsonl
./scripts/integration/run-live-tests.sh
```

The script uses only the dedicated kubeconfig and the fixed `kind-pci-phase6`
context. It refuses any cluster without the harness ownership/version marker,
checks the CNI is ready, and refuses pre-existing test namespaces. It uses
`create`, not `apply`, for test resources; policy cleanup selects only policies
with this run's owner label, and namespace cleanup requires the matching
per-run namespace label. It never deletes all policies in a namespace. On
completion or failure, cleanup revalidates the target and leaves resources in
place if ownership can no longer be verified. The output path is exclusively
created so an existing report is never truncated.

Pod readiness uses `kubectl wait` with a bounded 240-second deadline. After
each policy is accepted by the Kubernetes API, PCI polls the fixed TCP probe
up to three times (each attempt capped at four seconds, 0.5 seconds between
attempts) and requires two consecutive identical `success` or
`connection_failure` outcomes for a stable observation. A timeout, cancelled
poll, unsupported method/utility, unavailable source workload/exec permission,
or unstable sequence is represented distinctly; setup failures create an
`incomplete_setup` JSONL event rather than an invented connectivity result.
Kubernetes NetworkPolicy has no standard status condition acknowledging CNI
dataplane convergence, so API object visibility is not treated as proof that
the dataplane has converged. Repeated failures remain observations, never
proof of NetworkPolicy causation.

For the current policy fixture, the offline UDP-rule/TCP-flow test validates
protocol-specific rule matching rather than UDP socket behavior: the target
is selected by an ingress policy, but its only ingress allow rule names UDP,
so that rule cannot allow the TCP flow. The fixture and flow explicitly use
UDP and TCP respectively; there is no unknown-protocol defaulting involved.

JSONL records include modeled status, observation, timestamp, duration, flow,
cluster/context, Kubernetes/CNI versions and test image tag-plus-digest
references, comparison state, sanitized probe error classification,
per-attempt outcome sequence, and plausible alternative
explanations. `incomplete_setup`,
`unsupported`, `timeout`, and `inconclusive` are not counted as agreement. A
match is not proof of policy causation; a failed probe is never reported as a
verified policy denial.

Delete the dedicated cluster only after reviewing the marker and results:

```bash
export PCI_LIVE_DELETE_CONFIRM=I_CONFIRM_DELETE_PCI_PHASE6_CLUSTER
./scripts/integration/teardown-kind.sh
```

Teardown checks the dedicated context, owner ConfigMap, cluster name, and
cluster existence before deleting only the harness-created `pci-phase6` kind
cluster and its kubeconfig. Do not repurpose this cluster for other workloads.

## Use PCI locally with an explicit development context

Offline analysis has no Kubernetes dependency:

```bash
go run ./cmd/pci analyze --path ./fixtures/base
go run ./cmd/pci diff --base ./fixtures/base --proposed ./fixtures/proposed
```

For an intentional read-only discovery against a development cluster, choose
its kubeconfig and context explicitly. Namespace scope is the default:

```bash
go run ./cmd/pci \
  --kubeconfig "$HOME/.kube/dev-config" \
  --kube-context dev-context \
  --namespace pci-dev \
  discover
```

Use `--all-namespaces` only when cluster-wide collection is intended and
authorized. To run the API/UI locally against the same explicit context:

```bash
PCI_KUBECONFIG="$HOME/.kube/dev-config" \
PCI_KUBE_CONTEXT=dev-context \
PCI_NAMESPACE=pci-dev \
go run ./cmd/pci-server
```

The server binds to `127.0.0.1:8080` by default. The web development server is
started from `web/` with `npm run dev`; it calls that local API by default.
`GET http://127.0.0.1:8080/api/v1/clusters/dev/discover` performs a fresh,
scoped read using the selected context; it does not silently substitute
fixture or cached resources. PCI uses the caller's existing Kubernetes
credentials and does not persist the kubeconfig. The API is unauthenticated,
so keep it local and do not expose it to an untrusted network.

## Scenarios and interpretation

The script runs each fixed probe from an already-running BusyBox test Pod to the
nginx Pod IP over TCP/80. The same proposed NetworkPolicy YAML passed to
Kubernetes is fed to `pci connectivity-diff` (alongside a checked-in baseline),
with complete fixture endpoint labels and namespace labels. Scenarios cover:

- No applicable isolation (baseline allow).
- Ingress-only isolation and an additive ingress allow.
- Two independent additive policies, exercised by separate labeled source
  Pods.
- Namespace-selector boundary behavior.
- Egress-only isolation and a matching egress allow.
- Ingress and egress isolation both needing to permit the flow.
- TCP port and protocol mismatches.

The protocol-mismatch case is specifically an ingress-isolated destination
with a UDP/80 allow rule and a modeled/probed TCP/80 flow. Under standard
NetworkPolicy semantics the UDP rule does not match the TCP flow, so the
selected Pod remains ingress-isolated and the modeled result is denied. The
live probe is still TCP; this case does not claim to exercise UDP transport.

The script records `success`, `connection_failure`, `timeout`, `unsupported`,
`incomplete_setup`, or `inconclusive`, and compares only stable
success/failure observations with the shared analyzer's `allowed_by_model` or
`denied_by_model`. A disagreement is recorded, all scenarios continue where
possible, and the script exits nonzero. Unsupported/incomplete/unstable
observations leave model agreement inconclusive. The output lists plausible
CNI, routing, host-firewall, workload-readiness, and probe-timeout
alternatives. Neither matching nor disagreement identifies NetworkPolicy as
the cause.
Each case probes only its proposed/live state; `baselineModeled` and
`modeledChange` are analyzer counterfactuals, not a claim that the prior model
state was separately probed.

Incomplete policy inventory and absent endpoint/namespace labels are
information-quality conditions, not safely reproducible live packet scenarios;
their uncertainty propagation is covered by offline analyzer tests. Staleness
is not currently detected by the analyzer: callers can still supply old data
as complete, so stale inputs remain a false-positive risk. Service/NAT
rewriting, non-TCP behavior, other CNIs, CNI-specific policy, node traffic, DNS,
service meshes, and production workloads are outside this harness. Results do
not establish general Kubernetes conformance or production readiness.
Polling improves repeatability but cannot guarantee dataplane convergence. A
stable failure can still result from routing, an unavailable endpoint, host
firewall, probe utility, or another environmental cause.

Offline safety guards can be checked without Docker or cluster access:

```bash
bash -n scripts/integration/setup-kind.sh scripts/integration/run-live-tests.sh \
  scripts/integration/teardown-kind.sh scripts/integration/test-safety.sh
bash scripts/integration/test-safety.sh
```

This fake-command suite checks selected preflight/ownership refusals, partial
setup reporting, polling stability, timeout, cancellation, and inconclusive
outcomes; it does not establish that a live cluster is disposable.
The existing local cluster named `kind` is never an implicit target. Live use
requires separate explicit authorization for the new `pci-phase6` cluster,
context `kind-pci-phase6`, and the stated Kubernetes/Calico versions.

## Runtime-verification boundary

PCI still has no production runtime-probe provider. The API reports verification
as disabled by default, and the deterministic fake provider is test-only.
Installing a provider based on Kubernetes `pods/exec` is not a safe default:
RBAC for `pods/exec` effectively permits command execution in the selected Pod,
even if PCI's own API exposes only a fixed probe. PCI does not request or
automatically grant that permission. The live test script's `kubectl exec` is a
fixed, operator-run command against disposable test Pods, not an API feature.

Discovery in a real deployment remains read-only. It needs only scoped
`get`/`list` access to the supported core/apps/networking resources documented
in [SECURITY.md](./SECURITY.md); it does not need `pods/exec` or write verbs.
Keep the API bound to a trusted local interface: API authentication is not
implemented.
