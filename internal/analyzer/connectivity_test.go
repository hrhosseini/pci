package analyzer

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestNetworkPolicyConnectivitySemantics(t *testing.T) {
	query := ConnectivityQuery{
		Source: Endpoint{
			Namespace: "client", Name: "web", Labels: map[string]string{"app": "web"}, LabelsComplete: true,
			NamespaceLabels: map[string]string{"team": "frontend"}, NamespaceLabelsKnown: true, IP: "10.1.0.5", IPBlockAddressKnown: true,
		},
		Destination: Endpoint{
			Namespace: "server", Name: "api", Labels: map[string]string{"app": "api"}, LabelsComplete: true,
			NamespaceLabels: map[string]string{"team": "backend"}, NamespaceLabelsKnown: true, IP: "10.2.0.8", IPBlockAddressKnown: true,
		},
		Protocol: "TCP", Port: 8080, PolicyInventoryComplete: true,
	}

	tests := []struct {
		name     string
		policies []networkingv1.NetworkPolicy
		query    ConnectivityQuery
		want     TrafficStatus
		ingress  TrafficStatus
		egress   TrafficStatus
	}{
		{
			name: "no policy defaults to allow",
			want: AllowedByModel, ingress: AllowedByModel, egress: AllowedByModel,
		},
		{
			name:  "incomplete policy inventory does not imply default allow",
			query: withIncompleteInventory(query),
			want:  Unknown, ingress: Unknown, egress: Unknown,
		},
		{
			name:  "host network endpoint remains unsupported",
			query: withHostNetworkSource(query),
			want:  Unknown, ingress: Unknown, egress: Unknown,
		},
		{
			name:     "policy types inferred ingress only when egress omitted",
			policies: []networkingv1.NetworkPolicy{policy("server", "ingress-only", selector("app", "api"), nil, nil, nil)},
			want:     DeniedByModel, ingress: DeniedByModel, egress: AllowedByModel,
		},
		{
			name:     "egress inferred when egress rule exists",
			policies: []networkingv1.NetworkPolicy{policy("client", "egress", selector("app", "web"), nil, []networkingv1.NetworkPolicyEgressRule{{}}, nil)},
			want:     AllowedByModel, ingress: AllowedByModel, egress: AllowedByModel,
		},
		{
			name: "explicit ingress and egress isolate independently",
			policies: []networkingv1.NetworkPolicy{
				policy("server", "ingress", selector("app", "api"), nil, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}),
				policy("client", "egress", selector("app", "web"), nil, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}),
			},
			want: DeniedByModel, ingress: DeniedByModel, egress: DeniedByModel,
		},
		{
			name: "policies combine additively rather than first match",
			policies: []networkingv1.NetworkPolicy{
				policy("server", "deny-empty", selector("app", "api"), nil, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}),
				policy("server", "allow-web", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
					From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: ptrSelector("team", "frontend"), PodSelector: ptrSelector("app", "web")}},
				}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}),
			},
			want: AllowedByModel, ingress: AllowedByModel, egress: AllowedByModel,
		},
		{
			name:  "same namespace pod selector peer matches",
			query: withSourceNamespace(query, "server"),
			policies: []networkingv1.NetworkPolicy{policy("server", "peer", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{PodSelector: ptrSelector("app", "web")}},
			}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			want: AllowedByModel, ingress: AllowedByModel, egress: AllowedByModel,
		},
		{
			name: "pod selector peer does not cross namespace boundary",
			policies: []networkingv1.NetworkPolicy{policy("server", "peer", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{PodSelector: ptrSelector("app", "web")}},
			}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			want: DeniedByModel, ingress: DeniedByModel, egress: AllowedByModel,
		},
		{
			name: "namespace selector plus pod selector intersects",
			policies: []networkingv1.NetworkPolicy{policy("server", "combined", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: ptrSelector("team", "frontend"), PodSelector: ptrSelector("app", "web")}},
			}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			want: AllowedByModel, ingress: AllowedByModel, egress: AllowedByModel,
		},
		{
			name: "namespace selector alone matches all pods in selected namespace",
			policies: []networkingv1.NetworkPolicy{policy("server", "namespace", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: ptrSelector("team", "frontend")}},
			}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			want: AllowedByModel, ingress: AllowedByModel, egress: AllowedByModel,
		},
		{
			name:     "empty target selector selects every pod in policy namespace",
			policies: []networkingv1.NetworkPolicy{policy("server", "all-targets", metav1.LabelSelector{}, []networkingv1.NetworkPolicyIngressRule{{}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			want:     AllowedByModel, ingress: AllowedByModel, egress: AllowedByModel,
		},
		{
			name: "protocol mismatch denies isolated traffic",
			policies: []networkingv1.NetworkPolicy{policy("server", "udp-only", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: ptrProtocol(corev1.ProtocolUDP)}},
			}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			want: DeniedByModel, ingress: DeniedByModel, egress: AllowedByModel,
		},
		{
			name: "omitted network policy port matches all ports for protocol",
			policies: []networkingv1.NetworkPolicy{policy("server", "all-tcp-ports", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: ptrProtocol(corev1.ProtocolTCP)}},
			}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			want: AllowedByModel, ingress: AllowedByModel, egress: AllowedByModel,
		},
		{
			name:  "named port resolves against destination workload",
			query: withNamedPort(query, "http", 8080, "TCP"),
			policies: []networkingv1.NetworkPolicy{policy("server", "named", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
				Ports: []networkingv1.NetworkPolicyPort{{Port: ptrIntOrString(intstr.FromString("http"))}},
			}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			want: AllowedByModel, ingress: AllowedByModel, egress: AllowedByModel,
		},
		{
			name: "unresolved named port remains unknown",
			policies: []networkingv1.NetworkPolicy{policy("server", "named", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
				Ports: []networkingv1.NetworkPolicyPort{{Port: ptrIntOrString(intstr.FromString("http"))}},
			}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			want: Unknown, ingress: Unknown, egress: AllowedByModel,
		},
		{
			name: "unresolved named port on a nonmatching peer is irrelevant",
			policies: []networkingv1.NetworkPolicy{policy("server", "peer-and-name", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
				From:  []networkingv1.NetworkPolicyPeer{{PodSelector: ptrSelector("app", "different-client")}},
				Ports: []networkingv1.NetworkPolicyPort{{Port: ptrIntOrString(intstr.FromString("missing-port"))}},
			}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			want: DeniedByModel, ingress: DeniedByModel, egress: AllowedByModel,
		},
		{
			name:  "named port protocol mismatch does not allow traffic",
			query: withNamedPort(query, "http", 8080, "UDP"),
			policies: []networkingv1.NetworkPolicy{policy("server", "named", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
				Ports: []networkingv1.NetworkPolicyPort{{Port: ptrIntOrString(intstr.FromString("http"))}},
			}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			want: DeniedByModel, ingress: DeniedByModel, egress: AllowedByModel,
		},
		{
			name:     "unsupported policy type remains unknown",
			policies: []networkingv1.NetworkPolicy{policy("server", "unsupported-type", selector("app", "api"), nil, nil, []networkingv1.PolicyType{"Foo"})},
			want:     Unknown, ingress: Unknown, egress: AllowedByModel,
		},
		{
			name: "ipBlock cidr matches source address",
			policies: []networkingv1.NetworkPolicy{policy("server", "cidr", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "10.1.0.0/16"}}},
			}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			want: AllowedByModel, ingress: AllowedByModel, egress: AllowedByModel,
		},
		{
			name: "ipBlock except excludes matching address",
			policies: []networkingv1.NetworkPolicy{policy("server", "cidr-except", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "10.1.0.0/16", Except: []string{"10.1.0.0/24"}}}},
			}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			want: DeniedByModel, ingress: DeniedByModel, egress: AllowedByModel,
		},
		{
			name:     "missing workload labels propagate unknown",
			query:    withIncompleteTargetLabels(query),
			policies: []networkingv1.NetworkPolicy{policy("server", "target", selector("app", "api"), nil, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			want:     Unknown, ingress: Unknown, egress: AllowedByModel,
		},
		{
			name:  "namespace selector without namespace labels is unknown",
			query: withUnknownSourceNamespaceLabels(query),
			policies: []networkingv1.NetworkPolicy{policy("server", "namespace", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: ptrSelector("team", "frontend")}},
			}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			want: Unknown, ingress: Unknown, egress: AllowedByModel,
		},
		{
			name: "match expressions support In and Exists",
			policies: []networkingv1.NetworkPolicy{policy("server", "expressions", metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: "app", Operator: metav1.LabelSelectorOpIn, Values: []string{"api", "worker"}},
					{Key: "tier", Operator: metav1.LabelSelectorOpExists},
				},
			}, []networkingv1.NetworkPolicyIngressRule{{}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			query: withTargetLabels(query, map[string]string{"app": "api", "tier": "backend"}),
			want:  AllowedByModel, ingress: AllowedByModel, egress: AllowedByModel,
		},
		{
			name: "match expressions support NotIn and DoesNotExist",
			policies: []networkingv1.NetworkPolicy{policy("server", "negative-expressions", metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: "tier", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"frontend"}},
					{Key: "deprecated", Operator: metav1.LabelSelectorOpDoesNotExist},
				},
			}, []networkingv1.NetworkPolicyIngressRule{{}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})},
			query: withTargetLabels(query, map[string]string{"app": "api", "tier": "backend"}),
			want:  AllowedByModel, ingress: AllowedByModel, egress: AllowedByModel,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testQuery := query
			if test.query.Source.Name != "" {
				testQuery = test.query
			}
			got := EvaluateConnectivity(test.policies, testQuery)
			if got.Status != test.want || got.Ingress.Status != test.ingress || got.Egress.Status != test.egress {
				t.Fatalf("got status=%s ingress=%s egress=%s; want status=%s ingress=%s egress=%s; result=%#v",
					got.Status, got.Ingress.Status, got.Egress.Status, test.want, test.ingress, test.egress, got)
			}
		})
	}
}

func TestCompareConnectivityTransitions(t *testing.T) {
	query := ConnectivityQuery{
		Source:      Endpoint{Namespace: "client", Name: "web", Labels: map[string]string{"app": "web"}, LabelsComplete: true},
		Destination: Endpoint{Namespace: "server", Name: "api", Labels: map[string]string{"app": "api"}, LabelsComplete: true},
		Protocol:    "TCP", Port: 8080, PolicyInventoryComplete: true,
	}
	deny := []networkingv1.NetworkPolicy{policy("server", "deny", selector("app", "api"), nil, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})}
	allow := []networkingv1.NetworkPolicy{policy("server", "allow", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})}

	if change := CompareConnectivity(deny, allow, query); change.Change != "newly_allowed" {
		t.Fatalf("expected newly allowed connectivity, got %#v", change)
	}
	if change := CompareConnectivity(allow, deny, query); change.Change != "newly_denied" {
		t.Fatalf("expected newly denied connectivity, got %#v", change)
	}
	incompleteQuery := withIncompleteInventory(query)
	if change := CompareConnectivity(nil, nil, incompleteQuery); change.Change != "uncertain" {
		t.Fatalf("equal unknown states must remain uncertain, got %#v", change)
	}
}

func TestConnectivityDiffExplainsRuleEdit(t *testing.T) {
	query := ConnectivityQuery{
		Source:      Endpoint{Namespace: "server", Name: "web", Labels: map[string]string{"app": "web"}, LabelsComplete: true},
		Destination: Endpoint{Namespace: "server", Name: "api", Labels: map[string]string{"app": "api"}, LabelsComplete: true},
		Protocol:    "TCP", Port: 8080, PolicyInventoryComplete: true,
	}
	before := []networkingv1.NetworkPolicy{policy("server", "allow-web", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
		From:  []networkingv1.NetworkPolicyPeer{{PodSelector: ptrSelector("app", "web")}},
		Ports: []networkingv1.NetworkPolicyPort{{Port: ptrIntOrString(intstr.FromInt(8080))}},
	}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})}
	after := []networkingv1.NetworkPolicy{policy("server", "allow-web", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
		From:  []networkingv1.NetworkPolicyPeer{{PodSelector: ptrSelector("app", "web")}},
		Ports: []networkingv1.NetworkPolicyPort{{Port: ptrIntOrString(intstr.FromInt(8443))}},
	}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})}

	change := CompareConnectivity(before, after, query)
	if change.Change != "newly_denied" || change.Before != AllowedByModel || change.After != DeniedByModel {
		t.Fatalf("expected modeled allow-to-deny transition, got %#v", change)
	}
	if change.BeforeResult.Ingress.MatchedRules == nil || len(change.BeforeResult.Ingress.MatchedRules) != 1 {
		t.Fatalf("before evidence should identify the matching ingress rule: %#v", change.BeforeResult.Ingress)
	}
	if len(change.PolicyChanges) != 1 || change.PolicyChanges[0].Policy != "server/allow-web" ||
		change.PolicyChanges[0].Change != "modified" || !containsString(change.PolicyChanges[0].ChangedFields, "ingressRules") {
		t.Fatalf("expected changed ingress rule evidence, got %#v", change.PolicyChanges)
	}
	fieldChanges := change.PolicyChanges[0].FieldChanges
	if len(fieldChanges) != 1 || !strings.Contains(string(fieldChanges[0].Before), `"port":8080`) ||
		!strings.Contains(string(fieldChanges[0].After), `"port":8443`) {
		t.Fatalf("policy delta should preserve exact before/after ingress rule values: %#v", fieldChanges)
	}
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func TestConnectivityHandlesSelfTrafficAndPortRanges(t *testing.T) {
	selfQuery := ConnectivityQuery{
		Source:      Endpoint{Namespace: "server", Name: "api", LabelsComplete: true},
		Destination: Endpoint{Namespace: "server", Name: "api", LabelsComplete: true},
		Protocol:    "TCP", Port: 8080, PolicyInventoryComplete: true,
	}
	denyAll := []networkingv1.NetworkPolicy{policy("server", "deny", metav1.LabelSelector{}, nil, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress})}
	if got := EvaluateConnectivity(denyAll, selfQuery); got.Status != AllowedByModel {
		t.Fatalf("self traffic should not be blocked by NetworkPolicy, got %#v", got)
	}

	query := ConnectivityQuery{
		Source:      Endpoint{Namespace: "client", Name: "web", Labels: map[string]string{"app": "web"}, LabelsComplete: true},
		Destination: Endpoint{Namespace: "server", Name: "api", Labels: map[string]string{"app": "api"}, LabelsComplete: true},
		Protocol:    "TCP", Port: 8081, PolicyInventoryComplete: true,
	}
	end := int32(8082)
	portRange := []networkingv1.NetworkPolicy{policy("server", "range", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
		Ports: []networkingv1.NetworkPolicyPort{{Port: ptrIntOrString(intstr.FromInt(8080)), EndPort: &end}},
	}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})}
	if got := EvaluateConnectivity(portRange, query); got.Ingress.Status != AllowedByModel {
		t.Fatalf("port range should include 8081: %#v", got.Ingress)
	}
	query.Port = 8083
	if got := EvaluateConnectivity(portRange, query); got.Ingress.Status != DeniedByModel {
		t.Fatalf("port range should not include 8083: %#v", got.Ingress)
	}
}

func TestMalformedPortRangesAndIPBlockExceptionsAreUnknown(t *testing.T) {
	query := ConnectivityQuery{
		Source:      Endpoint{Namespace: "client", Name: "web", Labels: map[string]string{"app": "web"}, LabelsComplete: true, IP: "10.1.0.5"},
		Destination: Endpoint{Namespace: "server", Name: "api", Labels: map[string]string{"app": "api"}, LabelsComplete: true, IP: "10.2.0.8"},
		Protocol:    "TCP", Port: 8080, PolicyInventoryComplete: true,
	}
	reversedEnd := int32(8079)
	cases := []struct {
		name   string
		policy networkingv1.NetworkPolicy
	}{
		{
			name: "endPort before start",
			policy: policy("server", "invalid-range", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
				Ports: []networkingv1.NetworkPolicyPort{{Port: ptrIntOrString(intstr.FromInt(8080)), EndPort: &reversedEnd}},
			}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}),
		},
		{
			name: "except range outside parent CIDR",
			policy: policy("server", "invalid-except", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "10.1.0.0/16", Except: []string{"10.3.0.0/24"}}}},
			}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}),
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := EvaluateConnectivity([]networkingv1.NetworkPolicy{test.policy}, query)
			if got.Ingress.Status != Unknown || len(got.Ingress.Unsupported) == 0 {
				t.Fatalf("malformed rule must remain explicit unknown, got %#v", got.Ingress)
			}
		})
	}
}

func TestIPBlockRequiresEnforcementPointAddressMetadata(t *testing.T) {
	query := ConnectivityQuery{
		Source:      Endpoint{Namespace: "client", Name: "web", IP: "10.1.0.5", Labels: map[string]string{"app": "web"}, LabelsComplete: true},
		Destination: Endpoint{Namespace: "server", Name: "api", IP: "10.2.0.8", Labels: map[string]string{"app": "api"}, LabelsComplete: true},
		Protocol:    "TCP", Port: 8080, PolicyInventoryComplete: true,
	}
	policies := []networkingv1.NetworkPolicy{policy("server", "external-ingress", selector("app", "api"), []networkingv1.NetworkPolicyIngressRule{{
		From: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "10.1.0.0/16"}}},
	}}, nil, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress})}

	got := EvaluateConnectivity(policies, query)
	if got.Ingress.Status != Unknown || len(got.Ingress.Unsupported) == 0 {
		t.Fatalf("Pod/service IP ambiguity must not be converted into an ipBlock match: %#v", got.Ingress)
	}
}

func policy(namespace, name string, selector metav1.LabelSelector, ingress []networkingv1.NetworkPolicyIngressRule, egress []networkingv1.NetworkPolicyEgressRule, types []networkingv1.PolicyType) networkingv1.NetworkPolicy {
	return networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       networkingv1.NetworkPolicySpec{PodSelector: selector, Ingress: ingress, Egress: egress, PolicyTypes: types},
	}
}

func selector(key, value string) metav1.LabelSelector {
	return metav1.LabelSelector{MatchLabels: map[string]string{key: value}}
}

func ptrSelector(key, value string) *metav1.LabelSelector {
	selected := selector(key, value)
	return &selected
}

func ptrProtocol(protocol corev1.Protocol) *corev1.Protocol      { return &protocol }
func ptrIntOrString(port intstr.IntOrString) *intstr.IntOrString { return &port }

func withSourceNamespace(query ConnectivityQuery, namespace string) ConnectivityQuery {
	query.Source.Namespace = namespace
	return query
}

func withNamedPort(query ConnectivityQuery, name string, port int32, protocol string) ConnectivityQuery {
	query.Destination.NamedPortsComplete = true
	query.Destination.NamedPorts = map[string]NamedPort{name: {Port: port, Protocol: protocol}}
	return query
}

func withIncompleteTargetLabels(query ConnectivityQuery) ConnectivityQuery {
	query.Destination.LabelsComplete = false
	return query
}

func withUnknownSourceNamespaceLabels(query ConnectivityQuery) ConnectivityQuery {
	query.Source.NamespaceLabelsKnown = false
	query.Source.NamespaceLabels = nil
	return query
}

func withTargetLabels(query ConnectivityQuery, labels map[string]string) ConnectivityQuery {
	query.Destination.Labels = labels
	return query
}

func withIncompleteInventory(query ConnectivityQuery) ConnectivityQuery {
	query.PolicyInventoryComplete = false
	return query
}

func withHostNetworkSource(query ConnectivityQuery) ConnectivityQuery {
	query.Source.HostNetwork = true
	return query
}
