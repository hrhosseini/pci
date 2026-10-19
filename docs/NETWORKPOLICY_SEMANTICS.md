# NetworkPolicy Semantics Notes

PCI evaluates a supplied flow against supplied NetworkPolicy objects. The result is a model prediction, not a live cluster runtime simulation. Semantics follow the Kubernetes NetworkPolicy contract:
<https://kubernetes.io/docs/concepts/services-networking/network-policies/>.

## What is modeled
- Policy document parsing and normalization
- Pod selector matching using label selectors and matchExpressions
- Namespace selector matching and the intersection of namespace + pod selectors
- Independent source-egress and destination-ingress evaluation
- Additive rules across all applicable policies
- PolicyTypes inference and explicit Ingress/Egress policy types
- Numeric ports, protocol defaults, inclusive `endPort` ranges, resolvable named destination ports, and ipBlock CIDRs/exceptions
- Default allow for a direction that is not isolated, when the supplied policy inventory is explicitly complete
- Baseline vs proposed policy differences, including same-count rule edits, named changed fields, semantic fingerprints, and endpoint-specific before/after matched-rule evidence
- Read-only discovery list pagination, completeness flags, namespace labels, Pod-template labels, and hostNetwork metadata
- Structured findings and explanation output
- Distinction between modeled, observed, verified, and unknown states

## What the current implementation does not claim
- Real CNI behavior
- Runtime DNS or service mesh behavior
- Implicit support for every Kubernetes edge case
- Automatic inference of complete policy inventories from partial manifests or discovery results
- Conversion of discovery summaries into a complete flow-evaluation inventory; discovered policy results currently expose summaries, not full rules
- Freshness validation for caller-supplied endpoint labels/IPs or policy inventories; callers must ensure their metadata corresponds to the intended snapshot
- Confident flow evaluation for endpoints explicitly marked `hostNetwork`; these return `unknown`
- `ipBlock` decisions without an explicit caller assertion that the address is the one seen by the NetworkPolicy implementation and is not covered by the node exception; these return `unknown`
- Named-port decisions without destination workload port metadata
- Kubernetes node traffic exceptions, Service/NAT address rewriting, CNI-specific behavior, or runtime DNS/service-mesh behavior

## Supported status labels
- `allowed_by_model`
- `denied_by_model`
- `observed_success`
- `observed_failure`
- `unknown`

These states are not conflated. The connectivity evaluator emits only model states or `unknown`. Verification evidence records observations; the current production build does not have a live probe provider and does not emit verified policy-causation states. A failed connection does not imply a NetworkPolicy denial.

## Rule interpretation
The implementation treats NetworkPolicies as additive allowlists in the direction-specific model. It does not treat them as first-match firewall rules.

For a Pod-to-Pod flow, policy restrictions are evaluated on the forward path:
- source egress permissions
- destination ingress permissions

Reply traffic for an allowed connection is implicitly allowed by the Kubernetes model. Each direction is evaluated as a union of the relevant policies' rules, not as first-match behavior.

If either side is isolated and a rule is absent, the modeled result can be denied.

## Unknowns and limitations
- Endpoint labels and namespace labels must be supplied with explicit completeness flags. Missing/incomplete inputs produce `unknown` when relevant.
- `policyInventoryComplete` must be true before an otherwise non-isolated direction is reported as default-allowed. An omitted policy inventory is not assumed empty.
- Discovery sets `complete: false` and lists incomplete resource kinds if any paginated collection fails. Such a result must not be passed as a complete policy inventory.
- Named ports require destination workload port metadata; unresolved names produce `unknown`.
- Unsupported or invalid peer/port inputs produce `unknown` rather than a confident deny. Inputs on a rule whose other conjunct definitively does not match do not taint the flow result.
- `ipBlockAddressKnown` is an explicit caller assertion needed for CIDR matching. Kubernetes documents that source/destination rewriting can vary by Service, cloud provider, and network plugin, and that node traffic is always allowed; PCI cannot infer those facts from a Pod IP.
- Malformed NetworkPolicy documents fail analysis instead of silently disappearing from a path-based inventory.
- Broad selector and default-deny findings are best-effort warnings rather than runtime proof.

Connectivity diffs now retain both complete model results and identify changed policy references and changed semantic fields. Rule-level `evaluatedRules`/`matchedRules` are evidence for each state; the policy edit list is not a proof that a particular edit alone caused the transition, especially when several policies change.

The focused table-driven tests compare behavior against the upstream specification's documented additive, direction, selector, defaulting, port, and IPBlock rules. They are independently authored PCI tests, not the upstream Kubernetes conformance suite, and do not establish conformance for all Kubernetes edge cases.
