# Architecture

PolicyChangeImpact is structured as a Go monolith with clear internal boundaries instead of a distributed system. This keeps the analysis engine, API, CLI, and frontend aligned while preserving room for future CNI adapters or storage backends.

## Layers

### CLI layer
- `cmd/pci` defines the user-facing commands.
- Commands are thin wrappers around shared domain logic in `internal/analyzer` and `pkg/pci`.
- Exit codes are stable and reflect policy/analysis outcomes rather than internal errors.

### Service and API layer
- `internal/api` exposes the HTTP API used by the dashboard and automation.
- Services validate requests, authorize actions, and orchestrate workflows.
- Work is intentionally read-only unless a change is explicitly approved and executed through the change-management lifecycle.

### Analysis domain
- `internal/analyzer` models the policy semantics, selector matching, endpoint-specific flow evaluation, diffing, and impact findings.
- The engine is deterministic and written to operate on YAML/JSON policy inputs and cluster snapshots.
- Findings capture severity, assumptions, evidence, and unsupported aspects.
- Connectivity results are explicitly `modeled`; incomplete inventories and selector/port inputs propagate `unknown`.
- Connectivity diffs retain before/after direction and rule evidence plus exact normalized policy field changes. They do not establish that a policy change caused runtime behavior.

### Storage and evidence
- `internal/storage` owns persistence for snapshots, analysis results, and evidence records.
- SQLite is used for local persistence without requiring a distributed database.
- Evidence is separated from modeled conclusions so the system never conflates static model results with observed runtime behavior.
- Discovery results are timestamped and include a completeness flag; incomplete resource kinds are not treated as empty collections.

### Runtime verification boundary
- `internal/verification` defines a provider-neutral request, provider contract, validation, and evidence schema.
- The production server installs no provider, so runtime probes remain disabled.
- A deterministic synthetic provider is available only to tests and marks provenance as synthetic.
- There is no workload exec implementation in this phase; no shell command or cluster write path exists.

### Frontend
- `web` is a Vite + React + TypeScript application.
- It renders a dashboard and graph using data supplied by the backend or fixtures.
- It never bypasses backend authorization and always communicates via API calls for write actions.

## Data flow
1. Input manifests or snapshots are normalized into a canonical policy model.
2. The analyzer evaluates selectors, peers, ports, and default-deny conditions.
3. Findings are generated with severity, confidence, and assumptions.
4. The API exposes the results and supports snapshots and verification flows.
5. The dashboard visualizes the graph and summary findings for operators.

## Security boundaries
- Default mode is read-only.
- Approval is required before any mutation workflow begins.
- Operational results are marked as observed, modeled, or verified to avoid cross-label confusion.
- Evidence metadata is preserved for auditing and review.
