package config

import (
	"os"
	"strconv"
	"strings"
)

// Kubernetes connection mode values for PCI_KUBE_MODE.
const (
	KubeModeAuto       = "auto"
	KubeModeInCluster  = "in-cluster"
	KubeModeKubeconfig = "kubeconfig"
)

// Config contains CLI and service runtime settings.
type Config struct {
	Address            string
	Port               int
	Verbose            bool
	Context            string
	Namespace          string
	KubeconfigPath     string
	KubeContext        string
	KubeMode           string
	AllNamespaces      bool
	Output             string
	Format             string
	DataDir            string
	StaticDir          string
	SampleBasePath     string
	SampleProposedPath string
	MaxBody            int64
}

func Default() Config {
	return Config{
		Address: "127.0.0.1",
		Port:    8080,
		// Context / KubeContext default empty so client-go uses the kubeconfig current-context
		// instead of forcing a non-existent context named "default".
		Context:            "",
		Namespace:          "default",
		KubeconfigPath:     "",
		KubeContext:        "",
		KubeMode:           KubeModeAuto,
		AllNamespaces:      false,
		Output:             "json",
		Format:             "json",
		DataDir:            "",
		StaticDir:          "",
		SampleBasePath:     "fixtures/base",
		SampleProposedPath: "fixtures/proposed",
		MaxBody:            1 << 20,
	}
}

// InClusterEnvironment reports whether the process appears to run inside Kubernetes.
func InClusterEnvironment() bool {
	return strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_HOST")) != ""
}

func LoadFromEnv() Config {
	cfg := Default()
	if val := os.Getenv("PCI_ADDRESS"); val != "" {
		cfg.Address = val
	} else if InClusterEnvironment() {
		// Pods must listen on all interfaces for Service traffic.
		cfg.Address = "0.0.0.0"
	}
	if val := os.Getenv("PCI_PORT"); val != "" {
		if p, err := strconv.Atoi(val); err == nil {
			cfg.Port = p
		}
	}
	if val := os.Getenv("PCI_CONTEXT"); val != "" {
		cfg.Context = val
	}
	if val := os.Getenv("PCI_NAMESPACE"); val != "" {
		cfg.Namespace = val
	}
	if val := os.Getenv("PCI_KUBECONFIG"); val != "" {
		cfg.KubeconfigPath = val
	}
	if val := os.Getenv("PCI_KUBECONFIG_PATH"); val != "" {
		cfg.KubeconfigPath = val
	}
	if val := os.Getenv("PCI_KUBE_CONTEXT"); val != "" {
		cfg.KubeContext = val
	}
	if val := os.Getenv("PCI_CONTEXT"); val != "" {
		cfg.KubeContext = val
	}
	if val := os.Getenv("PCI_KUBE_MODE"); val != "" {
		cfg.KubeMode = strings.ToLower(strings.TrimSpace(val))
	}
	if val := os.Getenv("PCI_ALL_NAMESPACES"); val != "" {
		cfg.AllNamespaces = val == "true"
	} else if InClusterEnvironment() {
		cfg.AllNamespaces = true
	}
	if val := os.Getenv("PCI_OUTPUT"); val != "" {
		cfg.Output = val
	}
	if val := os.Getenv("PCI_FORMAT"); val != "" {
		cfg.Format = val
	}
	if val := os.Getenv("PCI_DATA_DIR"); val != "" {
		cfg.DataDir = val
	} else if InClusterEnvironment() {
		cfg.DataDir = "/var/lib/pci/pci.db"
	}
	if val := os.Getenv("PCI_STATIC_DIR"); val != "" {
		cfg.StaticDir = val
	} else if InClusterEnvironment() {
		cfg.StaticDir = "/app/ui"
	}
	if val := os.Getenv("PCI_SAMPLE_BASE"); val != "" {
		cfg.SampleBasePath = val
	} else if InClusterEnvironment() {
		cfg.SampleBasePath = "/app/fixtures/base"
	}
	if val := os.Getenv("PCI_SAMPLE_PROPOSED"); val != "" {
		cfg.SampleProposedPath = val
	} else if InClusterEnvironment() {
		cfg.SampleProposedPath = "/app/fixtures/proposed"
	}
	if val := os.Getenv("PCI_MAX_BODY"); val != "" {
		if n, err := strconv.ParseInt(val, 10, 64); err == nil && n > 0 {
			cfg.MaxBody = n
		}
	}
	if cfg.KubeContext == "" {
		cfg.KubeContext = cfg.Context
	}
	if cfg.KubeMode == "" {
		cfg.KubeMode = KubeModeAuto
	}
	cfg.Verbose = os.Getenv("PCI_VERBOSE") == "true"
	return cfg
}
