# PolicyChangeImpact (pci)

PolicyChangeImpact is an offline-first NetworkPolicy change-impact analyzer. It combines a Go analysis engine, a durable local SQLite store, a documented HTTP API, and a React UI that consumes real backend JSON rather than mock data.

## Current status
This repository is a working integrated vertical slice for static NetworkPolicy analysis. It does not claim to model live cluster traffic or to perform mutating Kubernetes operations. See [docs/IMPLEMENTATION_STATUS.md](docs/IMPLEMENTATION_STATUS.md) for the current checklist and known limitations.

## Components
- Go CLI: `analyze`, `diff`, `connectivity`, `connectivity-diff`, `explain`, `graph`, `report`, `snapshot`, `compare`, `verify`, `discover`, `doctor`, `serve`, `version`
- Static analysis engine: parses YAML/JSON NetworkPolicy manifests and summarizes policy intent, default-deny settings, selectors, and change impact.
- API server: `/api/v1` endpoints for health, version, capabilities, analysis, findings, graph export, semantic diff, snapshots, reports, and verification planning.
- SQLite storage: local persistence for analyses and snapshots with schema setup and reopen verification.
- Web frontend: React dashboard and policy views backed by real API responses from the Go backend.

## Quick start

### CLI
```bash
cd /home/hhoseini/projects_personal/pci
go run ./cmd/pci analyze --path ./fixtures/base
go run ./cmd/pci diff --base ./fixtures/base --proposed ./fixtures/proposed
go run ./cmd/pci explain --analysis /tmp/finding-analysis.json --finding <finding-id>
```

### Server
```bash
cd /home/hhoseini/projects_personal/pci
go run ./cmd/pci-server
```

### Web app
```bash
cd /home/hhoseini/projects_personal/pci/web
npm install
npm run dev -- --host 0.0.0.0
```

## Supported behavior
- YAML/JSON NetworkPolicy parsing with multi-document support
- Baseline/proposed semantic diff generation
- Endpoint-specific flow comparison with before/after rule evidence and exact normalized policy-field deltas
- Structured findings and explanations
- Graph export based on modeled permissions
- Snapshot creation and retrieval
- SQLite-backed persistence for analyses and snapshots
- API-backed UI for dashboard, topology, policies, comparison, reports, and settings
- Offline verification planning without live cluster claims
- Endpoint-specific NetworkPolicy flow prediction with explicit completeness/uncertainty
- Paginated, explicitly scoped read-only Kubernetes discovery that exposes partial collection status (discovery summaries are not yet a flow-evaluation inventory)
- Provider-neutral verification request/evidence contract; runtime execution disabled by default

## Known limitations
This repository intentionally keeps risk low by remaining offline-first. It does not include a production runtime probe provider or mutation pathways. Flow predictions require a supplied policy inventory and endpoint metadata; incomplete information produces `unknown`. A caller must ensure endpoint/policy metadata is fresh; there is no stale-snapshot enforcement. Host-network flows are unsupported, and `ipBlock` evaluation requires the caller to assert the address as seen by policy enforcement and absence of a node exception. CNI behavior, Service/NAT rewriting, DNS, node-traffic exceptions, and real workload execution are not verified by the static engine. See [docs/NETWORKPOLICY_SEMANTICS.md](docs/NETWORKPOLICY_SEMANTICS.md) and [docs/SECURITY.md](docs/SECURITY.md).

### Verify a constrained request (validation only)
Create a JSON request containing `sourceNamespace`, `sourceWorkload`, `destination`, `protocol`, `port`, and `timeoutMillis`, then run:

```bash
go run ./cmd/pci verify --plan ./verification-request.json
```

The CLI reports that execution is disabled unless a provider is explicitly installed. It does not execute a probe. The test-only fake provider is never enabled in normal server or CLI startup.
