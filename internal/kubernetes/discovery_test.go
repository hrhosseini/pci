package kubernetes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestDiscoverReadOnlyAggregatesReadOnlyResources(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-pod", Namespace: "default", Labels: map[string]string{"app": "api"}}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "api"}}}}},
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "api-policy", Namespace: "default"}, Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}, Ingress: []networkingv1.NetworkPolicyIngressRule{{}}}},
	)

	result, err := DiscoverReadOnly(context.Background(), client, DiscoveryOptions{Namespace: "default", Timeout: time.Second})
	if err != nil {
		t.Fatalf("discover read-only: %v", err)
	}
	if result.Status != "read-only" || !result.Complete {
		t.Fatalf("unexpected discovery status/completeness: %#v", result)
	}
	if len(result.Namespaces) == 0 || result.Namespaces[0] != "default" {
		t.Fatalf("unexpected namespaces: %#v", result.Namespaces)
	}
	if len(result.NetworkPolicies) != 1 {
		t.Fatalf("expected one NetworkPolicy, got %#v", result.NetworkPolicies)
	}
	if len(result.Workloads) == 0 {
		t.Fatal("expected workloads to be discovered")
	}
	for _, action := range client.Actions() {
		verb := action.GetVerb()
		if verb == "create" || verb == "update" || verb == "patch" || verb == "delete" || verb == "deletecollection" {
			t.Fatalf("discovery performed a write action: %s", verb)
		}
		if verb == "list" && action.GetResource().Resource == "namespaces" {
			t.Fatal("namespace-scoped discovery must not list namespace metadata cluster-wide")
		}
	}
}

func TestDiscoverReadOnlyAllNamespaces(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "prod"}},
	)
	result, err := DiscoverReadOnly(context.Background(), client, DiscoveryOptions{AllNamespaces: true, Timeout: time.Second})
	if err != nil {
		t.Fatalf("discover all namespaces: %v", err)
	}
	if len(result.Namespaces) != 2 {
		t.Fatalf("expected 2 namespaces, got %#v", result.Namespaces)
	}
	if !result.Complete {
		t.Fatalf("complete namespace collection should not be marked incomplete: %#v", result)
	}
}

func TestDiscoverReadOnlyRequiresExplicitScope(t *testing.T) {
	client := fake.NewSimpleClientset()
	result, err := DiscoverReadOnly(context.Background(), client, DiscoveryOptions{Timeout: time.Second})
	if err == nil || result.Status != "invalid-scope" || result.Complete {
		t.Fatalf("discovery without namespace or all-namespace mode must fail closed: result=%#v err=%v", result, err)
	}
}

func TestDiscoverReadOnlyFollowsContinuationTokens(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}})
	client.PrependReactor("list", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		options := action.(ktesting.ListActionImpl).ListOptions
		if options.Continue == "" {
			return true, &corev1.PodList{
				ListMeta: metav1.ListMeta{Continue: "next"},
				Items:    []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "first", Namespace: "default"}}},
			}, nil
		}
		if options.Continue != "next" {
			t.Fatalf("unexpected continuation token %q", options.Continue)
		}
		return true, &corev1.PodList{Items: []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "second", Namespace: "default"}}}}, nil
	})

	result, err := DiscoverReadOnly(context.Background(), client, DiscoveryOptions{Namespace: "default", Timeout: time.Second})
	if err != nil {
		t.Fatalf("discover paginated resources: %v", err)
	}
	if !result.Complete || len(result.Workloads) != 2 {
		t.Fatalf("expected all pages and complete inventory, got complete=%t workloads=%#v errors=%#v",
			result.Complete, result.Workloads, result.Errors)
	}
}

func TestDiscoverReadOnlyUsesPodTemplateLabelsAndHostNetwork(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"team": "backend"}}},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", Labels: map[string]string{"controller": "deployment"}},
			Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "api"}},
				Spec:       corev1.PodSpec{HostNetwork: true},
			}},
		},
	)
	result, err := DiscoverReadOnly(context.Background(), client, DiscoveryOptions{Namespace: "default", Timeout: time.Second})
	if err != nil {
		t.Fatalf("discover resources: %v", err)
	}
	if !result.Complete {
		t.Fatalf("successful collection should be marked complete: %#v", result)
	}
	if got := result.NamespaceLabels["default"]["team"]; got != "backend" {
		t.Fatalf("namespace labels not retained: %#v", result.NamespaceLabels)
	}
	for _, workload := range result.Workloads {
		if workload.Kind != "Deployment" {
			continue
		}
		if workload.Labels["app"] != "api" || workload.Labels["controller"] != "" || !workload.HostNetwork {
			t.Fatalf("deployment data should reflect Pod template metadata: %#v", workload)
		}
		return
	}
	t.Fatal("expected the Deployment to be discovered")
}

func TestDiscoverReadOnlyReportsDeniedControllerAccess(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}})
	client.PrependReactor("list", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("permission denied")
	})

	result, err := DiscoverReadOnly(context.Background(), client, DiscoveryOptions{Namespace: "default", Timeout: time.Second})
	if err != nil {
		t.Fatalf("partial discovery should return its result with explicit status: %v", err)
	}
	if result.Complete || result.Status != "read-only-limited" || !hasString(result.Incomplete, "deployments") {
		t.Fatalf("expected explicit incomplete discovery state, got %#v", result)
	}
}

func TestDiscoverReadOnlyDistinguishesDeniedNamespaceListing(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "namespaces", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, "", errors.New("denied"))
	})
	result, err := DiscoverReadOnly(context.Background(), client, DiscoveryOptions{AllNamespaces: true, Timeout: time.Second})
	if err == nil || result.Status != "permission-denied" || result.Complete {
		t.Fatalf("expected explicit permission failure and incomplete result: result=%#v err=%v", result, err)
	}
}

func TestReadOnlyCapabilityReportNilConfigReturnsOffline(t *testing.T) {
	report, err := ReadOnlyCapabilityReport(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !report.Offline || report.Status != "not-configured" {
		t.Fatalf("unexpected offline report: %#v", report)
	}
}

func TestLoadConfigUsesExplicitKubeconfigPath(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config")
	contents := `apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: localctx
  context:
    cluster: local
    user: localuser
current-context: localctx
users:
- name: localuser
  user:
    token: demo-token
`
	if err := os.WriteFile(file, []byte(contents), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	cfg, err := LoadConfig(file, "")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !strings.HasPrefix(cfg.Host, "https://") {
		t.Fatalf("unexpected host: %q", cfg.Host)
	}
}

func hasString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
