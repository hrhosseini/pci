# Implementation Plan

## Goal
PolicyChangeImpact is a Kubernetes NetworkPolicy analyzer that helps operators answer whether a proposed change broadens, narrows, or reconfigures allowed traffic. The project is implemented as a modular Go backend and a React frontend sharing a single analysis model.

> **Status pointer.** Phases 1–5 below are the original foundation plan. Later work (connectivity modeling, discovery, evidence integrity, disposable harness, offline release audit) is tracked in [IMPLEMENTATION_STATUS.md](./IMPLEMENTATION_STATUS.md). That status document is authoritative for what is implemented, partial, or untested today.

## Phase 1 — Foundation and architecture
- Create the repository structure, Go module, CLI entry points, and standard interfaces for analysis, validation, and reporting.
- Define domain types for clusters, policies, findings, snapshots, and evidence.
- Document architecture, threat model, and ADR decisions.
- Acceptance criteria: `go test ./...` passes for the core engine; `pci version` and `pci doctor` work.

## Phase 2 — Static analysis engine
- Parse Kubernetes NetworkPolicy YAML/JSON documents.
- Normalize selectors, peers, ports, and policy types.
- Detect malformed or broad rules, default-deny patterns, and default DNS risk warnings.
- Produce a structured finding model with IDs, evidence links, and assumptions.
- Acceptance criteria: `pci analyze --path ./fixtures/policies` emits JSON findings and summary counts.

## Phase 3 — Semantic diff and impact predictions
- Compare baseline and proposed policy snapshots.
- Detect semantic differences beyond raw YAML diffs.
- Rank changes by severity using explainable rules.
- Acceptance criteria: `pci diff --base ./fixtures/base --proposed ./fixtures/proposed` returns added/removed/modified policy summaries.

## Phase 4 — API and serving layer
- Implement a local HTTP API for analysis, snapshots, and evidence.
- Support JSON responses, error handling, and request-size enforcement.
- Acceptance criteria: `pci serve` starts on a configured port and `/api/v1/health` responds successfully.

## Phase 5 — Frontend dashboard
- Build a React dashboard with summary cards, lists, and a network graph.
- Connect to backend data and render analysis summaries without hiding assumptions or confidence.
- Acceptance criteria: `npm run build` completes and the dashboard loads a simplified analysis state.

## Phase 6 — Validation, verification, and docs (original plan)
- Run core tests, Go formatting, and frontend build checks.
- Validate CLI exit codes and output contracts.
- Document supported limitations and security controls.
- Acceptance criteria: docs clearly separate modeled, observed, and verified traffic states.

## Later phases (summary; see status doc for detail)

Numbering in [IMPLEMENTATION_STATUS.md](./IMPLEMENTATION_STATUS.md) diverged from this original Phase 6 definition:

| Status-doc phase | Intent |
|------------------|--------|
| 6–8 | Disposable kind/Calico harness scaffolding, offline safety tests, TCP scenario fixtures (not live-executed) |
| 9 | Evidence integrity, insert-only persistence, idempotency reservations |
| 10 | Offline release-readiness audit; SQLite concurrency/file-mode hardening |
| 11 | Working-tree stabilization, doc reconciliation, loopback bind enforcement for the unauthenticated API |

Still out of scope until explicitly authorized: production runtime provider, controlled apply, and live CNI validation against a disposable cluster.

## Architecture decisions
- Use a single analysis engine shared by the CLI and backend.
- Keep the data model deterministic and serializable to JSON.
- Treat NetworkPolicy as a modeling primitive rather than evidence of actual runtime behavior.
- Require explicit approval before mutating cluster state.
- Keep the HTTP API loopback-bound by default while it remains unauthenticated.

## Current status (pointer)
Do not use the early “repository is initialized scaffolding” wording below as current truth. For the live checklist, limitations, and offline validation results, use [IMPLEMENTATION_STATUS.md](./IMPLEMENTATION_STATUS.md).

### Historical note (early implementation wording)
The repository was initialized with the Go CLI, server skeleton, React frontend, and the foundational policy-analysis model. The project is intentionally designed to be executable and testable without a live Kubernetes cluster.
