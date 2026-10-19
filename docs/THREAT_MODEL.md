# Threat Model

This project models Kubernetes NetworkPolicy changes and helps operators reason about the impact of policy modifications. The primary risk is incorrect change assessment causing either overly broad exposure or accidental disruption to required workloads.

## Assets
- Kubernetes manifests and policy intent.
- Cluster metadata and workload relationships.
- Operator decisions and approvals.
- Evidence records that support or refute predictions.
- API availability and credentials.

## Threats
- Malformed or ambiguous NetworkPolicy manifests.
- Broad pod selectors unintentionally selecting many workloads.
- Incorrectly assuming a missing rule equals unrestricted traffic.
- Confusing modeled policy results with runtime observations.
- Unauthorized mutation of cluster state.
- Exposure through unvalidated change approval workflows.

## Controls
- Strong typing and deterministic normalization for policy models.
- Structured findings that explicitly separate modeled, observed, and verified traffic states.
- Requirement for explicit approval before apply or verify transitions.
- Read-only default mode and least-privilege semantics in the change-management flow.
- Evidence provenance metadata and warnings for unsupported conclusions.
- CLI and API validation to prevent unsafe or ambiguous input.

## Assumptions and limitations
- The analyzer models NetworkPolicy semantics and known metadata; it does not replace runtime testing.
- CNI differences and DNS/service behavior may affect actual connectivity beyond the static model.
- The frontend and CLI share the same analysis engine to minimize drift.
- Production authorization and RBAC gating must be enforced at the deployment layer.
