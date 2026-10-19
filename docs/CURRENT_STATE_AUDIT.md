# Current State Audit

## Scope
This audit reflects the code checked into the repository at the current point in the project lifecycle. It is intentionally conservative: it reports only behavior confirmed by reading source files and executing the relevant Go and frontend checks.

## 1. Existing functionality

Implemented and tested:
- Offline NetworkPolicy parsing for YAML/JSON files through the Kubernetes API types.
- Semantic diff between a baseline and proposed directory or file set.
- Structured analyzer output containing policy summaries and findings.
- Built-in CLI commands for analyze, diff, explain, graph, report, version, doctor, and serve.
- A minimal React dashboard that renders mock structured data and a simple graph overview.
- SQLite-ready persistence layer with schema initialization for analysis records.

Implemented but insufficiently tested or incomplete:
- API server endpoints for health, version, capabilities, analysis, snapshots, verification acceptance, and report generation.
- CLI snapshot and compare commands that are intentionally minimal and offline-only.
- Structured explanation model for findings.
- Graph serialization for modeled connectivity.

Partially implemented:
- End-to-end backend + frontend integration.
- Write-enabled change lifecycle and Kubernetes integration.
- Runtime verification and evidence provenance.
- Policy-review workflow and graph interactions.

Not implemented:
- Real Kubernetes client-go discovery and RBAC-aware cluster integration.
- Full NetworkPolicy semantics for every selector, peer, and port edge case.
- Real CNI adapter or runtime flow evidence ingestion.
- Authenticated mutation approval with server enforcement.
- Actual DB migrations, audit retention, and durable lifecycle persistence beyond the local SQLite-ready scaffolding.
- Browser-side graph controls such as zoom/pan, fit-to-view, or filtering.

Blocked by unavailable infrastructure:
- Real cluster tests, live NetworkPolicy verification, and write-enabled K8s mutation checks.

## 2. Actual CLI command coverage

Implemented and working in this repository:
- `pci analyze`
- `pci diff`
- `pci explain`
- `pci graph`
- `pci report`
- `pci doctor`
- `pci serve`
- `pci version`
- `pci completion`
- `pci snapshot` (offline path snapshot logic)
- `pci compare` (manifest or snapshot comparison utility)
- `pci verify` (acceptance-only validation, no runtime probe)

## 3. Analyzer capabilities

Supported by the current code:
- YAML/JSON doc discovery
- Multi-document YAML parsing
- Multi-policy summarization
- Selector normalization for label selectors and matchExpressions
- Broad selector detection for empty pod selectors
- Default-deny detection for Ingress/Egress policy types
- Semantic diff detection for policy changes
- Deterministic ordering of outputs

Limitations:
- The analyzer does not model the full Kubernetes NetworkPolicy traffic matrix and is not a substitute for runtime inference.
- It does not evaluate namespace selectors against an actual cluster inventory.
- It does not compute an exact Pod-to-Pod allow matrix from live cluster state.
- It treats policy behavior as a static model only and cannot infer CNI-specific behavior or runtime state.

## 4. NetworkPolicy semantic limitations

The current implementation models static manifest semantics and does not claim to fully implement every Kubernetes NetworkPolicy capability. Key limitations include:
- No live cluster inventory to resolve selectors to actual workloads.
- No CNI-specific enforcement semantics.
- No service translation, DNS, or host networking evaluation.
- No runtime evidence correlation for observed traffic results.
- No namespace selector evaluation against real cluster labels.
- No full peer and port matrix modeling beyond manifest summaries and diff reports.

## 5. API endpoint inventory

Implemented endpoints (current code):
- `GET /healthz`
- `GET /api/v1/health`
- `GET /api/v1/version`
- `GET /api/v1/capabilities`
- `GET /api/v1/clusters/{clusterId}`
- `GET /api/v1/clusters/{clusterId}/pods`
- `GET /api/v1/clusters/{clusterId}/network-policies`
- `GET /api/v1/clusters/{clusterId}/capabilities`
- `GET /api/v1/analyses`
- `POST /api/v1/analyses`
- `GET /api/v1/analyses/{analysisId}`
- `GET /api/v1/analyses/{analysisId}/findings`
- `GET /api/v1/analyses/{analysisId}/graph`
- `GET /api/v1/analyses/{analysisId}/explain/{findingId}`
- `POST /api/v1/snapshots`
- `GET /api/v1/snapshots`
- `GET /api/v1/snapshots/{snapshotId}`
- `POST /api/v1/snapshots/compare`
- `POST /api/v1/verifications`
- `POST /api/v1/reports`

The server is intentionally minimal and is not yet an authenticated production API.

## 6. Frontend API consumption

The current frontend is a static dashboard mock, not a real API consumer. It does not fetch or display backend responses. It is a presentational UI built from static arrays and is not yet wired to the Go API contracts.

## 7. SQLite persistence status

SQLite is initialized and ready for local persistence, but the repository does not yet provide a complete repository layer with migrations, retention, or end-to-end transactional usage from the rest of the application.

Current state:
- `internal/storage/sqlite.go` initializes the `analyses` table.
- There is no robust migration framework or backup retention policy.
- The app does not yet persist analysis records in a production-backed service layer.

## 8. Security controls

Present:
- Read-only default mode for core analysis workflow.
- No secret values are embedded in the CLI or API examples.
- Error responses are structured JSON and avoid stack traces.
- API outputs are versioned under `/api/v1`.
- The server uses explicit request timeouts.

Missing or partial:
- No real authn/authz.
- No request-size guarding beyond minimal server-level timeouts.
- No CORS/CSRF protection model.
- No DB credential handling or secret management pattern.
- No real Kubernetes permission boundaries or RBAC enforcement.

## 9. Missing integration tests

Missing or weakly covered areas:
- Namespace selector matching against real or synthetic cluster data.
- Ingress/egress isolation logic across multiple policies.
- Additive permission union semantics.
- Named ports and workload metadata resolution.
- DNS-related operational risk prediction.
- CNI and runtime flow adapters.
- Frontend API hooking and end-to-end rendering.
- SQLite persistence across reopen/restart behavior.
- CLI exit codes across failure conditions.
- Server-side security and request validation.

## 10. Build and test results

Commands executed and results:
- `cd /home/hhoseini/projects_personal/pci && go test ./...` — passed
- `cd /home/hhoseini/projects_personal/pci && go run ./cmd/pci analyze --path ./fixtures/base` — passed
- `cd /home/hhoseini/projects_personal/pci && go run ./cmd/pci diff --base ./fixtures/base --proposed ./fixtures/proposed` — passed
- `cd /home/hhoseini/projects_personal/pci && go run ./cmd/pci explain finding-001 --analysis /tmp/finding-analysis.json` — passed
- `cd /home/hhoseini/projects_personal/pci/web && npm run build` — passed

## 11. Prioritized defects and gaps

1. NetworkPolicy semantics are intentionally static and incomplete relative to the full K8s spec.
2. No live cluster integration or RBAC-aware discovery is in place.
3. Frontend is static and not connected to backend responses.
4. SQLite persistence is not a complete durable repository layer.
5. Verification/change lifecycle remains stubbed and not tied to real cluster mutation.
6. Security model is not yet production-ready.

## 12. Capability classification summary

- Implemented and tested: Static analysis, semantic diff, basic CLI, graph serialization, basic API shell, frontend build.
- Implemented but insufficiently tested: Explanation model, snapshot and compare commands, verification acceptance.
- Partially implemented: Backend API routes, SQLite-ready storage layer, change lifecycle shells.
- Not implemented: Kubernetes integration, runtime verification adapters, production auth/security, real UI reactivity.
- Blocked by unavailable infrastructure: cluster-based verification and write-enabled K8s tests.
