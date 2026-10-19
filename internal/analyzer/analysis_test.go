package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzePath(t *testing.T) {
	result, err := AnalyzePath("../../fixtures/base")
	if err != nil {
		t.Fatalf("AnalyzePath returned error: %v", err)
	}
	if result.Summary.TotalPolicies != 1 {
		t.Fatalf("expected 1 policy, got %d", result.Summary.TotalPolicies)
	}
	if result.Policies[0].Selector != "app=api" {
		t.Fatalf("expected selector app=api, got %q", result.Policies[0].Selector)
	}
}

func TestDiffPaths(t *testing.T) {
	report, err := DiffPaths("../../fixtures/base", "../../fixtures/proposed")
	if err != nil {
		t.Fatalf("DiffPaths returned error: %v", err)
	}
	if len(report.Modified) == 0 {
		t.Fatal("expected semantic diff for changed policy")
	}
}

func TestAnalyzePathRejectsMalformedNetworkPolicyInsteadOfIgnoringIt(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.yaml")
	content := `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: broken
  namespace: backend
spec:
  ingress: [
`
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatalf("write malformed policy: %v", err)
	}
	if _, err := AnalyzePath(dir); err == nil {
		t.Fatal("expected malformed NetworkPolicy to fail analysis rather than disappear from the inventory")
	}
}

func TestAnalyzePathInfersPolicyTypesForEgressPolicies(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.yaml")
	content := `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: egress-only-rules
  namespace: backend
spec:
  podSelector:
    matchLabels:
      app: api
  egress:
    - {}
`
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	analysis, err := AnalyzePath(dir)
	if err != nil {
		t.Fatalf("AnalyzePath: %v", err)
	}
	if got := analysis.Policies[0].PolicyType; got != "Ingress,Egress" {
		t.Fatalf("inferred policy types = %q, want Ingress,Egress", got)
	}
	if !analysis.Policies[0].DefaultDeny {
		t.Fatal("policy with egress rules but no ingress rules isolates ingress by default")
	}
}
