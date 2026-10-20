package config

import (
	"fmt"
	"strings"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ConfigMeta describes how the Kubernetes client was resolved without exposing secrets.
type ConfigMeta struct {
	Path    string
	Context string
	Mode    string // in-cluster | kubeconfig
	Source  string // human-readable resolution reason (no secrets)
}

// LoadConfig loads kubeconfig or in-cluster config using auto mode precedence.
func LoadConfig(kubeconfigPath, context string) (*rest.Config, error) {
	cfg, _, err := LoadConfigWithMeta(KubeModeAuto, kubeconfigPath, context)
	return cfg, err
}

// LoadConfigWithMeta loads Kubernetes client configuration with an explicit mode.
//
// Precedence for mode "auto":
//  1. Explicit kubeconfig path (PCI_KUBECONFIG / --kubeconfig) → kubeconfig only
//     (no silent in-cluster fallback on failure).
//  2. Else if KUBERNETES_SERVICE_HOST is set → in-cluster config.
//  3. Else → kubeconfig loading rules (KUBECONFIG env, then ~/.kube/config).
//
// Modes "in-cluster" and "kubeconfig" force that path and fail if unavailable.
// Credentials, tokens, and certificate material are never returned in ConfigMeta.
func LoadConfigWithMeta(mode, kubeconfigPath, context string) (*rest.Config, ConfigMeta, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = KubeModeAuto
	}
	kubeconfigPath = strings.TrimSpace(kubeconfigPath)
	context = strings.TrimSpace(context)

	switch mode {
	case KubeModeInCluster:
		return loadInCluster(context)
	case KubeModeKubeconfig:
		return loadKubeconfig(kubeconfigPath, context)
	case KubeModeAuto:
		if kubeconfigPath != "" {
			return loadKubeconfig(kubeconfigPath, context)
		}
		if InClusterEnvironment() {
			return loadInCluster(context)
		}
		return loadKubeconfig("", context)
	default:
		return nil, ConfigMeta{}, fmt.Errorf("unknown kubernetes mode %q (want auto|in-cluster|kubeconfig)", mode)
	}
}

func loadInCluster(context string) (*rest.Config, ConfigMeta, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, ConfigMeta{
			Path:   "in-cluster",
			Mode:   KubeModeInCluster,
			Source: "in-cluster (service account)",
		}, fmt.Errorf("in-cluster config: %w", err)
	}
	applyClientDefaults(cfg)
	return cfg, ConfigMeta{
		Path:    "in-cluster",
		Mode:    KubeModeInCluster,
		Context: context,
		Source:  "in-cluster (service account)",
	}, nil
}

func loadKubeconfig(kubeconfigPath, context string) (*rest.Config, ConfigMeta, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		rules.ExplicitPath = kubeconfigPath
	}
	overrides := &clientcmd.ConfigOverrides{}
	if context != "" {
		overrides.CurrentContext = context
	}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
	cfg, err := clientConfig.ClientConfig()
	pathHint := kubeconfigPath
	if pathHint == "" {
		pathHint = rules.GetDefaultFilename()
	}
	if err != nil {
		return nil, ConfigMeta{
			Path:    pathHint,
			Context: context,
			Mode:    KubeModeKubeconfig,
			Source:  "kubeconfig",
		}, fmt.Errorf("load kubeconfig %q: %w", pathHint, err)
	}
	applyClientDefaults(cfg)
	resolvedContext := context
	if resolvedContext == "" {
		if raw, rawErr := clientConfig.RawConfig(); rawErr == nil {
			resolvedContext = raw.CurrentContext
		}
	}
	resolvedPath := kubeconfigPath
	if resolvedPath == "" {
		resolvedPath = rules.GetDefaultFilename()
	}
	return cfg, ConfigMeta{
		Path:    resolvedPath,
		Context: resolvedContext,
		Mode:    KubeModeKubeconfig,
		Source:  "kubeconfig",
	}, nil
}

func applyClientDefaults(cfg *rest.Config) {
	if cfg == nil {
		return
	}
	cfg.QPS = 20
	cfg.Burst = 40
	if cfg.UserAgent == "" {
		cfg.UserAgent = "pci"
	}
}
