# Implementation Status

## Current state

PCI remains an offline-first NetworkPolicy analyzer with an API-backed UI and local SQLite persistence. This phase adds endpoint-specific modeled connectivity evaluation and a conservative verification contract. It does not add a production runtime probe provider, claim full Kubernetes conformance, or add any cluster write capability.

## Implemented

- [x] Kubernetes connection modes: `auto`, `in-cluster`, `kubeconfig` with deterministic precedence; Helm chart + Dockerfile for in-cluster deploy; `/readyz` and same-origin static UI serving; single-replica SQLite PVC guidance.
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
- [x] Verification evidence retains the requested timeout and optional references to existing analyses and snapshots; API rejects nonexistent references.
- [x] Optional evidence schema includes CNI identity and linked discovery freshness/completeness without fabricating unknown values.
- [x] Local Git repository established with a pre-Phase-6 source snapshot (`62b483b3957ee647480686d8e06e5c11cfe253b4`); generated frontend dependencies/build output and SQLite files are ignored.
- [x] Opt-in kind test setup is pinned to Kubernetes `v1.31.4`, kubectl `v1.32.2`, Calico `v3.30.0`, and kind `v0.33.0`; it disables kind's default CNI and checks cluster ownership/version before use.
- [x] Live harness uses the same NetworkPolicy YAML with `pci connectivity-diff` and a fixed TCP/80 probe, writes model-versus-observation JSONL, and cleans up only resources bearing its per-run owner label.
- [x] Live harness revalidates target identity/ownership before cluster writes, deletes, and exec; it uses exclusive report creation and fake-command safety tests.
- [x] Live scenario fixtures include separate TCP port-mismatch and UDP-vs-TCP protocol-mismatch cases.
- [x] OpenAPI contract successfully bundled/validated with Redocly CLI 1.34.5.

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
- [ ] The live kind/Calico harness has not yet been run; the workspace has an existing kind cluster named `kind`, which the harness deliberately does not use. The live test must be separately confirmed and creates a distinct `pci-phase6` cluster.
- [ ] No production runtime provider is operational. The harness uses fixed operator-run `kubectl exec` calls only inside its disposable cluster; it does not expose execution through PCI's API.
- [ ] Live scenarios probe TCP/80 only and do not establish general conformance. UDP traffic, Service/NAT, DNS, CNI-specific policies, stale/incomplete live discovery, and production workloads remain untested.
- [ ] Runtime verification evidence has no live population source for CNI or discovery metadata until an independently reviewed provider exists.

## Validation

### Kubernetes-native productization (this change set)

- `go test ./...` passed.
- `go test -race ./...` passed.
- `go vet ./...` passed.
- `cd web && npm run lint` passed (pre-existing `react(set-state-in-effect)` warning in `App.tsx`).
- `cd web && npm test` passed.
- `cd web && npm run build` passed.
- `helm lint ./charts/pci` passed.
- `helm template pci ./charts/pci -n pci-system` rendered ClusterRole (read-only) + Deployment + PVC + Service.
- Namespace-scoped RBAC template renders Roles; combining `rbac.scope=namespace` with `config.allNamespaces=true` correctly fails.
- `docker build -t pci:0.1.0 .` passed (multi-stage UI + Go image).
- No live Helm install or kind deploy was executed in this change set (image was built locally only).

### Historical

- Baseline before Phase 5 changes: `go test ./...` and `cd web && npm run build` passed.
- CLI smoke test: `go run ./cmd/pci diff --base ./fixtures/base --proposed ./fixtures/proposed` succeeded and emitted modified policy fields plus exact before/after values.
- No live Kubernetes cluster or CNI tests were run as part of default CI.

### Phase 6 historical validation

- Pre-implementation Phase 6 source snapshot: local Git baseline commit `62b483b3957ee647480686d8e06e5c11cfe253b4`; no previous `.git` directory or remote configuration existed.
- Baseline `go test ./...`, `go test -race ./...`, `go vet ./...`, frontend build, OpenAPI YAML parsing, and CLI diff smoke test passed. `gofmt -l` identified pre-existing formatting in `internal/config/config.go`, which is unrelated and was not modified.
- Post-change `go test ./...`, `go test -race ./...`, and `go vet ./...` passed. The sqlite3 C dependency emitted compiler warnings; no Go test/vet failures occurred.
- `gofmt -l` is clean for every Go file changed in this phase. Repository-wide formatting still reports only the pre-existing `internal/config/config.go`.
- `cd web && npm run build` passed. `npm run lint` exited successfully with one `react(set-state-in-effect)` warning in `web/src/App.tsx`.
- Redocly CLI 1.34.5 `bundle api/openapi.yaml` and `lint api/openapi.yaml` passed; lint emitted 39 non-fatal style warnings.
- `bash -n` passed for all three integration scripts. Setup, test, and teardown confirmation guards were checked without cluster access; each refused to run without its required confirmation.
- `go test ./internal/analyzer -run TestLiveHarnessPolicyScenariosHaveExpectedModelResults -v` passed all nine fixture scenarios using the shared offline analyzer.
- The live-fixture test also checks four independently specified modeled transitions: newly denied, newly allowed, unchanged for no-effect policies, and egress newly denied.
- `go run ./cmd/pci diff --base ./fixtures/base --proposed ./fixtures/proposed` passed and emitted valid JSON with one modified policy.
- The exact `pci connectivity-diff` CLI command used by the live harness returned `allowed_by_model -> denied_by_model (newly_denied)` for its checked-in fixture pair.
- No live Kubernetes/CNI tests were executed because explicit confirmation to create/use the Docker-backed disposable cluster was unavailable. The target versions (kind 0.33.0, Kubernetes 1.31.4, Calico 3.30.0) are pinned but not actually tested; the existing `kind-kind` cluster was not used or modified.

### Phase 7 offline validation

- `go test ./...` passed after the Phase 7 harness and semantic-fixture changes.
- `go test -race ./...` passed.
- `go vet ./...` passed.
- `gofmt -l internal/analyzer/live_scenarios_test.go` returned no files.
- `bash -n scripts/integration/setup-kind.sh scripts/integration/run-live-tests.sh scripts/integration/teardown-kind.sh scripts/integration/test-safety.sh` passed.
- `scripts/integration/test-safety.sh` passed using fake `kind` and `kubectl` commands only; no cluster was contacted.
- `cd web && npm run lint` passed with the existing `react(set-state-in-effect)` warning in `web/src/App.tsx`; `npm run build` passed.
- `npx --yes @redocly/cli@1.34.5 lint api/openapi.yaml` passed OpenAPI validation with 39 non-fatal style warnings; `npx --yes @redocly/cli@1.34.5 bundle api/openapi.yaml --output <temporary-file>` passed.
- Live authorization was not obtained, so no cluster commands, probes, setup, or teardown were executed. The existing `kind` cluster was neither contacted nor modified. Kubernetes `v1.31.4` and Calico `v3.30.0` remain declared targets only, not tested versions.

### Phase 8 offline validation and reproducibility

- Replaced the fixed policy delay with bounded per-probe polling: up to three fixed TCP attempts, each capped at four seconds, at 0.5-second intervals; two consecutive identical connectivity observations are required for a stable result. Pod readiness continues to use Kubernetes' bounded 240-second wait.
- JSONL now distinguishes `success`, `connection_failure`, `timeout`, `unsupported`, `incomplete_setup`, and `inconclusive`; agreement stays null for inconclusive/setup/unsupported/timeout outcomes. No result asserts policy causation.
- `scripts/integration/test-safety.sh` exercises stable allow/failure observations, unstable results, timeout, unsupported utility/protocol, cancellation, structured partial-setup evidence, and fake `kubectl` refusal/error paths. It does not invoke a real cluster.
- BusyBox `1.36.1` and nginx `1.27.4-alpine` are pinned by multi-platform OCI index digests, with the version/digest update procedure in [LIVE_VALIDATION.md](./LIVE_VALIDATION.md).
- The protocol fixture does test an explicit UDP rule against a TCP flow to a selected ingress-isolated Pod. It checks rule-protocol mismatch semantics; it does not test UDP transport.
- `cd web && npm run lint` is clean after restructuring async dashboard loading; `npm run build` passed.
- Redocly CLI 1.34.5 lint and bundle passed. All operationId and missing-4XX-response warnings were addressed; one `info-license` warning remains because this checkout has no declared project license and one was not invented.
- SQLite C compiler diagnostics originate in the bundled `sqlite3-binding.c` shipped by `github.com/mattn/go-sqlite3` v1.6.0 (including the diagnostic source lines in the dependency), compiled with the toolchain's default CGO settings. No project-specific C warning suppression or `CGO_CFLAGS` override was found; warnings were not hidden.
- No cluster was contacted or modified. No live Kubernetes/CNI result is claimed; the production runtime provider and controlled apply remain unimplemented.

### Phase 9 evidence integrity and release readiness

- [x] Verification evidence writes use insert-only semantics. A repeated evidence ID fails without replacing the original payload.
- [x] Verification IDs use cryptographic randomness instead of a timestamp-only identifier.
- [x] Optional `Idempotency-Key` reservations are persisted before provider execution. Matching completed retries return the same evidence; reuse with different request content conflicts; an ambiguous in-progress request is not executed a second time.
- [x] Evidence insertion and idempotency completion commit in one SQLite transaction. A persistence failure does not create a partially completed result.
- [x] Cancellation detected before provider execution returns a cancellation error without reserving the idempotency key; timeout and cancellation have distinct API errors.
- [x] Verification list/get and idempotent replay validate stored evidence rather than returning malformed JSON as a successful API response.
- [x] Provider evidence with missing/invalid provenance or environment metadata is rejected; modeled comparison remains `not-evaluated`, and observation state is derived from the provider outcome.
- [x] The live harness fake tests also verify JSONL append semantics and refusal to overwrite an existing results file.
- [x] Deterministic tests cover idempotent replay, request conflict, pending request refusal, cancellation before execution, authorization/provider timeouts, duplicate evidence IDs, transaction rollback, database reopen, malformed persistence, and missing provenance.
- [ ] Reservations that remain in progress after process interruption or uncertain provider failure are deliberately not auto-cleared. Recovery is a manual operational decision; clearing a reservation without proving no attempt occurred can duplicate a probe.
- [ ] Evidence from a configured provider is still trusted in-process. The production runtime provider is not implemented, and API authentication is absent.
- [ ] Live kind/Calico setup, probe scenarios, and teardown were not run. Kubernetes `v1.31.4`, Calico `v3.30.0`, and pinned tool/image versions are targets only, not empirically tested versions.
- [ ] Features tested only with fakes include live probe outcome classification, bounded polling, partial setup/error handling, cancellation, and harness safety/refusal branches. They do not establish CNI enforcement or live model agreement.
- [ ] Standard NetworkPolicy modeling remains limited by incomplete endpoint facts, freshness, service/NAT transformations, host/node exceptions, named-port metadata, and CNI-specific policy mechanisms. No full-conformance claim is made.
- [ ] The exact prerequisite for live validation is an operator-confirmed disposable environment created by the documented harness with Docker, pinned tools, cluster-admin only for local disposable-cluster setup/CNI installation, and a new dedicated kubeconfig/results path. No existing context may be substituted.
- [ ] Production runtime verification and controlled policy apply are not implemented. This workspace is not production-ready and no authorization to contact a Kubernetes cluster was provided for this phase.

Phase 9 validation (offline only):

- `go test ./...` — passed. The compiler emitted known warnings from bundled `github.com/mattn/go-sqlite3` `sqlite3-binding.c`; no project C warning suppression was added.
- `go test -race ./...` — passed, with the same dependency warnings.
- `go vet ./...` — passed, with the same dependency warnings.
- `gofmt -l` for the changed Go files and `git diff --check` — passed.
- `cd web && npm run lint && npm run build` — passed (Oxlint, TypeScript, and Vite production build).
- `npx --yes @redocly/cli@1.34.5 lint api/openapi.yaml` — valid, one non-fatal `info-license` warning because no project license is declared; `bundle` passed.
- `bash -n` for the setup, runner, teardown, harness library, and safety test scripts — passed; `scripts/integration/test-safety.sh` passed using fake commands.
- No Kubernetes cluster was contacted or modified; all live probe, CNI agreement, setup, and teardown results remain untested.

### Phase 10 offline release-readiness audit

- [x] Code-backed review of modeled vs observed vs causation separation, unknown/incomplete handling, insert-only evidence, idempotency reservation/completion, malformed evidence reads, discovery incompleteness, and harness JSONL exclusive-create/append behavior.
- [x] SQLite store hardened for concurrent use: single open connection, busy timeout, directory mode `0700`, database/WAL/SHM mode `0600`.
- [x] Added deterministic tests for provider timeout after idempotency reservation (no retry execution) and concurrent identical `Idempotency-Key` requests (single provider execution).
- [x] README, SECURITY.md, and this status document aligned on offline scope, discovery limits, TCP-only harness targets (not live-validated versions), missing production provider/auth/controlled apply, and pending-idempotency recovery limits.
- [ ] This workspace is **not** production-ready. No production runtime provider, live CNI validation, API authentication, or controlled apply.
- [ ] Live kind/Calico setup, probes, and teardown remain unexecuted; pinned Kubernetes/Calico/image digests are targets only.
- [x] `docs/CURRENT_STATE_AUDIT.md` and `docs/IMPLEMENTATION_PLAN.md` marked historical with pointers to this status document (Phase 11).
- [ ] Ambiguous in-progress idempotency reservations are still not auto-cleared after process interruption or provider/persistence failure (at-most-once tradeoff).

Phase 10 validation (offline only; executed during this audit):

- `go test ./...` — passed. Known warnings from bundled `github.com/mattn/go-sqlite3` `sqlite3-binding.c` only; no project C warning suppression.
- `go test -race ./...` — passed, with the same dependency warnings.
- `go vet ./...` — passed, with the same dependency warnings.
- `gofmt -l` — clean for Phase 10 changed Go files; repository-wide still reports pre-existing `internal/config/config.go` only.
- `git diff --check` — passed.
- `cd web && npm run lint && npm run build` — passed (Oxlint, TypeScript, Vite production build).
- `npx --yes @redocly/cli@1.34.5 lint api/openapi.yaml` — valid; one non-fatal `info-license` warning (no project license declared). `bundle` passed.
- `bash -n` for setup, runner, teardown, harness-lib, and test-safety scripts — passed; `scripts/integration/test-safety.sh` passed with faked kind/kubectl only.
- No Kubernetes cluster was contacted or modified.

### Phase 11 working-tree stabilization

- [x] Reconfirmed Phase 10 SQLite hardening is present: `SetMaxOpenConns(1)` / `SetMaxIdleConns(1)`, `_busy_timeout=5000`, insert-only evidence + transactional idempotency completion, Unix `0700`/`0600` modes with documented Windows limitation.
- [x] Unauthenticated API bind guard: empty and non-loopback addresses refused unless `PCI_ALLOW_NON_LOOPBACK=1`/`true`.
- [x] Historical docs reconciled without deleting useful early-audit content.
- [ ] No production readiness claim. No live cluster contact. No commits created in this phase without operator approval.
- [ ] Optional working-tree backup was proposed earlier and was **not** created; create it before any destructive local operations if desired.

Phase 11 validation (offline only; re-run during stabilization):

- `go test ./...` — exit 0 (sqlite3 C warnings only).
- `go test -race ./...` — exit 0 (same warnings).
- `go vet ./...` — exit 0 (same warnings).
- Phase 11 Go files `gofmt -l` clean; repository-wide still lists pre-existing `internal/config/config.go`.
- `git diff --check` — exit 0.
- `cd web && npm run lint` — exit 0; `npm run build` — exit 0.
- `npx --yes @redocly/cli@1.34.5 lint api/openapi.yaml` — exit 0, one non-fatal `info-license` warning; `bundle` — exit 0.
- `bash -n` on all five integration scripts — exit 0; `scripts/integration/test-safety.sh` — exit 0 (faked commands).
- No Kubernetes cluster was contacted or modified. No commits were created.
