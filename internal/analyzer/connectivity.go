package analyzer

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type DirectionResult struct {
	Status             TrafficStatus `json:"status"`
	Direction          string        `json:"direction"`
	Isolated           bool          `json:"isolated"`
	ApplicablePolicies []string      `json:"applicablePolicies,omitempty"`
	EvaluatedRules     []string      `json:"evaluatedRules,omitempty"`
	MatchedRules       []string      `json:"matchedRules,omitempty"`
	Reason             string        `json:"reason"`
	Assumptions        []string      `json:"assumptions,omitempty"`
	Unsupported        []string      `json:"unsupported,omitempty"`
}

type ConnectivityQuery struct {
	Source                  Endpoint `json:"source"`
	Destination             Endpoint `json:"destination"`
	Protocol                string   `json:"protocol"`
	Port                    int32    `json:"port"`
	PolicyInventoryComplete bool     `json:"policyInventoryComplete"`
}

type Endpoint struct {
	Namespace            string               `json:"namespace"`
	Name                 string               `json:"name"`
	Labels               map[string]string    `json:"labels,omitempty"`
	LabelsComplete       bool                 `json:"labelsComplete"`
	NamespaceLabels      map[string]string    `json:"namespaceLabels,omitempty"`
	NamespaceLabelsKnown bool                 `json:"namespaceLabelsKnown"`
	HostNetwork          bool                 `json:"hostNetwork,omitempty"`
	IP                   string               `json:"ip,omitempty"`
	IPBlockAddressKnown  bool                 `json:"ipBlockAddressKnown"`
	NamedPortsComplete   bool                 `json:"namedPortsComplete"`
	NamedPorts           map[string]NamedPort `json:"namedPorts,omitempty"`
}

type NamedPort struct {
	Port     int32  `json:"port"`
	Protocol string `json:"protocol,omitempty"`
}

type ConnectivityResult struct {
	Status                  TrafficStatus   `json:"status"`
	Source                  string          `json:"source"`
	Destination             string          `json:"destination"`
	Protocol                string          `json:"protocol"`
	Port                    int32           `json:"port"`
	PolicyInventoryComplete bool            `json:"policyInventoryComplete"`
	Ingress                 DirectionResult `json:"ingress"`
	Egress                  DirectionResult `json:"egress"`
	Reason                  string          `json:"reason"`
	Assumptions             []string        `json:"assumptions,omitempty"`
	Unsupported             []string        `json:"unsupported,omitempty"`
	Provenance              string          `json:"provenance"`
}

type ConnectivityChange struct {
	Before        TrafficStatus          `json:"before"`
	After         TrafficStatus          `json:"after"`
	Change        string                 `json:"change"`
	BeforeResult  ConnectivityResult     `json:"beforeResult"`
	AfterResult   ConnectivityResult     `json:"afterResult"`
	PolicyChanges []PolicyChangeEvidence `json:"policyChanges"`
	Explanation   string                 `json:"explanation"`
}

type PolicyChangeEvidence struct {
	Policy        string              `json:"policy"`
	Change        string              `json:"change"`
	ChangedFields []string            `json:"changedFields,omitempty"`
	FieldChanges  []PolicyFieldChange `json:"fieldChanges,omitempty"`
}

// EvaluateConnectivity evaluates only the Kubernetes NetworkPolicy model. It
// does not predict CNI behavior or establish observed network connectivity.
func EvaluateConnectivity(policies []networkingv1.NetworkPolicy, query ConnectivityQuery) ConnectivityResult {
	protocol := strings.ToUpper(strings.TrimSpace(query.Protocol))
	if protocol == "" {
		protocol = string(corev1.ProtocolTCP)
	}
	result := ConnectivityResult{
		Source:                  endpointID(query.Source),
		Destination:             endpointID(query.Destination),
		Protocol:                protocol,
		Port:                    query.Port,
		Provenance:              "modeled",
		PolicyInventoryComplete: query.PolicyInventoryComplete,
		Assumptions: []string{
			"The result uses only the supplied policy inventory and endpoint metadata.",
			"CNI enforcement, routing, service translation, DNS, and observed traffic are not evaluated.",
		},
	}
	if !validProtocol(protocol) || query.Port < 1 || query.Port > 65535 {
		result.Status = Unknown
		result.Reason = "Protocol or destination port is invalid or unsupported."
		result.Unsupported = []string{"Only TCP, UDP, and SCTP with a valid numeric destination port can be evaluated."}
		return result
	}
	if query.Source.Namespace == "" || query.Source.Name == "" || query.Destination.Namespace == "" || query.Destination.Name == "" {
		result.Status = Unknown
		result.Reason = "Source and destination workload identities are required for flow evaluation."
		result.Unsupported = []string{"Endpoint identity is incomplete."}
		return result
	}
	if query.Source.HostNetwork || query.Destination.HostNetwork {
		result.Status = Unknown
		result.Reason = "Host-network endpoint behavior is outside the portable NetworkPolicy model."
		result.Unsupported = []string{"Kubernetes NetworkPolicy enforcement for hostNetwork Pods depends on node and network-plugin behavior."}
		result.Ingress = DirectionResult{Status: Unknown, Direction: "ingress", Reason: result.Reason, Unsupported: append([]string{}, result.Unsupported...)}
		result.Egress = DirectionResult{Status: Unknown, Direction: "egress", Reason: result.Reason, Unsupported: append([]string{}, result.Unsupported...)}
		return result
	}
	if query.Source.Namespace == query.Destination.Namespace &&
		query.Source.Name != "" && query.Source.Name == query.Destination.Name {
		result.Status = AllowedByModel
		result.Ingress = DirectionResult{Status: AllowedByModel, Direction: "ingress", Reason: "A Pod cannot block access to itself under NetworkPolicy semantics."}
		result.Egress = DirectionResult{Status: AllowedByModel, Direction: "egress", Reason: "A Pod cannot block access to itself under NetworkPolicy semantics."}
		result.Reason = "Source and destination identify the same Pod; the NetworkPolicy model does not block self-traffic."
		return result
	}

	result.Ingress = evaluateDirection(policies, query, protocol, "Ingress")
	result.Egress = evaluateDirection(policies, query, protocol, "Egress")
	result.Assumptions = uniqueSorted(append(result.Assumptions, append(result.Ingress.Assumptions, result.Egress.Assumptions...)...))
	result.Unsupported = uniqueSorted(append(
		append([]string{}, result.Ingress.Unsupported...),
		result.Egress.Unsupported...,
	))
	if result.Ingress.Status == DeniedByModel || result.Egress.Status == DeniedByModel {
		result.Status = DeniedByModel
		result.Reason = "At least one direction is isolated and no applicable policy rule allows the connection."
	} else if result.Ingress.Status == Unknown || result.Egress.Status == Unknown {
		result.Status = Unknown
		result.Reason = "Available endpoint or policy information is insufficient to determine both directions."
	} else {
		result.Status = AllowedByModel
		result.Reason = "Both ingress and egress are allowed by the supplied NetworkPolicy model."
	}
	return result
}

// CompareConnectivity reports an endpoint-specific modeled transition.
func CompareConnectivity(before, after []networkingv1.NetworkPolicy, query ConnectivityQuery) ConnectivityChange {
	beforeResult := EvaluateConnectivity(before, query)
	afterResult := EvaluateConnectivity(after, query)
	change := "unchanged"
	switch {
	case beforeResult.Status == Unknown || afterResult.Status == Unknown:
		change = "uncertain"
	case beforeResult.Status == AllowedByModel && afterResult.Status == DeniedByModel:
		change = "newly_denied"
	case beforeResult.Status == DeniedByModel && afterResult.Status == AllowedByModel:
		change = "newly_allowed"
	}
	explanation := "No modeled connectivity transition was identified; policy-level changes are listed separately from flow evidence."
	if change == "newly_allowed" || change == "newly_denied" {
		explanation = "The endpoint-specific modeled status changed. Before/after direction results list the applicable policies and evaluated or matched rules; this is not runtime evidence or proof of policy causation."
	} else if change == "uncertain" {
		explanation = "The before/after model states differ or are incomplete; inspect each result's assumptions and unsupported inputs."
	}
	return ConnectivityChange{
		Before:        beforeResult.Status,
		After:         afterResult.Status,
		Change:        change,
		BeforeResult:  beforeResult,
		AfterResult:   afterResult,
		PolicyChanges: comparePolicySpecs(before, after),
		Explanation:   explanation,
	}
}

func comparePolicySpecs(before, after []networkingv1.NetworkPolicy) []PolicyChangeEvidence {
	beforeByKey := make(map[string]networkingv1.NetworkPolicy, len(before))
	afterByKey := make(map[string]networkingv1.NetworkPolicy, len(after))
	for _, policy := range before {
		beforeByKey[policyKey(policy)] = policy
	}
	for _, policy := range after {
		afterByKey[policyKey(policy)] = policy
	}
	keys := sortedKeys(beforeByKey, afterByKey)
	changes := make([]PolicyChangeEvidence, 0)
	for _, key := range keys {
		oldPolicy, hadBefore := beforeByKey[key]
		newPolicy, hasAfter := afterByKey[key]
		switch {
		case !hadBefore:
			changes = append(changes, PolicyChangeEvidence{
				Policy: key, Change: "added",
				FieldChanges: []PolicyFieldChange{{Field: "policySpec", Before: json.RawMessage("null"), After: marshalJSONValue(normalizedPolicySpec(newPolicy))}},
			})
		case !hasAfter:
			changes = append(changes, PolicyChangeEvidence{
				Policy: key, Change: "removed",
				FieldChanges: []PolicyFieldChange{{Field: "policySpec", Before: marshalJSONValue(normalizedPolicySpec(oldPolicy)), After: json.RawMessage("null")}},
			})
		default:
			fieldChanges := changedPolicyFieldDetails(oldPolicy, newPolicy)
			if len(fieldChanges) > 0 {
				fields := make([]string, 0, len(fieldChanges))
				for _, fieldChange := range fieldChanges {
					fields = append(fields, fieldChange.Field)
				}
				changes = append(changes, PolicyChangeEvidence{
					Policy: key, Change: "modified", ChangedFields: fields, FieldChanges: fieldChanges,
				})
			}
		}
	}
	return changes
}

func evaluateDirection(policies []networkingv1.NetworkPolicy, query ConnectivityQuery, protocol, direction string) DirectionResult {
	result := DirectionResult{Direction: strings.ToLower(direction), Status: AllowedByModel}
	var endpoint Endpoint
	if direction == "Ingress" {
		endpoint = query.Destination
	} else {
		endpoint = query.Source
	}

	uncertainSelection := false
	var applicable []networkingv1.NetworkPolicy
	for _, policy := range policies {
		if policy.Namespace != endpoint.Namespace {
			continue
		}
		applies, typesKnown := policyAppliesToDirection(policy, direction)
		if !typesKnown {
			uncertainSelection = true
			result.Unsupported = append(result.Unsupported, fmt.Sprintf("policy %s/%s has unsupported policyTypes", policy.Namespace, policy.Name))
			continue
		}
		if !applies {
			continue
		}
		matched, known := selectorMatches(policy.Spec.PodSelector, endpoint.Labels, endpoint.LabelsComplete)
		if !known {
			uncertainSelection = true
			result.Unsupported = append(result.Unsupported, fmt.Sprintf("workload labels are incomplete for %s/%s; applicability of policy %s/%s is unknown", endpoint.Namespace, endpoint.Name, policy.Namespace, policy.Name))
			continue
		}
		if matched {
			applicable = append(applicable, policy)
			result.ApplicablePolicies = append(result.ApplicablePolicies, policy.Namespace+"/"+policy.Name)
		}
	}
	result.Isolated = len(applicable) > 0 || uncertainSelection
	if uncertainSelection && len(applicable) == 0 {
		result.Status = Unknown
		result.Reason = "Policy applicability is uncertain because input labels or policy types are incomplete or unsupported."
		return finishDirection(result)
	}
	if len(applicable) == 0 {
		if !query.PolicyInventoryComplete {
			result.Status = Unknown
			result.Reason = "No applicable policy was found in an incomplete policy inventory."
			result.Unsupported = append(result.Unsupported, "policy inventory completeness was not established")
			return finishDirection(result)
		}
		result.Status = AllowedByModel
		result.Reason = "No policy isolates this endpoint in this direction; Kubernetes defaults to allow."
		return finishDirection(result)
	}

	allowed := false
	uncertainRule := false
	for _, policy := range applicable {
		ruleCount := len(policy.Spec.Ingress)
		if direction == "Egress" {
			ruleCount = len(policy.Spec.Egress)
		}
		for ruleIndex := 0; ruleIndex < ruleCount; ruleIndex++ {
			ruleID := fmt.Sprintf("%s/%s:%s[%d]", policy.Namespace, policy.Name, strings.ToLower(direction), ruleIndex)
			result.EvaluatedRules = append(result.EvaluatedRules, ruleID)
			var peers []networkingv1.NetworkPolicyPeer
			var ports []networkingv1.NetworkPolicyPort
			if direction == "Ingress" {
				peers = policy.Spec.Ingress[ruleIndex].From
				ports = policy.Spec.Ingress[ruleIndex].Ports
			} else {
				peers = policy.Spec.Egress[ruleIndex].To
				ports = policy.Spec.Egress[ruleIndex].Ports
			}
			ruleResult := DirectionResult{}
			peerMatch := peerRulesMatch(peers, query, direction, policy.Namespace, &ruleResult)
			if peerMatch == matchNo {
				continue
			}
			ruleUncertain := false
			portMatch := portsMatch(ports, query, protocol, &ruleResult, &ruleUncertain)
			if portMatch == matchNo {
				continue
			}
			if peerMatch == matchUnknown || portMatch == matchUnknown {
				uncertainRule = true
				result.Unsupported = append(result.Unsupported, ruleResult.Unsupported...)
				continue
			}
			if peerMatch == matchYes && portMatch == matchYes {
				allowed = true
				result.MatchedRules = append(result.MatchedRules, ruleID)
			}
			uncertainRule = uncertainRule || ruleUncertain
		}
	}
	if allowed {
		result.Status = AllowedByModel
		result.Reason = "At least one rule in the additive set of applicable policies allows this direction."
	} else if uncertainSelection {
		result.Status = Unknown
		result.Reason = "A policy may additionally isolate or allow this direction, but policy applicability is uncertain."
	} else if uncertainRule {
		result.Status = Unknown
		result.Reason = "Applicable policies isolate this direction, but one or more rule inputs are unsupported or incomplete."
	} else if !query.PolicyInventoryComplete {
		result.Status = Unknown
		result.Reason = "Known policies isolate this direction without a matching allow rule, but the policy inventory is incomplete."
		result.Unsupported = append(result.Unsupported, "policy inventory completeness was not established")
	} else {
		result.Status = DeniedByModel
		result.Reason = "Applicable policies isolate this direction and none of their rules match."
	}
	return finishDirection(result)
}

type matchResult int

const (
	matchNo matchResult = iota
	matchYes
	matchUnknown
)

func peerRulesMatch(peers []networkingv1.NetworkPolicyPeer, query ConnectivityQuery, direction, policyNamespace string, result *DirectionResult) matchResult {
	if len(peers) == 0 {
		return matchYes
	}
	unknown := false
	for _, peer := range peers {
		matched := matchPeer(peer, query, direction, policyNamespace, result)
		if matched == matchYes {
			return matchYes
		}
		if matched == matchUnknown {
			unknown = true
		}
	}
	if unknown {
		return matchUnknown
	}
	return matchNo
}

func matchPeer(peer networkingv1.NetworkPolicyPeer, query ConnectivityQuery, direction, policyNamespace string, result *DirectionResult) matchResult {
	if peer.IPBlock != nil {
		if peer.PodSelector != nil || peer.NamespaceSelector != nil {
			result.Unsupported = append(result.Unsupported, "ipBlock cannot be combined with podSelector or namespaceSelector")
			return matchUnknown
		}
		endpoint := query.Source
		if direction == "Egress" {
			endpoint = query.Destination
		}
		if !endpoint.IPBlockAddressKnown {
			result.Unsupported = append(result.Unsupported, "ipBlock evaluation requires the enforcement-point address and confirmation that the node-traffic exception does not apply")
			return matchUnknown
		}
		if endpoint.IP == "" {
			result.Unsupported = append(result.Unsupported, "endpoint IP is required to evaluate an ipBlock peer")
			return matchUnknown
		}
		result.Assumptions = append(result.Assumptions, "ipBlock matching uses the caller-asserted address seen by NetworkPolicy enforcement; Service/NAT rewriting and node exceptions are not independently discovered")
		address, err := netip.ParseAddr(endpoint.IP)
		if err != nil {
			result.Unsupported = append(result.Unsupported, "endpoint IP is not a valid address")
			return matchUnknown
		}
		cidr, err := netip.ParsePrefix(peer.IPBlock.CIDR)
		if err != nil {
			result.Unsupported = append(result.Unsupported, "ipBlock contains an invalid CIDR")
			return matchUnknown
		}
		exceptPrefixes := make([]netip.Prefix, 0, len(peer.IPBlock.Except))
		for _, excluded := range peer.IPBlock.Except {
			prefix, err := netip.ParsePrefix(excluded)
			if err != nil {
				result.Unsupported = append(result.Unsupported, "ipBlock contains an invalid except CIDR")
				return matchUnknown
			}
			if prefix.Addr().Is4() != cidr.Addr().Is4() || prefix.Bits() < cidr.Bits() ||
				!cidr.Contains(prefix.Addr()) {
				result.Unsupported = append(result.Unsupported, "ipBlock except CIDR is not contained within its parent CIDR")
				return matchUnknown
			}
			exceptPrefixes = append(exceptPrefixes, prefix)
		}
		if !cidr.Contains(address) {
			return matchNo
		}
		for _, prefix := range exceptPrefixes {
			if prefix.Contains(address) {
				return matchNo
			}
		}
		return matchYes
	}

	endpoint := query.Source
	if direction == "Egress" {
		endpoint = query.Destination
	}
	if peer.PodSelector == nil && peer.NamespaceSelector == nil {
		result.Unsupported = append(result.Unsupported, "empty NetworkPolicy peer is ambiguous")
		return matchUnknown
	}
	if peer.PodSelector != nil {
		if peer.NamespaceSelector == nil && endpoint.Namespace != policyNamespace {
			return matchNo
		}
		podMatch, known := selectorMatches(*peer.PodSelector, endpoint.Labels, endpoint.LabelsComplete)
		if !known {
			result.Unsupported = append(result.Unsupported, fmt.Sprintf("labels are incomplete for peer endpoint %s/%s", endpoint.Namespace, endpoint.Name))
			return matchUnknown
		}
		if !podMatch {
			return matchNo
		}
	}
	if peer.NamespaceSelector != nil {
		namespaceMatch, known := selectorMatches(*peer.NamespaceSelector, endpoint.NamespaceLabels, endpoint.NamespaceLabelsKnown)
		if !known {
			result.Unsupported = append(result.Unsupported, fmt.Sprintf("namespace labels are unavailable for %s", endpoint.Namespace))
			return matchUnknown
		}
		if !namespaceMatch {
			return matchNo
		}
	}
	return matchYes
}

func portsMatch(ports []networkingv1.NetworkPolicyPort, query ConnectivityQuery, protocol string, result *DirectionResult, uncertain *bool) matchResult {
	if len(ports) == 0 {
		return matchYes
	}
	unknown := false
	for _, policyPort := range ports {
		portProtocol := string(corev1.ProtocolTCP)
		if policyPort.Protocol != nil {
			portProtocol = string(*policyPort.Protocol)
		}
		if portProtocol != string(corev1.ProtocolTCP) && portProtocol != string(corev1.ProtocolUDP) && portProtocol != string(corev1.ProtocolSCTP) {
			*uncertain = true
			result.Unsupported = append(result.Unsupported, fmt.Sprintf("policy uses unsupported protocol %q", portProtocol))
			unknown = true
			continue
		}
		if !strings.EqualFold(portProtocol, protocol) {
			continue
		}
		if policyPort.Port == nil {
			if policyPort.EndPort != nil {
				*uncertain = true
				result.Unsupported = append(result.Unsupported, "endPort cannot be evaluated without a numeric port")
				return matchUnknown
			}
			return matchYes
		}
		if policyPort.Port.Type == intstr.Int {
			start := policyPort.Port.IntVal
			end := start
			if policyPort.EndPort != nil {
				end = *policyPort.EndPort
			}
			if start < 1 || start > 65535 || end < start || end > 65535 {
				*uncertain = true
				result.Unsupported = append(result.Unsupported, "numeric port or endPort range is outside the supported 1-65535 range or has an endPort before its start")
				return matchUnknown
			}
			if query.Port >= start && query.Port <= end {
				return matchYes
			}
			continue
		}
		if policyPort.Port.Type == intstr.String {
			if policyPort.EndPort != nil {
				*uncertain = true
				result.Unsupported = append(result.Unsupported, "endPort cannot be combined with a named port")
				return matchUnknown
			}
			portName := policyPort.Port.StrVal
			resolved, ok := query.Destination.NamedPorts[portName]
			if !ok {
				if !query.Destination.NamedPortsComplete {
					unknown = true
					result.Unsupported = append(result.Unsupported, fmt.Sprintf("named port %q cannot be resolved on the destination workload", portName))
				}
				continue
			}
			namedProtocol := strings.ToUpper(resolved.Protocol)
			if namedProtocol == "" {
				namedProtocol = string(corev1.ProtocolTCP)
			}
			if resolved.Port < 1 || resolved.Port > 65535 || (namedProtocol != string(corev1.ProtocolTCP) && namedProtocol != string(corev1.ProtocolUDP) && namedProtocol != string(corev1.ProtocolSCTP)) {
				unknown = true
				result.Unsupported = append(result.Unsupported, fmt.Sprintf("named port %q has incomplete or unsupported workload metadata", portName))
				continue
			}
			if resolved.Port == query.Port && namedProtocol == protocol {
				return matchYes
			}
		}
		if policyPort.Port.Type != intstr.Int && policyPort.Port.Type != intstr.String {
			*uncertain = true
			result.Unsupported = append(result.Unsupported, "policy port has an unsupported value type")
			unknown = true
		}
	}
	if unknown {
		*uncertain = true
		return matchUnknown
	}
	return matchNo
}

func selectorMatches(selector metav1.LabelSelector, values map[string]string, complete bool) (bool, bool) {
	compiled, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil {
		return false, false
	}
	if compiled.Empty() && len(selector.MatchLabels) == 0 && len(selector.MatchExpressions) == 0 {
		return true, true
	}
	if !complete {
		return false, false
	}
	return compiled.Matches(labels.Set(values)), true
}

func policyAppliesToDirection(policy networkingv1.NetworkPolicy, direction string) (bool, bool) {
	applies := false
	for _, policyType := range effectivePolicyTypes(policy) {
		switch policyType {
		case networkingv1.PolicyTypeIngress:
			applies = applies || direction == "Ingress"
		case networkingv1.PolicyTypeEgress:
			applies = applies || direction == "Egress"
		default:
			return false, false
		}
	}
	return applies, true
}

func validProtocol(protocol string) bool {
	return protocol == string(corev1.ProtocolTCP) ||
		protocol == string(corev1.ProtocolUDP) ||
		protocol == string(corev1.ProtocolSCTP)
}

func endpointID(endpoint Endpoint) string {
	return strings.Trim(endpoint.Namespace+"/"+endpoint.Name, "/")
}

func finishDirection(result DirectionResult) DirectionResult {
	result.ApplicablePolicies = uniqueSorted(result.ApplicablePolicies)
	result.EvaluatedRules = uniqueSorted(result.EvaluatedRules)
	result.MatchedRules = uniqueSorted(result.MatchedRules)
	result.Assumptions = uniqueSorted(result.Assumptions)
	result.Unsupported = uniqueSorted(result.Unsupported)
	return result
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
