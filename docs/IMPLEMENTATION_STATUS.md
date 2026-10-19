# Implementation Status

## Current state

PCI remains an offline-first NetworkPolicy analyzer with an API-backed UI and local SQLite persistence. This phase adds endpoint-specific modeled connectivity evaluation and a conservative verification contract. It does not add a production runtime probe provider, claim full Kubernetes conformance, or add any cluster write capability.

## Implemented

- [x] Existing manifest analyzer, semantic policy diff, SQLite persistence, API, frontend, and read-only discovery retained.
- [x] Per-flow ingress and egress evaluation with additive policy semantics and policyTypes defaulting.
- [x] Pod selectors, selector expressions, namespace selectors, combined selectors, empty selectors, and namespace boundaries.
- [x] TCP/UDP/SCTP model protocol values, omitted ports, numeric ports/ranges, and named ports when destination workload metadata resolves them.
- [x] IPBlock CIDR and `except` matching.
- [x] `policyInventoryComplete` gate; missing policy inventory, missing relevant labels, host-network endpoints, malformed port ranges, invalid IPBlock exceptions, and unsupported inputs produce `unknown` rather than assumed allow/deny.
- [x] API `POST /connectivity` and `/connectivity/diff`; CLI `connectivity` and `connectivity-diff` use the shared analyzer.
- [x] Connectivity diffs retain full before/after endpoint results and relevant rules, report semantically changed policy names/fields with exact normalized field values, and avoid calling configuration deltas proof of runtime causation.
- [x] Path-based semantic diffs inspect normalized full policy specs, not only selectors and rule counts; malformed NetworkPolicies fail instead of silently disappearing.
- [x] Policy summaries infer omitted `policyTypes` per Kubernetes defaulting, including ingress isolation when an Egress rule is present but Ingress rules are absent.
- [x] Kubernetes read-only discovery requires an explicit namespace or all-namespaces mode, uses namespace-scoped namespace metadata reads, follows list continuation tokens, exposes collection completeness and incomplete resource kinds, records namespace labels/Pod-template labels, reports hostNetwork, and propagates request cancellation.
- [x] Manifest findings carry modeled provenance; finding explanations no longer infer a flow allow/deny result from finding severity or wording.
- [x] Provider-neutral verification request/provider/authorizer/evidence types.
- [x] Exact source/destination/protocol/port allowlist authorizer; empty allowlists deny. Verification requires an installed provider, authorizer, and persistence store.
- [x] Verification API validates identity, DNS/IP destination, TCP/UDP, port, and a 1–10,000 ms timeout; request context and timeout propagate; concurrent executions are bounded to two.
- [x] Verification evidence persistence and retrieval in SQLite, including attempted time, duration, method, observation state, provenance, and limitations.
- [x] API capability reporting, OpenAPI contract, CLI request validation, frontend presentation of model and observation layers.
- [x] Deterministic fake provider is used only in tests and marks all output synthetic. It is not installed in normal server or CLI startup.
- [x] No live `exec`, arbitrary shell command, Pod creation, deployment modification, or policy mutation path.

## Partial or unsupported

- [ ] No production runtime provider is implemented. Runtime verification remains disabled in normal operation and returns `VERIFICATION_DISABLED` without creating evidence.
- [ ] The flow evaluator accepts caller-supplied inventory and endpoint facts; freshness of endpoint/policy metadata is not independently validated.
- [ ] Named-port evaluation needs destination workload named-port metadata; missing metadata yields `unknown`.
- [ ] Full Kubernetes edge cases are not modeled, including CNI-specific behavior, node traffic exceptions, service/NAT address rewriting, and runtime DNS/service-mesh behavior.
- [ ] Observed success/failure is not connected to a policy-causation verifier. Failures never become `denied_by_model` or verified NetworkPolicy denials.
- [ ] Discovery retains NetworkPolicy summaries rather than complete NetworkPolicy specs and is not yet wired into endpoint-specific flow evaluation. `complete` means the requested supported collections were fully retrieved, not that unsupported API kinds, CNI policy, or runtime facts were collected.
- [ ] A missing `hostNetwork` flag in caller-supplied flow input is interpreted as false; callers must supply Pod facts accurately. Discovery returns the actual hostNetwork flag and the analyzer returns unknown when it is true.
- [ ] Namespace metadata or endpoint labels can become stale between collection and analysis; there is no freshness TTL or automatic revalidation.
- [ ] API authentication is not implemented. A future server deployment must secure access to analysis manifests, cluster discovery, and verification endpoints.
- [ ] No tests require a live cluster; real CNI/runtime behavior is untested.

## Validation

- Baseline before Phase 5 changes: `go test ./...` and `cd web && npm run build` passed.
- Final tree: `go test ./...` passed.
- Final tree: `go test -race ./...` passed.
- Final tree: `go vet ./...` passed.
- Final tree: `gofmt -l` check on all changed Go files returned no files.
- Final tree: `cd web && npm run build` passed (TypeScript build and Vite production build).
- Final tree: `api/openapi.yaml` parsed successfully as YAML; no OpenAPI schema validator was run.
- CLI smoke test: `go run ./cmd/pci diff --base ./fixtures/base --proposed ./fixtures/proposed` succeeded and emitted modified policy fields plus exact before/after values.
- No live Kubernetes cluster or CNI tests were run.

The workspace is not a Git checkout in the current environment, so `git status`/diff-based change inspection is unavailable.
