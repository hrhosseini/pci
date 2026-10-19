# Security

## Current posture
The project currently operates in read-only offline analysis mode by default. This reduces the risk of accidental cluster mutation while still enabling static policy evaluation.

## Security controls present
- No secrets are hardcoded into sample manifests or docs.
- API errors are structured and redact stack traces.
- Server timeouts are configured for read operations.
- The CLI emits machine-readable output to stdout and keeps diagnostics on stderr where appropriate.
- The default server bind address is loopback on local development.

## Missing or future protections
- Authentication and authorization for the API and UI.
- CSRF protection and safe CORS configuration.
- Database credential and secret management.
- Real RBAC enforcement for cluster access.
- Write-enabled mutation controls with explicit approval requirements.
- Safe handling for cluster access tokens and kubeconfig files.

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
- Read-only discovery is explicitly scoped: namespace mode uses `get` on the selected Namespace and `list` on Pods, NetworkPolicies, Deployments, DaemonSets, StatefulSets, Jobs, and CronJobs in that namespace. All-namespaces mode additionally lists Namespaces and the same resource kinds cluster-wide. Discovery uses paginated reads and marks any failed collection incomplete. PCI does not currently ship an RBAC manifest; operators must grant only the applicable scoped read verbs and must not grant write verbs.

## Verification limitations
The provider interface and deterministic synthetic provider exist for tests only. The synthetic provider is not installed by the server or CLI and always marks evidence as `provenance: synthetic`. There is no live workload execution adapter. In particular, PCI does not invoke `kubectl exec`, submit arbitrary commands, create probe Pods, or alter cluster resources. Configure no provider unless a future adapter has explicit authorization, read-only scope, output bounds, auditability, and cancellation tests.

Observation states are `observed_success`, `observed_failure`, or `unknown`. A successful TCP/UDP connection shows only that a connection succeeded from that source under the tested conditions. A failed connection does not prove NetworkPolicy caused the failure. PCI reserves no policy-causation claim based on a connectivity attempt alone.
