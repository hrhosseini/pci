package analyzer

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

// PolicySummary is the canonical summary of a NetworkPolicy.
type PolicySummary struct {
	Namespace    string   `json:"namespace"`
	Name         string   `json:"name"`
	PolicyType   string   `json:"policyType"`
	Selector     string   `json:"selector"`
	IngressRules int      `json:"ingressRules"`
	EgressRules  int      `json:"egressRules"`
	DefaultDeny  bool     `json:"defaultDeny"`
	MatchLabels  []string `json:"matchLabels,omitempty"`
	RelatedPeers []string `json:"relatedPeers,omitempty"`
	Unsupported  []string `json:"unsupported,omitempty"`
}

// Finding is a structured issue or derived result.
type Finding struct {
	ID                  string   `json:"id"`
	Kind                string   `json:"kind"`
	Message             string   `json:"message"`
	Severity            string   `json:"severity"`
	Confidence          string   `json:"confidence"`
	Provenance          string   `json:"provenance"`
	SourceEndpoint      string   `json:"sourceEndpoint,omitempty"`
	DestinationEndpoint string   `json:"destinationEndpoint,omitempty"`
	Source              string   `json:"source,omitempty"`
	Target              string   `json:"target,omitempty"`
	RelevantPolicies    []string `json:"relevantPolicies,omitempty"`
	Evidence            []string `json:"evidence,omitempty"`
	Assumptions         []string `json:"assumptions,omitempty"`
}

// Analysis is the canonical output of a static or semantic analysis.
type Analysis struct {
	ID          string          `json:"id,omitempty"`
	Path        string          `json:"path"`
	GeneratedAt time.Time       `json:"generatedAt"`
	Summary     AnalysisSummary `json:"summary"`
	Policies    []PolicySummary `json:"policies"`
	Findings    []Finding       `json:"findings"`
}

type AnalysisSummary struct {
	TotalPolicies int `json:"totalPolicies"`
	DefaultDeny   int `json:"defaultDeny"`
	FindingCount  int `json:"findingCount"`
}

type DiffReport struct {
	Added    []string             `json:"added"`
	Removed  []string             `json:"removed"`
	Modified []PolicyModification `json:"modified"`
	Findings []Finding            `json:"findings"`
}

type PolicyModification struct {
	Key           string              `json:"key"`
	Old           string              `json:"old"`
	New           string              `json:"new"`
	Reason        string              `json:"reason"`
	ChangedFields []string            `json:"changedFields"`
	FieldChanges  []PolicyFieldChange `json:"fieldChanges"`
}

type PolicyFieldChange struct {
	Field  string          `json:"field"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

func AnalyzePath(path string) (Analysis, error) {
	abs, policies, err := loadPoliciesFromPath(path)
	if err != nil {
		return Analysis{}, err
	}

	policySummaries := make([]PolicySummary, 0, len(policies))
	findings := make([]Finding, 0)
	for _, policy := range policies {
		summary := summarizePolicy(policy)
		policySummaries = append(policySummaries, summary)
		if summary.DefaultDeny {
			findings = append(findings, Finding{
				ID:               fmt.Sprintf("finding-%s-default-deny", summary.Namespace+"-"+summary.Name),
				Kind:             "default-deny",
				Message:          fmt.Sprintf("Policy %s/%s is default-deny for at least one direction.", summary.Namespace, summary.Name),
				Severity:         "medium",
				Confidence:       "high",
				Source:           summary.Namespace + "/" + summary.Name,
				RelevantPolicies: []string{summary.Namespace + "/" + summary.Name},
				Evidence:         []string{"manifest:policy-types", "manifest:rules"},
				Assumptions:      []string{"Exact workload matches are inferred from the selector and manifest metadata only."},
			})
		}
		if summary.IngressRules == 0 && containsPolicyType(effectivePolicyTypes(policy), networkingv1.PolicyTypeIngress) {
			findings = append(findings, Finding{ID: fmt.Sprintf("finding-%s-allow-none", summary.Namespace+"-"+summary.Name), Kind: "allow-none", Message: fmt.Sprintf("Ingress policy %s/%s permits no explicit ingress rules; review whether this is intentional.", summary.Namespace, summary.Name), Severity: "low", Confidence: "medium", Source: summary.Namespace + "/" + summary.Name, RelevantPolicies: []string{summary.Namespace + "/" + summary.Name}, Evidence: []string{"manifest:ingress"}, Assumptions: []string{"The analyzer does not infer workload labels from a live cluster."}})
		}
		if summary.EgressRules == 0 && containsPolicyType(effectivePolicyTypes(policy), networkingv1.PolicyTypeEgress) {
			findings = append(findings, Finding{ID: fmt.Sprintf("finding-%s-egress-deny", summary.Namespace+"-"+summary.Name), Kind: "default-egress-deny", Message: fmt.Sprintf("Egress policy %s/%s denies all egress unless additional rules are added.", summary.Namespace, summary.Name), Severity: "medium", Confidence: "medium", Source: summary.Namespace + "/" + summary.Name, RelevantPolicies: []string{summary.Namespace + "/" + summary.Name}, Evidence: []string{"manifest:egress"}, Assumptions: []string{"DNS exceptions must be separately validated against runtime behavior."}})
		}
		if summary.Selector == "" {
			findings = append(findings, Finding{ID: fmt.Sprintf("finding-%s-broad-selector", summary.Namespace+"-"+summary.Name), Kind: "broad-selector", Message: fmt.Sprintf("Policy %s/%s selects all pods in the namespace because the selector is empty.", summary.Namespace, summary.Name), Severity: "high", Confidence: "high", Source: summary.Namespace + "/" + summary.Name, RelevantPolicies: []string{summary.Namespace + "/" + summary.Name}, Evidence: []string{"manifest:pod-selector"}, Assumptions: []string{"The analyzer treats an empty pod selector as a broad selector in the absence of live workload metadata."}})
		}
	}

	sort.Slice(policySummaries, func(i, j int) bool {
		return policySummaries[i].Namespace+"/"+policySummaries[i].Name < policySummaries[j].Namespace+"/"+policySummaries[j].Name
	})
	for i := range findings {
		if findings[i].Provenance == "" {
			findings[i].Provenance = "modeled"
		}
		if len(findings[i].Assumptions) == 0 {
			findings[i].Assumptions = []string{"This finding describes manifest-level policy intent; no source/destination workload flow was evaluated."}
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].ID < findings[j].ID })

	result := Analysis{
		Path:        abs,
		GeneratedAt: time.Now().UTC(),
		Summary: AnalysisSummary{
			TotalPolicies: len(policySummaries),
			DefaultDeny:   countDefaultDeny(policySummaries),
			FindingCount:  len(findings),
		},
		Policies: policySummaries,
		Findings: findings,
	}
	return result, nil
}

// ParseNetworkPolicies parses NetworkPolicy objects from a YAML/JSON stream.
// Non-NetworkPolicy documents are ignored; malformed documents return errors
// instead of being treated as an empty policy inventory.
func ParseNetworkPolicies(manifest string) ([]networkingv1.NetworkPolicy, error) {
	policies := make([]networkingv1.NetworkPolicy, 0)
	seen := make(map[string]int)
	for index, document := range splitYAMLDocuments([]byte(manifest)) {
		if len(bytes.TrimSpace(document)) == 0 {
			continue
		}
		var header struct {
			Kind string `json:"kind"`
		}
		if err := yaml.Unmarshal(document, &header); err != nil {
			return nil, fmt.Errorf("parse document %d: %w", index+1, err)
		}
		if header.Kind != "NetworkPolicy" {
			continue
		}
		var policy networkingv1.NetworkPolicy
		if err := yaml.Unmarshal(document, &policy); err != nil {
			return nil, fmt.Errorf("parse NetworkPolicy document %d: %w", index+1, err)
		}
		if policy.Name == "" || policy.Namespace == "" {
			return nil, fmt.Errorf("NetworkPolicy document %d must specify metadata.name and metadata.namespace for flow evaluation", index+1)
		}
		key := policy.Namespace + "/" + policy.Name
		if previous, exists := seen[key]; exists {
			return nil, fmt.Errorf("NetworkPolicy %s is duplicated in documents %d and %d", key, previous, index+1)
		}
		seen[key] = index + 1
		policies = append(policies, policy)
	}
	return policies, nil
}

func DiffPaths(basePath, proposedPath string) (DiffReport, error) {
	_, base, err := loadPoliciesFromPath(basePath)
	if err != nil {
		return DiffReport{}, err
	}
	_, proposed, err := loadPoliciesFromPath(proposedPath)
	if err != nil {
		return DiffReport{}, err
	}
	return diff(base, proposed), nil
}

func summarizePolicy(policy networkingv1.NetworkPolicy) PolicySummary {
	selector := selectorToString(policy.Spec.PodSelector)
	labels := selectorParts(policy.Spec.PodSelector.MatchLabels)
	relatedPeers := peerSummary(policy.Spec.Ingress, policy.Spec.Egress)
	summary := PolicySummary{
		Namespace:    policy.Namespace,
		Name:         policy.Name,
		PolicyType:   resolvePolicyType(effectivePolicyTypes(policy)),
		Selector:     selector,
		IngressRules: len(policy.Spec.Ingress),
		EgressRules:  len(policy.Spec.Egress),
		DefaultDeny:  defaultDenyForPolicy(policy),
		MatchLabels:  labels,
		RelatedPeers: relatedPeers,
	}
	if summary.Selector == "" {
		summary.Unsupported = append(summary.Unsupported, "empty pod selector treated as broad match")
	}
	return summary
}

func defaultDenyForPolicy(policy networkingv1.NetworkPolicy) bool {
	for _, pt := range effectivePolicyTypes(policy) {
		if pt == networkingv1.PolicyTypeIngress && len(policy.Spec.Ingress) == 0 {
			return true
		}
		if pt == networkingv1.PolicyTypeEgress && len(policy.Spec.Egress) == 0 {
			return true
		}
	}
	return false
}

func countDefaultDeny(policies []PolicySummary) int {
	count := 0
	for _, policy := range policies {
		if policy.DefaultDeny {
			count++
		}
	}
	return count
}

func diff(base, proposed []networkingv1.NetworkPolicy) DiffReport {
	baseMap := make(map[string]networkingv1.NetworkPolicy, len(base))
	for _, p := range base {
		baseMap[policyKey(p)] = p
	}
	proposedMap := make(map[string]networkingv1.NetworkPolicy, len(proposed))
	for _, p := range proposed {
		proposedMap[policyKey(p)] = p
	}

	report := DiffReport{Added: []string{}, Removed: []string{}, Modified: []PolicyModification{}, Findings: []Finding{}}
	for key := range baseMap {
		if _, exists := proposedMap[key]; !exists {
			report.Removed = append(report.Removed, key)
		}
	}
	for key := range proposedMap {
		if _, exists := baseMap[key]; !exists {
			report.Added = append(report.Added, key)
		}
	}
	for _, key := range sortedKeys(baseMap, proposedMap) {
		oldVal, okOld := baseMap[key]
		newVal, okNew := proposedMap[key]
		if !okOld || !okNew {
			continue
		}
		changedFields := changedPolicyFields(oldVal, newVal)
		if len(changedFields) > 0 {
			report.Modified = append(report.Modified, PolicyModification{
				Key:           key,
				Old:           policySignature(oldVal),
				New:           policySignature(newVal),
				Reason:        "NetworkPolicy semantics changed: " + strings.Join(changedFields, ", "),
				ChangedFields: changedFields,
				FieldChanges:  changedPolicyFieldDetails(oldVal, newVal),
			})
		}
	}
	sort.Strings(report.Added)
	sort.Strings(report.Removed)
	sort.Slice(report.Modified, func(i, j int) bool { return report.Modified[i].Key < report.Modified[j].Key })
	return report
}

func sortedKeys[T any](baseMap, proposedMap map[string]T) []string {
	keys := map[string]bool{}
	for key := range baseMap {
		keys[key] = true
	}
	for key := range proposedMap {
		keys[key] = true
	}
	all := make([]string, 0, len(keys))
	for key := range keys {
		all = append(all, key)
	}
	sort.Strings(all)
	return all
}

func policyKey(policy networkingv1.NetworkPolicy) string {
	return policy.Namespace + "/" + policy.Name
}

func changedPolicyFields(before, after networkingv1.NetworkPolicy) []string {
	changes := changedPolicyFieldDetails(before, after)
	fields := make([]string, 0, len(changes))
	for _, change := range changes {
		fields = append(fields, change.Field)
	}
	return fields
}

func changedPolicyFieldDetails(before, after networkingv1.NetworkPolicy) []PolicyFieldChange {
	beforeSpec := normalizedPolicySpec(before)
	afterSpec := normalizedPolicySpec(after)
	changed := make([]PolicyFieldChange, 0, 4)
	add := func(field string, oldValue, newValue any) {
		if !equalJSON(oldValue, newValue) {
			changed = append(changed, PolicyFieldChange{
				Field:  field,
				Before: marshalJSONValue(oldValue),
				After:  marshalJSONValue(newValue),
			})
		}
	}
	add("podSelector", beforeSpec.PodSelector, afterSpec.PodSelector)
	add("policyTypes", beforeSpec.PolicyTypes, afterSpec.PolicyTypes)
	add("ingressRules", beforeSpec.Ingress, afterSpec.Ingress)
	add("egressRules", beforeSpec.Egress, afterSpec.Egress)
	return changed
}

func policySignature(policy networkingv1.NetworkPolicy) string {
	summary := summarizePolicy(policy)
	spec, _ := json.Marshal(normalizedPolicySpec(policy))
	fingerprint := sha256.Sum256(spec)
	return fmt.Sprintf("type=%s selector=%s ingress=%d egress=%d defaultdeny=%t spec-sha256=%s",
		summary.PolicyType, summary.Selector, summary.IngressRules, summary.EgressRules,
		summary.DefaultDeny, hex.EncodeToString(fingerprint[:]))
}

func normalizedPolicySpec(policy networkingv1.NetworkPolicy) networkingv1.NetworkPolicySpec {
	spec := *policy.Spec.DeepCopy()
	spec.PolicyTypes = effectivePolicyTypes(policy)
	sortPolicyTypes(spec.PolicyTypes)
	spec.PodSelector = normalizeSelector(spec.PodSelector)

	for i := range spec.Ingress {
		rule := &spec.Ingress[i]
		for peerIndex := range rule.From {
			normalizePeer(&rule.From[peerIndex])
		}
		for portIndex := range rule.Ports {
			normalizePolicyPort(&rule.Ports[portIndex])
		}
		sortJSONSlice(rule.From)
		sortJSONSlice(rule.Ports)
		if len(rule.From) == 0 {
			rule.From = nil
		}
		if len(rule.Ports) == 0 {
			rule.Ports = nil
		}
	}
	sortJSONSlice(spec.Ingress)
	if len(spec.Ingress) == 0 {
		spec.Ingress = nil
	}

	for i := range spec.Egress {
		rule := &spec.Egress[i]
		for peerIndex := range rule.To {
			normalizePeer(&rule.To[peerIndex])
		}
		for portIndex := range rule.Ports {
			normalizePolicyPort(&rule.Ports[portIndex])
		}
		sortJSONSlice(rule.To)
		sortJSONSlice(rule.Ports)
		if len(rule.To) == 0 {
			rule.To = nil
		}
		if len(rule.Ports) == 0 {
			rule.Ports = nil
		}
	}
	sortJSONSlice(spec.Egress)
	if len(spec.Egress) == 0 {
		spec.Egress = nil
	}
	return spec
}

func normalizeSelector(selector metav1.LabelSelector) metav1.LabelSelector {
	if len(selector.MatchLabels) == 0 {
		selector.MatchLabels = nil
	}
	for i := range selector.MatchExpressions {
		sort.Strings(selector.MatchExpressions[i].Values)
	}
	sort.Slice(selector.MatchExpressions, func(i, j int) bool {
		left, right := selector.MatchExpressions[i], selector.MatchExpressions[j]
		if left.Key != right.Key {
			return left.Key < right.Key
		}
		if left.Operator != right.Operator {
			return left.Operator < right.Operator
		}
		return strings.Join(left.Values, "\x00") < strings.Join(right.Values, "\x00")
	})
	return selector
}

func normalizePeer(peer *networkingv1.NetworkPolicyPeer) {
	if peer.PodSelector != nil {
		*peer.PodSelector = normalizeSelector(*peer.PodSelector)
	}
	if peer.NamespaceSelector != nil {
		*peer.NamespaceSelector = normalizeSelector(*peer.NamespaceSelector)
	}
	if peer.IPBlock != nil {
		sort.Strings(peer.IPBlock.Except)
		if len(peer.IPBlock.Except) == 0 {
			peer.IPBlock.Except = nil
		}
	}
}

func normalizePolicyPort(port *networkingv1.NetworkPolicyPort) {
	if port.Protocol == nil {
		tcp := corev1.ProtocolTCP
		port.Protocol = &tcp
	}
	if port.Port != nil && port.Port.Type == intstr.Int &&
		port.EndPort != nil && *port.EndPort == port.Port.IntVal {
		port.EndPort = nil
	}
}

func sortJSONSlice[T any](items []T) {
	sort.Slice(items, func(i, j int) bool {
		left, _ := json.Marshal(items[i])
		right, _ := json.Marshal(items[j])
		return bytes.Compare(left, right) < 0
	})
}

func equalJSON(left, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return bytes.Equal(leftJSON, rightJSON)
}

func marshalJSONValue(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

func effectivePolicyTypes(policy networkingv1.NetworkPolicy) []networkingv1.PolicyType {
	if len(policy.Spec.PolicyTypes) == 0 {
		types := []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
		if len(policy.Spec.Egress) > 0 {
			types = append(types, networkingv1.PolicyTypeEgress)
		}
		return types
	}
	types := append([]networkingv1.PolicyType(nil), policy.Spec.PolicyTypes...)
	sortPolicyTypes(types)
	return types
}

func sortPolicyTypes(types []networkingv1.PolicyType) {
	sort.Slice(types, func(i, j int) bool {
		if types[i] == types[j] {
			return false
		}
		if types[i] == networkingv1.PolicyTypeIngress {
			return true
		}
		if types[j] == networkingv1.PolicyTypeIngress {
			return false
		}
		if types[i] == networkingv1.PolicyTypeEgress {
			return true
		}
		if types[j] == networkingv1.PolicyTypeEgress {
			return false
		}
		return types[i] < types[j]
	})
}

func containsPolicyType(types []networkingv1.PolicyType, target networkingv1.PolicyType) bool {
	for _, policyType := range types {
		if policyType == target {
			return true
		}
	}
	return false
}

func resolvePolicyType(policyTypes []networkingv1.PolicyType) string {
	return strings.Join(policyTypesToStrings(policyTypes), ",")
}

func policyTypesToStrings(policyTypes []networkingv1.PolicyType) []string {
	result := make([]string, 0, len(policyTypes))
	for _, pt := range policyTypes {
		result = append(result, string(pt))
	}
	return result
}

func selectorToString(selector metav1.LabelSelector) string {
	if len(selector.MatchLabels) == 0 && len(selector.MatchExpressions) == 0 {
		return "" // intentionally broad / namespace-scoped selector
	}
	parts := make([]string, 0, len(selector.MatchLabels)+len(selector.MatchExpressions))
	for k, v := range selector.MatchLabels {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	for _, expr := range selector.MatchExpressions {
		parts = append(parts, fmt.Sprintf("%s %s %s", expr.Key, expr.Operator, strings.Join(expr.Values, ",")))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func selectorParts(labels map[string]string) []string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k+"="+labels[k])
	}
	sort.Strings(keys)
	return keys
}

func peerSummary(ingressRules []networkingv1.NetworkPolicyIngressRule, egressRules []networkingv1.NetworkPolicyEgressRule) []string {
	peers := make([]string, 0)
	for _, rule := range ingressRules {
		for _, peer := range rule.From {
			if peer.PodSelector != nil || peer.NamespaceSelector != nil || peer.IPBlock != nil {
				peers = append(peers, "ingress")
			}
		}
	}
	for _, rule := range egressRules {
		for _, peer := range rule.To {
			if peer.PodSelector != nil || peer.NamespaceSelector != nil || peer.IPBlock != nil {
				peers = append(peers, "egress")
			}
		}
	}
	return peers
}

func collectPolicyFiles(root string) ([]string, error) {
	files := make([]string, 0)
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		lower := strings.ToLower(d.Name())
		if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".json") {
			files = append(files, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(files)
	return files, nil
}

func loadPoliciesFromPath(path string) (string, []networkingv1.NetworkPolicy, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", nil, err
	}
	entries, err := collectPolicyFiles(abs)
	if err != nil {
		return "", nil, err
	}
	policies, err := loadNetworkPolicies(entries)
	if err != nil {
		return "", nil, err
	}
	return abs, policies, nil
}

func loadNetworkPolicies(files []string) ([]networkingv1.NetworkPolicy, error) {
	policies := make([]networkingv1.NetworkPolicy, 0)
	seen := make(map[string]string)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		filePolicies, err := ParseNetworkPolicies(string(data))
		if err != nil {
			return nil, fmt.Errorf("parse policy file %q: %w", file, err)
		}
		for _, policy := range filePolicies {
			key := policyKey(policy)
			if previous, exists := seen[key]; exists {
				return nil, fmt.Errorf("NetworkPolicy %s is duplicated in files %q and %q", key, previous, file)
			}
			seen[key] = file
			policies = append(policies, policy)
		}
	}
	return policies, nil
}

func splitYAMLDocuments(data []byte) [][]byte {
	reader := yamlutil.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	docs := make([][]byte, 0)
	for {
		buf, err := reader.Read()
		if err != nil {
			break
		}
		docs = append(docs, buf)
	}
	return docs
}

func ToJSON(v any) (string, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}
