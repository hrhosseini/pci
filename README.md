# PolicyChangeImpact (pci)

PolicyChangeImpact is an offline-first NetworkPolicy change-impact analyzer. It combines a Go analysis engine, a durable local SQLite store, a documented HTTP API, and a React UI that consumes real backend JSON rather than mock data.

## Current status
This repository is a working offline-first NetworkPolicy analysis vertical slice with API, SQLite persistence, and a React UI. It is **not production-ready**: there is no API authentication, no production runtime probe provider, no live CNI validation, and no controlled apply. Modeled predictions, runtime observations, and policy-causation claims are kept distinct; incomplete inputs yield `unknown` rather than assumed allow/deny. See [docs/IMPLEMENTATION_STATUS.md](docs/IMPLEMENTATION_STATUS.md) and [docs/SECURITY.md](docs/SECURITY.md).

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

The API listens on `127.0.0.1:8080` by default and has no authentication. Non-loopback binds (including empty/`0.0.0.0`) are refused unless `PCI_ALLOW_NON_LOOPBACK=1` is set after reviewing [docs/SECURITY.md](docs/SECURITY.md).

### Web app
```bash
cd /home/hhoseini/projects_personal/pci/web
npm install
npm run dev
```

For a local Vite UI that only your machine reaches, prefer the default host. Binding the frontend with `--host 0.0.0.0` does not secure the Go API; keep the API on loopback unless you have consciously set `PCI_ALLOW_NON_LOOPBACK`.

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
- Optional disposable kind + Calico live-validation harness, never run by default

## Known limitations
This repository intentionally keeps risk low by remaining offline-first. It does not include a production runtime probe provider or mutation pathways. Flow predictions require a supplied policy inventory and endpoint metadata; incomplete information produces `unknown`. A caller must ensure endpoint/policy metadata is fresh; there is no stale-snapshot enforcement. Host-network flows are unsupported, and `ipBlock` evaluation requires the caller to assert the address as seen by policy enforcement and absence of a node exception. CNI behavior, Service/NAT rewriting, DNS, node-traffic exceptions, and real workload execution are not verified by the static engine. See [docs/NETWORKPOLICY_SEMANTICS.md](docs/NETWORKPOLICY_SEMANTICS.md) and [docs/SECURITY.md](docs/SECURITY.md).

### Optional live NetworkPolicy validation

The pinned, disposable kind/Calico setup, explicit confirmation strings, fixed
scenarios, safeguards, JSONL results, and teardown commands are documented in
[docs/LIVE_VALIDATION.md](docs/LIVE_VALIDATION.md). These tests are not part of
`go test ./...`; never reuse or target an existing cluster. The harness is
TCP/80-focused. Pinned Kubernetes `v1.31.4`, Calico `v3.30.0`, and image digests
are declared targets only—they have not been validated by a live run in this
workspace. Before any future live test: review `docs/LIVE_VALIDATION.md`, use a
new dedicated kubeconfig and results path, set the required confirmation
variables, and never target an existing context such as `kind-kind`.

### Verify a constrained request (validation only)
Create a JSON request containing `sourceNamespace`, `sourceWorkload`, `destination`, `protocol`, `port`, and `timeoutMillis`, then run:

```bash
go run ./cmd/pci verify --plan ./verification-request.json
```

The CLI reports that execution is disabled unless a provider is explicitly installed. It does not execute a probe. The test-only fake provider is never enabled in normal server or CLI startup.
