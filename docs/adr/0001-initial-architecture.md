# 0001: Initial architecture

Date: 2026-10-19

## Status
Accepted

## Context
The product must support both a CLI and a web UI while preserving a single analysis engine and consistent semantics. A full distributed system would delay delivery and add operational complexity without improving the initial correctness of NetworkPolicy analysis.

## Decision
Use a single Go-based analysis engine, a lightweight HTTP API, and a standard React frontend. Keep the service as a modular monolith with clear internal packages for analyzer, API, storage, evidence, and graph generation.

## Consequences
- Easier to test and reason about.
- Shared JSON schema and deterministic results across CLI and dashboard.
- Clear path to add CNI adapters or a richer storage layer later.
- Requires disciplined boundary management to avoid mixing domain logic with UI concerns.
