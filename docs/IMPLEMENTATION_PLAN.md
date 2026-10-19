# Implementation Plan

## Goal
PolicyChangeImpact is a Kubernetes NetworkPolicy analyzer that helps operators answer whether a proposed change broadens, narrows, or reconfigures allowed traffic. The project is implemented as a modular Go backend and a React frontend sharing a single analysis model.

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

## Phase 6 — Validation, verification, and docs
- Run core tests, Go formatting, and frontend build checks.
- Validate CLI exit codes and output contracts.
- Document supported limitations and security controls.
- Acceptance criteria: docs clearly separate modeled, observed, and verified traffic states.

## Architecture decisions
- Use a single analysis engine shared by the CLI and backend.
- Keep the data model deterministic and serializable to JSON.
- Treat NetworkPolicy as a modeling primitive rather than evidence of actual runtime behavior.
- Require explicit approval before mutating cluster state.

## Current status (this implementation)
The repository is initialized with the Go CLI, server skeleton, React frontend, and the foundational policy-analysis model. The project is intentionally designed to be executable and testable without a live Kubernetes cluster.
