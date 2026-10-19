# API Reference

## Base path
`/api/v1`

## Implemented endpoints

- `GET /api/v1/health` — health response
- `GET /api/v1/version` — returns version and capabilities
- `GET /api/v1/capabilities` — capability discovery
- `GET /api/v1/clusters/{clusterId}` — cluster context metadata
- `GET /api/v1/clusters/{clusterId}/pods` — read-only Pod discovery (requires cluster configuration)
- `GET /api/v1/clusters/{clusterId}/network-policies` — read-only NetworkPolicy discovery (requires cluster configuration)
- `GET /api/v1/clusters/{clusterId}/discover` — namespace, Pod/workload, and policy discovery
- `GET /api/v1/clusters/{clusterId}/capabilities` — cluster capability list
- `GET /api/v1/analyses` — list analysis entries
- `POST /api/v1/analyses` — analyze a directory or manifest path
- `GET /api/v1/analyses/{analysisId}` — fetch an analysis item
- `GET /api/v1/analyses/{analysisId}/findings` — fetch findings
- `GET /api/v1/analyses/{analysisId}/graph` — fetch graph summary
- `GET /api/v1/analyses/{analysisId}/explain/{findingId}` — fetch structured explanation
- `POST /api/v1/snapshots` — create a snapshot from a path
- `GET /api/v1/snapshots` — list snapshots
- `GET /api/v1/snapshots/{snapshotId}` — read snapshot
- `POST /api/v1/snapshots/compare` — compare two inputs
- `POST /api/v1/connectivity` — evaluate a supplied flow against a supplied policy model; incomplete inputs produce `unknown`
- `POST /api/v1/connectivity/diff` — compare a flow's modeled before/after state across two policy inventories
- `GET /api/v1/verifications` — list persisted evidence records and provider state
- `POST /api/v1/verifications` — validate and dispatch a constrained probe request only when a provider is explicitly configured; returns `503 VERIFICATION_DISABLED` otherwise
- `GET /api/v1/verifications/{evidenceId}` — retrieve persisted observation evidence
- `POST /api/v1/reports` — export an analysis report

## Connectivity input
`POST /connectivity` requires a policy YAML/JSON stream and a query with source/destination namespace/name, endpoint labels and label-completeness flags, protocol, port, and `policyInventoryComplete`. The inventory flag must be true before PCI reports default allow from absence of isolating policies. Results include separate ingress/egress decisions and `provenance: modeled`.

Set `hostNetwork: true` for a host-network Pod to receive `unknown`. `ipBlock` peers only produce a CIDR result when the relevant endpoint includes `ipBlockAddressKnown: true`, asserting the enforcement-point address and that the node exception does not apply; PCI does not independently verify that assertion.

`POST /connectivity/diff` returns complete before/after modeled results, including the source and destination IDs, direction-specific applicable/evaluated/matched rule IDs, assumptions, and unsupported inputs. `policyChanges` identifies added, removed, and semantically changed policies with changed fields and normalized before/after JSON values; it is evidence for review, not a claim that a single change caused runtime traffic to fail or succeed.

`GET /clusters/{clusterId}/discover` includes `complete`, `incomplete`, and `collectedAt`. A result with `complete: false` must not be represented to the flow evaluator as a complete inventory. Discovery follows API pagination, records namespace labels and Pod-template labels, and explicitly reports host-network workloads. It currently returns NetworkPolicy summaries rather than the complete rule specs required to construct a flow query.

Path-based policy analysis fails on malformed or duplicate-identity NetworkPolicy documents instead of silently treating them as absent. Policy diffs compare normalized policy specs, including rule contents, and include changed fields, exact normalized before/after values, and a semantic fingerprint in each modification.

## Verification status
The production application has no runtime probe provider. The API and CLI do not fabricate evidence. The deterministic fake provider is only used in automated tests and its records are marked synthetic. Any future provider must use an existing explicitly selected workload, never accept shell commands, honor timeouts/cancellation, enforce authorization, and retain provenance.

The API remains local/offline-first and does not claim complete Kubernetes conformance, runtime NetworkPolicy causation, or production-grade authentication.
