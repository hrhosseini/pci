package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/pager"

	"pci/internal/config"
)

// CapabilityReport describes the read-only discovery capabilities available to the app.
type CapabilityReport struct {
	Mode       string   `json:"mode"`
	Status     string   `json:"status"`
	Offline    bool     `json:"offline"`
	Cluster    string   `json:"cluster,omitempty"`
	Context    string   `json:"context,omitempty"`
	Namespaces []string `json:"namespaces,omitempty"`
	Pods       []string `json:"pods,omitempty"`
	Policies   []string `json:"policies,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
	Errors     []string `json:"errors,omitempty"`
}

// DiscoveryOptions defines the scope intended for a read-only discovery query.
type DiscoveryOptions struct {
	Namespace     string
	AllNamespaces bool
	Context       string
	Timeout       time.Duration
}

// Workload describes a Kubernetes workload or pod that the local analyzer can model.
type Workload struct {
	Namespace   string            `json:"namespace"`
	Name        string            `json:"name"`
	Kind        string            `json:"kind"`
	Labels      map[string]string `json:"labels,omitempty"`
	Selector    string            `json:"selector,omitempty"`
	HostNetwork bool              `json:"hostNetwork"`
}

// NetworkPolicyRecord describes the NetworkPolicy subset relevant to PCI.
type NetworkPolicyRecord struct {
	Namespace    string   `json:"namespace"`
	Name         string   `json:"name"`
	PodSelector  string   `json:"podSelector,omitempty"`
	PolicyTypes  []string `json:"policyTypes,omitempty"`
	IngressRules int      `json:"ingressRules"`
	EgressRules  int      `json:"egressRules"`
}

// DiscoveryResult captures a read-only cluster discovery from a real config.
type DiscoveryResult struct {
	Mode            string                       `json:"mode"`
	Status          string                       `json:"status"`
	Offline         bool                         `json:"offline"`
	Complete        bool                         `json:"complete"`
	Cluster         string                       `json:"cluster,omitempty"`
	Context         string                       `json:"context,omitempty"`
	Namespace       string                       `json:"namespace,omitempty"`
	Namespaces      []string                     `json:"namespaces,omitempty"`
	NamespaceLabels map[string]map[string]string `json:"namespaceLabels,omitempty"`
	Workloads       []Workload                   `json:"workloads,omitempty"`
	NetworkPolicies []NetworkPolicyRecord        `json:"networkPolicies,omitempty"`
	CollectedAt     time.Time                    `json:"collectedAt"`
	Warnings        []string                     `json:"warnings,omitempty"`
	Errors          []string                     `json:"errors,omitempty"`
	Incomplete      []string                     `json:"incomplete,omitempty"`
}

// ConfigMeta describes which kubeconfig path and context were resolved for a client.
type ConfigMeta struct {
	Path    string `json:"path,omitempty"`
	Context string `json:"context,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Source  string `json:"source,omitempty"`
}

// LoadConfig builds a client-go rest.Config using auto mode precedence
// (see config.LoadConfigWithMeta).
func LoadConfig(kubeconfigPath, contextName string) (*rest.Config, error) {
	cfg, _, err := LoadConfigWithMeta(kubeconfigPath, contextName)
	return cfg, err
}

// LoadConfigWithMeta is LoadConfig plus resolved path/context/mode metadata.
func LoadConfigWithMeta(kubeconfigPath, contextName string) (*rest.Config, ConfigMeta, error) {
	return LoadConfigWithMode(config.KubeModeAuto, kubeconfigPath, contextName)
}

// LoadConfigWithMode loads cluster config with an explicit connection mode.
func LoadConfigWithMode(mode, kubeconfigPath, contextName string) (*rest.Config, ConfigMeta, error) {
	cfg, meta, err := config.LoadConfigWithMeta(mode, kubeconfigPath, contextName)
	return cfg, ConfigMeta{
		Path:    meta.Path,
		Context: meta.Context,
		Mode:    meta.Mode,
		Source:  meta.Source,
	}, err
}

func DiscoverReadOnly(ctx context.Context, client kubernetes.Interface, opts DiscoveryOptions) (DiscoveryResult, error) {
	result := DiscoveryResult{
		Mode:            "cluster",
		Status:          "read-only",
		Offline:         false,
		Complete:        true,
		Context:         strings.TrimSpace(opts.Context),
		Namespace:       strings.TrimSpace(opts.Namespace),
		NamespaceLabels: map[string]map[string]string{},
		CollectedAt:     time.Now().UTC(),
	}
	if client == nil {
		result.Mode = "offline"
		result.Status = "not-configured"
		result.Offline = true
		result.Complete = false
		result.Incomplete = []string{"cluster"}
		result.Warnings = append(result.Warnings, "No cluster client was configured; offline policy analysis remains available.")
		return result, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	if version, err := client.Discovery().ServerVersion(); err != nil {
		result.Mode = "cluster"
		result.Status = "unreachable"
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			result.Status = "permission-denied"
		}
		result.Offline = true
		result.Complete = false
		result.Incomplete = []string{"cluster"}
		result.Errors = append(result.Errors, err.Error())
		if result.Status == "permission-denied" {
			result.Warnings = append(result.Warnings, "The configured identity was denied access to the Kubernetes discovery API.")
		} else {
			result.Warnings = append(result.Warnings, "The cluster API could not be reached.")
		}
		return result, err
	} else if version != nil {
		result.Cluster = strings.TrimSpace(version.GitVersion)
	}

	namespaceFilter := strings.TrimSpace(opts.Namespace)
	var err error
	if opts.AllNamespaces {
		result.Namespace = metav1.NamespaceAll
		namespaceFilter = metav1.NamespaceAll
	}
	if namespaceFilter == "" || namespaceFilter == metav1.NamespaceAll {
		if !opts.AllNamespaces {
			err := errors.New("discovery requires a namespace or explicit all-namespaces mode")
			result.Status = "invalid-scope"
			result.Offline = true
			result.Complete = false
			markIncomplete(&result, "scope", err)
			return result, err
		}
		err := eachPage(ctx, func(options metav1.ListOptions) (runtime.Object, error) {
			return client.CoreV1().Namespaces().List(ctx, options)
		}, func(object runtime.Object) error {
			namespace, ok := object.(*corev1.Namespace)
			if !ok {
				return fmt.Errorf("unexpected namespace object %T", object)
			}
			result.Namespaces = append(result.Namespaces, namespace.Name)
			result.NamespaceLabels[namespace.Name] = copyLabels(namespace.Labels)
			return nil
		})
		if err != nil {
			result.Status = "read-only-limited"
			if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
				result.Status = "permission-denied"
			}
			result.Offline = true
			markIncomplete(&result, "namespaces", err)
			result.Warnings = append(result.Warnings, "The cluster is reachable, but listing namespaces is not permitted.")
			return result, err
		}
		sort.Strings(result.Namespaces)
	} else {
		result.Namespace = namespaceFilter
		namespace, err := client.CoreV1().Namespaces().Get(ctx, namespaceFilter, metav1.GetOptions{})
		if err != nil {
			// Namespace objects are cluster-scoped. Namespace-scoped Roles often cannot get them.
			// Continue discovery for the requested name when list/get of namespaced resources may still work.
			if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
				result.Namespaces = []string{namespaceFilter}
				result.Warnings = append(result.Warnings, fmt.Sprintf("Cannot read Namespace object %q (RBAC); continuing namespaced discovery without namespace labels.", namespaceFilter))
				result.Errors = append(result.Errors, err.Error())
			} else if apierrors.IsNotFound(err) {
				result.Status = "namespace-unavailable"
				result.Offline = true
				markIncomplete(&result, "namespace", err)
				return result, err
			} else {
				result.Status = "namespace-unavailable"
				result.Offline = true
				markIncomplete(&result, "namespace", err)
				return result, err
			}
		} else {
			result.Namespaces = []string{namespace.Name}
			result.NamespaceLabels[namespace.Name] = copyLabels(namespace.Labels)
		}
	}

	err = eachPage(ctx, func(options metav1.ListOptions) (runtime.Object, error) {
		return client.CoreV1().Pods(namespaceFilter).List(ctx, options)
	}, func(object runtime.Object) error {
		pod, ok := object.(*corev1.Pod)
		if !ok {
			return fmt.Errorf("unexpected Pod object %T", object)
		}
		result.Workloads = append(result.Workloads, Workload{
			Namespace:   pod.Namespace,
			Name:        pod.Name,
			Kind:        "Pod",
			Labels:      copyLabels(pod.Labels),
			Selector:    selectorFromLabels(pod.Labels),
			HostNetwork: pod.Spec.HostNetwork,
		})
		return nil
	})
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Pod discovery failed for %q: %s", namespaceFilter, err.Error()))
		markIncomplete(&result, "pods", err)
	}

	workloads, workloadErrors := collectWorkloads(ctx, client, namespaceFilter)
	for _, workload := range workloads {
		result.Workloads = appendUniqueWorkload(result.Workloads, workload)
	}
	for _, collectionError := range workloadErrors {
		markIncomplete(&result, collectionError.Resource, collectionError.Err)
		result.Warnings = append(result.Warnings, fmt.Sprintf("%s discovery failed: %s", collectionError.Resource, collectionError.Err))
	}

	err = eachPage(ctx, func(options metav1.ListOptions) (runtime.Object, error) {
		return client.NetworkingV1().NetworkPolicies(namespaceFilter).List(ctx, options)
	}, func(object runtime.Object) error {
		policy, ok := object.(*networkingv1.NetworkPolicy)
		if !ok {
			return fmt.Errorf("unexpected NetworkPolicy object %T", object)
		}
		result.NetworkPolicies = append(result.NetworkPolicies, NetworkPolicyRecord{
			Namespace:    policy.Namespace,
			Name:         policy.Name,
			PodSelector:  selectorFromLabelSelector(policy.Spec.PodSelector),
			PolicyTypes:  policyTypesToStrings(discoveredPolicyTypes(*policy)),
			IngressRules: len(policy.Spec.Ingress),
			EgressRules:  len(policy.Spec.Egress),
		})
		return nil
	})
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("NetworkPolicy discovery failed for %q: %s", namespaceFilter, err.Error()))
		markIncomplete(&result, "networkPolicies", err)
	}

	sort.Slice(result.Workloads, func(i, j int) bool {
		return result.Workloads[i].Namespace+"/"+result.Workloads[i].Kind+"/"+result.Workloads[i].Name < result.Workloads[j].Namespace+"/"+result.Workloads[j].Kind+"/"+result.Workloads[j].Name
	})
	sort.Slice(result.NetworkPolicies, func(i, j int) bool {
		return result.NetworkPolicies[i].Namespace+"/"+result.NetworkPolicies[i].Name < result.NetworkPolicies[j].Namespace+"/"+result.NetworkPolicies[j].Name
	})
	if !result.Complete {
		result.Status = "read-only-limited"
		result.Offline = true
	}
	return result, nil
}

func ReadOnlyCapabilityReport(cfg *rest.Config) (CapabilityReport, error) {
	if cfg == nil {
		return CapabilityReport{
			Mode:     "offline",
			Status:   "not-configured",
			Offline:  true,
			Warnings: []string{"No cluster config was provided; offline policy analysis remains fully available."},
		}, nil
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return CapabilityReport{Mode: "cluster", Status: "disabled", Offline: true, Cluster: cfg.Host, Warnings: []string{"Cluster config was present but client-go could not initialize a client."}, Errors: []string{err.Error()}}, err
	}
	result, err := DiscoverReadOnly(context.Background(), client, DiscoveryOptions{AllNamespaces: true, Timeout: 5 * time.Second})
	report := CapabilityReport{
		Mode:       result.Mode,
		Status:     result.Status,
		Offline:    result.Offline,
		Cluster:    cfg.Host,
		Context:    result.Context,
		Warnings:   append([]string{}, result.Warnings...),
		Errors:     append([]string{}, result.Errors...),
		Namespaces: append([]string{}, result.Namespaces...),
	}
	for _, workload := range result.Workloads {
		if workload.Kind == "Pod" {
			report.Pods = append(report.Pods, workload.Namespace+"/"+workload.Name)
		}
	}
	for _, policy := range result.NetworkPolicies {
		report.Policies = append(report.Policies, policy.Namespace+"/"+policy.Name)
	}
	if err != nil {
		return report, err
	}
	return report, nil
}

func selectorFromLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+labels[key])
	}
	return strings.Join(parts, ", ")
}

type collectionError struct {
	Resource string
	Err      error
}

func collectWorkloads(ctx context.Context, client kubernetes.Interface, namespaceFilter string) ([]Workload, []collectionError) {
	workloads := make([]Workload, 0)
	errors := make([]collectionError, 0)
	collect := func(resource string, list func(metav1.ListOptions) (runtime.Object, error), handle func(runtime.Object) error) {
		if err := eachPage(ctx, list, handle); err != nil {
			errors = append(errors, collectionError{Resource: resource, Err: err})
		}
	}
	collect("deployments", func(options metav1.ListOptions) (runtime.Object, error) {
		return client.AppsV1().Deployments(namespaceFilter).List(ctx, options)
	}, func(object runtime.Object) error {
		deployment, ok := object.(*appsv1.Deployment)
		if !ok {
			return fmt.Errorf("unexpected Deployment object %T", object)
		}
		workloads = append(workloads, Workload{
			Namespace: deployment.Namespace, Name: deployment.Name, Kind: "Deployment",
			Labels: copyLabels(deployment.Spec.Template.Labels), Selector: selectorFromLabels(deployment.Spec.Template.Labels),
			HostNetwork: deployment.Spec.Template.Spec.HostNetwork,
		})
		return nil
	})
	collect("daemonSets", func(options metav1.ListOptions) (runtime.Object, error) {
		return client.AppsV1().DaemonSets(namespaceFilter).List(ctx, options)
	}, func(object runtime.Object) error {
		daemonSet, ok := object.(*appsv1.DaemonSet)
		if !ok {
			return fmt.Errorf("unexpected DaemonSet object %T", object)
		}
		workloads = append(workloads, Workload{
			Namespace: daemonSet.Namespace, Name: daemonSet.Name, Kind: "DaemonSet",
			Labels: copyLabels(daemonSet.Spec.Template.Labels), Selector: selectorFromLabels(daemonSet.Spec.Template.Labels),
			HostNetwork: daemonSet.Spec.Template.Spec.HostNetwork,
		})
		return nil
	})
	collect("statefulSets", func(options metav1.ListOptions) (runtime.Object, error) {
		return client.AppsV1().StatefulSets(namespaceFilter).List(ctx, options)
	}, func(object runtime.Object) error {
		statefulSet, ok := object.(*appsv1.StatefulSet)
		if !ok {
			return fmt.Errorf("unexpected StatefulSet object %T", object)
		}
		workloads = append(workloads, Workload{
			Namespace: statefulSet.Namespace, Name: statefulSet.Name, Kind: "StatefulSet",
			Labels: copyLabels(statefulSet.Spec.Template.Labels), Selector: selectorFromLabels(statefulSet.Spec.Template.Labels),
			HostNetwork: statefulSet.Spec.Template.Spec.HostNetwork,
		})
		return nil
	})
	collect("jobs", func(options metav1.ListOptions) (runtime.Object, error) {
		return client.BatchV1().Jobs(namespaceFilter).List(ctx, options)
	}, func(object runtime.Object) error {
		job, ok := object.(*batchv1.Job)
		if !ok {
			return fmt.Errorf("unexpected Job object %T", object)
		}
		workloads = append(workloads, Workload{
			Namespace: job.Namespace, Name: job.Name, Kind: "Job",
			Labels: copyLabels(job.Spec.Template.Labels), Selector: selectorFromLabels(job.Spec.Template.Labels),
			HostNetwork: job.Spec.Template.Spec.HostNetwork,
		})
		return nil
	})
	collect("cronJobs", func(options metav1.ListOptions) (runtime.Object, error) {
		return client.BatchV1().CronJobs(namespaceFilter).List(ctx, options)
	}, func(object runtime.Object) error {
		cronJob, ok := object.(*batchv1.CronJob)
		if !ok {
			return fmt.Errorf("unexpected CronJob object %T", object)
		}
		template := cronJob.Spec.JobTemplate.Spec.Template
		workloads = append(workloads, Workload{
			Namespace: cronJob.Namespace, Name: cronJob.Name, Kind: "CronJob",
			Labels: copyLabels(template.Labels), Selector: selectorFromLabels(template.Labels),
			HostNetwork: template.Spec.HostNetwork,
		})
		return nil
	})
	return workloads, errors
}

func eachPage(ctx context.Context, list func(metav1.ListOptions) (runtime.Object, error), handle func(runtime.Object) error) error {
	return pager.New(pager.SimplePageFunc(list)).EachListItem(ctx, metav1.ListOptions{Limit: 500}, handle)
}

func markIncomplete(result *DiscoveryResult, resource string, err error) {
	result.Complete = false
	result.Errors = append(result.Errors, err.Error())
	result.Incomplete = append(result.Incomplete, resource)
}

func copyLabels(source map[string]string) map[string]string {
	labels := make(map[string]string, len(source))
	for key, value := range source {
		labels[key] = value
	}
	return labels
}

func selectorFromLabelSelector(selector metav1.LabelSelector) string {
	parts := make([]string, 0, len(selector.MatchLabels)+len(selector.MatchExpressions))
	for key, value := range selector.MatchLabels {
		parts = append(parts, key+"="+value)
	}
	for _, expression := range selector.MatchExpressions {
		parts = append(parts, fmt.Sprintf("%s %s %s", expression.Key, expression.Operator, strings.Join(expression.Values, ",")))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func appendUniqueWorkload(workloads []Workload, candidate Workload) []Workload {
	for _, existing := range workloads {
		if existing.Namespace == candidate.Namespace && existing.Kind == candidate.Kind && existing.Name == candidate.Name {
			return workloads
		}
	}
	return append(workloads, candidate)
}

func policyTypesToStrings(policyTypes []networkingv1.PolicyType) []string {
	result := make([]string, 0, len(policyTypes))
	for _, pt := range policyTypes {
		result = append(result, string(pt))
	}
	return result
}

func discoveredPolicyTypes(policy networkingv1.NetworkPolicy) []networkingv1.PolicyType {
	if len(policy.Spec.PolicyTypes) > 0 {
		return policy.Spec.PolicyTypes
	}
	types := []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
	if len(policy.Spec.Egress) > 0 {
		types = append(types, networkingv1.PolicyTypeEgress)
	}
	return types
}
