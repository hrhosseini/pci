# Security

## Current posture
The project currently operates in read-only offline analysis mode by default. This reduces the risk of accidental cluster mutation while still enabling static policy evaluation.

## Security controls present
- No secrets are hardcoded into sample manifests or docs.
- API errors are structured and redact stack traces.
- Server timeouts are configured for read operations.
- Request JSON bodies are bounded (`MaxBody`, default 1 MiB).
- The CLI emits machine-readable output to stdout and keeps diagnostics on stderr where appropriate.
- The default server bind address is loopback (`127.0.0.1`). `Serve` refuses an empty address (which would become `:port` and listen on all interfaces) and refuses non-loopback addresses unless `PCI_ALLOW_NON_LOOPBACK=1` or `true` is set. This is a deployment guard for an unauthenticated API, not authentication.
- The local SQLite store opens with a single connection and a 5-second busy timeout, creates its directory mode `0700`, and sets the database (and WAL/SHM sidecar) files to mode `0600` when present on platforms where `os.Chmod` enforces Unix permission bits. On Windows, these mode bits are best-effort and do not provide equivalent ACL secrecy; treat the DB path as sensitive regardless of OS. File modes are not a substitute for encrypting the filesystem or authenticating API callers.

## Missing or future protections
- Authentication and authorization for the API and UI. The API remains unauthenticated. Loopback-by-default plus `PCI_ALLOW_NON_LOOPBACK` only reduces accidental exposure; they do not authorize remote callers.
- CSRF protection and safe CORS configuration.
- Database credential and secret management beyond local file-mode restrictions.
- Real RBAC enforcement for cluster access (operators must grant only scoped read verbs for discovery).
- Write-enabled mutation controls with explicit approval requirements. Controlled apply is not implemented.
- Safe handling for cluster access tokens and kubeconfig files outside the disposable live harness path.

## Operational guidance
- Treat all cluster-connected features as requiring explicit environment configuration and least-privilege RBAC.
- Do not store kubeconfig content in browser storage or local JS state.
- Keep verification targets constrained and approved; do not allow broad scanning by default.
- Treat all runtime flow evidence as non-authoritative until it has a documented source and the relevant environment metadata.
- Runtime verification is disabled by default. No production probe provider is configured in this build.
- Verification requests are constrained to a named source workload, an IP/DNS destination, TCP or UDP, a numeric port, and a timeout no greater than 10 seconds. API callers cannot submit shell commands.
- Provider execution is bounded to two concurrent API requests; request cancellation and timeout are propagated.
- API errors from providers are redacted. Authorization failures and timeouts have separate structured status codes.
- Evidence is stored separately in SQLite with source identity, destination, protocol/port, timestamp, duration, outcome, method, provenance, and limitations.
- Verification POST requests may supply an `Idempotency-Key`; completed retries with identical request content return the original persisted evidence, while conflicting content is rejected. A request reserved before provider execution is not automatically retried if its completion is ambiguous, preventing accidental duplicate probes. Use a new key only when intentionally initiating a new attempt.
- Evidence IDs are random and collision-safe at the SQLite constraint boundary. Duplicate IDs fail rather than replacing prior evidence; malformed persisted evidence is returned as a structured server error instead of a success-shaped invalid JSON response.
- Read-only discovery is explicitly scoped: namespace mode uses `get` on the selected Namespace and `list` on Pods, NetworkPolicies, Deployments, DaemonSets, StatefulSets, Jobs, and CronJobs in that namespace. All-namespaces mode additionally lists Namespaces and the same resource kinds cluster-wide. Discovery uses paginated reads and marks any failed collection incomplete. PCI does not currently ship an RBAC manifest; operators must grant only the applicable scoped read verbs and must not grant write verbs.

## Verification limitations
The provider interface and deterministic synthetic provider exist for tests only. The synthetic provider is not installed by the server or CLI and always marks evidence as `provenance: synthetic`. There is no live workload execution adapter. In particular, PCI does not invoke `kubectl exec`, submit arbitrary commands, create probe Pods, or alter cluster resources. Configure no provider unless a future adapter has explicit authorization, read-only scope, output bounds, auditability, and cancellation tests.

Observation states are `observed_success`, `observed_failure`, or `unknown`. A successful TCP/UDP connection shows only that a connection succeeded from that source under the tested conditions. A failed connection does not prove NetworkPolicy caused the failure. PCI reserves no policy-causation claim based on a connectivity attempt alone.

Verification evidence also retains the configured timeout and can refer to an
existing analysis and policy snapshot. Discovery completeness, collection
timestamp, cluster/context, and CNI are optional provider evidence and remain
omitted when not known; PCI does not infer them. A reference supplied to the
verification API must identify an existing persisted analysis or snapshot.

The API itself does not currently authenticate callers. Do not expose it on an
untrusted network. No live provider is installed, and PCI does not request the
powerful Kubernetes `pods/exec` permission. A future provider requiring that
permission needs a separate authorization and threat-model review; the fixed
probe API must not be treated as reducing the principal's underlying exec
capability.

Idempotency reservations intentionally remain `in_progress` if the server
stops or persistence fails after a request is reserved. This is an at-most-once
safety tradeoff: the system cannot determine whether a provider operation
already happened. Repeating the same key receives a conflict; operators should
not delete or reset the reservation unless they have established that no
provider attempt occurred.

The reproducible opt-in kind/Calico setup and teardown, exact confirmation
values, privileges, scenarios, and interpretation limits are documented in
[LIVE_VALIDATION.md](./LIVE_VALIDATION.md).
