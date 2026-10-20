package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLiveHarnessPolicyScenariosHaveExpectedModelResults(t *testing.T) {
	manifest := func(names ...string) string {
		t.Helper()
		combined := ""
		for _, name := range names {
			data, err := os.ReadFile(filepath.Join("..", "..", "tests", "live", "policies", name+".yaml"))
			if err != nil {
				t.Fatalf("read scenario policy %q: %v", name, err)
			}
			if combined != "" {
				combined += "\n---\n"
			}
			combined += string(data)
		}
		return combined
	}
	query := func(sourceNamespace, sourceName, sourceRole, sourceNamespaceRole string, port int32) ConnectivityQuery {
		return ConnectivityQuery{
			Source: Endpoint{
				Namespace: sourceNamespace, Name: sourceName,
				Labels:         map[string]string{"app": "client", "role": sourceRole},
				LabelsComplete: true, NamespaceLabels: map[string]string{"pci-phase6-role": sourceNamespaceRole},
				NamespaceLabelsKnown: true, NamedPortsComplete: true,
			},
			Destination: Endpoint{
				Namespace: "pci-phase6-destination", Name: "server",
				Labels: map[string]string{"app": "server"}, LabelsComplete: true,
				NamespaceLabels:      map[string]string{"pci-phase6-role": "destination"},
				NamespaceLabelsKnown: true, NamedPortsComplete: true,
			},
			Protocol: "TCP", Port: port, PolicyInventoryComplete: true,
		}
	}

	tests := []struct {
		name     string
		policies []string
		query    ConnectivityQuery
		want     TrafficStatus
	}{
		{name: "unselected ingress policy leaves baseline allowed", policies: []string{"baseline-no-isolation"}, query: query("pci-phase6-source", "client-a", "client-a", "source", 80), want: AllowedByModel},
		{name: "ingress isolation denies unmatched flow", policies: []string{"ingress-deny"}, query: query("pci-phase6-source", "client-a", "client-a", "source", 80), want: DeniedByModel},
		{name: "combined namespace and pod selectors allow", policies: []string{"ingress-deny", "ingress-allow-a"}, query: query("pci-phase6-source", "client-a", "client-a", "source", 80), want: AllowedByModel},
		{name: "additive policies allow source B", policies: []string{"ingress-deny", "ingress-allow-a", "ingress-allow-b"}, query: query("pci-phase6-source", "client-b", "client-b", "source", 80), want: AllowedByModel},
		{name: "namespace selector boundary denies unrelated namespace", policies: []string{"ingress-deny", "ingress-allow-a"}, query: query("pci-phase6-unrelated", "outsider", "client-a", "unrelated", 80), want: DeniedByModel},
		{name: "egress isolation denies unmatched flow", policies: []string{"egress-deny"}, query: query("pci-phase6-source", "client-a", "client-a", "source", 80), want: DeniedByModel},
		{name: "egress rule allows matching namespace and pod", policies: []string{"egress-allow"}, query: query("pci-phase6-source", "client-a", "client-a", "source", 80), want: AllowedByModel},
		{name: "both ingress and egress rules allow the flow", policies: []string{"ingress-deny", "ingress-allow-a", "egress-deny", "egress-allow"}, query: query("pci-phase6-source", "client-a", "client-a", "source", 80), want: AllowedByModel},
		{name: "port mismatch denies the flow", policies: []string{"ingress-port-mismatch"}, query: query("pci-phase6-source", "client-a", "client-a", "source", 80), want: DeniedByModel},
		{name: "protocol mismatch denies the flow", policies: []string{"ingress-protocol-mismatch"}, query: query("pci-phase6-source", "client-a", "client-a", "source", 80), want: DeniedByModel},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policies, err := ParseNetworkPolicies(manifest(test.policies...))
			if err != nil {
				t.Fatalf("ParseNetworkPolicies returned error: %v", err)
			}
			result := EvaluateConnectivity(policies, test.query)
			if result.Status != test.want {
				t.Fatalf("EvaluateConnectivity() = %q, want %q; reason: %s", result.Status, test.want, result.Reason)
			}
		})
	}

	diffTests := []struct {
		name   string
		before []string
		after  []string
		want   string
	}{
		{name: "baseline ingress isolation denies", before: []string{"baseline-no-isolation"}, after: []string{"ingress-deny"}, want: "newly_denied"},
		{name: "additive ingress rule allows", before: []string{"ingress-deny"}, after: []string{"ingress-deny", "ingress-allow-a"}, want: "newly_allowed"},
		{name: "unselected policy has no modeled effect", before: []string{"baseline-no-isolation"}, after: []string{"baseline-no-isolation"}, want: "unchanged"},
		{name: "egress isolation denies", before: []string{"baseline-no-isolation"}, after: []string{"egress-deny"}, want: "newly_denied"},
	}
	for _, test := range diffTests {
		t.Run(test.name, func(t *testing.T) {
			before, err := ParseNetworkPolicies(manifest(test.before...))
			if err != nil {
				t.Fatalf("parse baseline policies: %v", err)
			}
			after, err := ParseNetworkPolicies(manifest(test.after...))
			if err != nil {
				t.Fatalf("parse proposed policies: %v", err)
			}
			change := CompareConnectivity(before, after, query("pci-phase6-source", "client-a", "client-a", "source", 80))
			if change.Change != test.want {
				t.Fatalf("CompareConnectivity() change = %q, want %q", change.Change, test.want)
			}
		})
	}
}
