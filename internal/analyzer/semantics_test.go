package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzePathMultiDocumentYAML(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policies.yaml")
	content := `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-web
  namespace: prod
spec:
  podSelector:
    matchLabels:
      app: web
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-api
  namespace: prod
spec:
  podSelector:
    matchLabels:
      app: api
`
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	result, err := AnalyzePath(dir)
	if err != nil {
		t.Fatalf("AnalyzePath: %v", err)
	}
	if result.Summary.TotalPolicies != 2 {
		t.Fatalf("expected 2 policies, got %d", result.Summary.TotalPolicies)
	}
}

func TestSelectorMatchExpressionsArePreserved(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "selector.yaml")
	content := `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: match-expr
  namespace: prod
spec:
  podSelector:
    matchExpressions:
      - key: tier
        operator: In
        values: ["frontend", "api"]
  policyTypes:
    - Ingress
  ingress:
    - from: []
`
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	result, err := AnalyzePath(dir)
	if err != nil {
		t.Fatalf("AnalyzePath: %v", err)
	}
	if len(result.Policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(result.Policies))
	}
	if result.Policies[0].Selector == "" {
		t.Fatal("expected selector to be non-empty for matchExpressions")
	}
}

func TestExplainFindingReturnsStructuredModel(t *testing.T) {
	result, err := AnalyzePath("../../fixtures/base")
	if err != nil {
		t.Fatalf("AnalyzePath: %v", err)
	}
	if len(result.Findings) == 0 {
		t.Skip("fixture has no static findings; explanation is still supported for future findings")
	}
	explanation, err := ExplainFinding(result, result.Findings[0].ID)
	if err != nil {
		t.Fatalf("ExplainFinding: %v", err)
	}
	if explanation.TrafficStatus == "" {
		t.Fatal("expected a non-empty traffic status")
	}
	if len(explanation.Assumptions) == 0 {
		t.Fatal("expected assumptions for the explanation")
	}
}

func TestExplainFindingDoesNotInferFlowOutcomeFromPolicyFinding(t *testing.T) {
	result := Analysis{Findings: []Finding{{
		ID: "finding-policy", Kind: "default-deny", Message: "Policy isolates ingress", Provenance: "modeled",
		RelevantPolicies: []string{"server/api"}, Evidence: []string{"manifest:policy-types"},
	}}}
	explanation, err := ExplainFinding(result, "finding-policy")
	if err != nil {
		t.Fatalf("ExplainFinding: %v", err)
	}
	if explanation.TrafficStatus != Unknown {
		t.Fatalf("manifest-level finding must not imply a flow result, got %q", explanation.TrafficStatus)
	}
	if explanation.Provenance != "modeled" || len(explanation.Unsupported) == 0 {
		t.Fatalf("explanation lacks provenance or limitation: %#v", explanation)
	}
}

func TestDiffReportIgnoresWhitespaceOrderingAndDefaultProtocolChanges(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yaml")
	proposed := filepath.Join(dir, "proposed.yaml")
	baseContent := `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: api-allow
  namespace: backend
spec:
  podSelector:
    matchLabels:
      app: api
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: web
      ports:
        - protocol: TCP
          port: 8080
`
	proposedContent := `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  namespace: backend
  name: api-allow
spec:
  ingress:
    - ports:
        - port: 8080
      from:
        - podSelector:
            matchLabels:
              app: web
  podSelector:
    matchLabels:
      app: api
`
	if err := os.WriteFile(base, []byte(baseContent), 0o600); err != nil {
		t.Fatalf("write base: %v", err)
	}
	if err := os.WriteFile(proposed, []byte(proposedContent), 0o600); err != nil {
		t.Fatalf("write proposed: %v", err)
	}
	report, err := DiffPaths(base, proposed)
	if err != nil {
		t.Fatalf("DiffPaths: %v", err)
	}
	if len(report.Modified) != 0 {
		t.Fatalf("expected semantically equivalent policies to produce no modified diff, got %#v", report.Modified)
	}
}

func TestDiffReportDetectsPolicyPermissionChanges(t *testing.T) {
	report, err := DiffPaths("../../fixtures/base", "../../fixtures/proposed")
	if err != nil {
		t.Fatalf("DiffPaths: %v", err)
	}
	if len(report.Modified) == 0 {
		t.Fatal("expected modified diff when egress permission is added")
	}
}

func TestDiffPathsDetectsRuleEditsWithoutRuleCountChanges(t *testing.T) {
	dir := t.TempDir()
	baseDir := filepath.Join(dir, "base")
	proposedDir := filepath.Join(dir, "proposed")
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		t.Fatalf("create baseline directory: %v", err)
	}
	if err := os.MkdirAll(proposedDir, 0o700); err != nil {
		t.Fatalf("create proposed directory: %v", err)
	}
	base := `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-client
  namespace: backend
spec:
  podSelector:
    matchLabels:
      app: api
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: web
      ports:
        - protocol: TCP
          port: 8080
`
	proposed := strings.Replace(base, "port: 8080", "port: 8443", 1)
	if err := os.WriteFile(filepath.Join(baseDir, "policy.yaml"), []byte(base), 0o600); err != nil {
		t.Fatalf("write baseline policy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(proposedDir, "policy.yaml"), []byte(proposed), 0o600); err != nil {
		t.Fatalf("write proposed policy: %v", err)
	}

	report, err := DiffPaths(baseDir, proposedDir)
	if err != nil {
		t.Fatalf("DiffPaths: %v", err)
	}
	if len(report.Modified) != 1 {
		t.Fatalf("expected same-name policy rule edit in diff, got %#v", report.Modified)
	}
	if report.Modified[0].Key != "backend/allow-client" ||
		!containsString(report.Modified[0].ChangedFields, "ingressRules") {
		t.Fatalf("diff should point to the changed ingress rules: %#v", report.Modified[0])
	}
}
