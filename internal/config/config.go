package config

import (
	"os"
	"strconv"
)

// Config contains CLI and service runtime settings.
type Config struct {
	Address         string
	Port            int
	Verbose         bool
	Context         string
	Namespace       string
	KubeconfigPath  string
	KubeContext     string
	AllNamespaces   bool
	Output          string
	Format          string
	DataDir         string
	MaxBody         int64
}

func Default() Config {
	return Config{
		Address:        "127.0.0.1",
		Port:           8080,
		Context:        "default",
		Namespace:      "default",
		KubeconfigPath: "",
		KubeContext:    "",
		AllNamespaces:  false,
		Output:         "json",
		Format:         "json",
		DataDir:        "",
		MaxBody:        1 << 20,
	}
}

func LoadFromEnv() Config {
	cfg := Default()
	if val := os.Getenv("PCI_ADDRESS"); val != "" {
		cfg.Address = val
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
	if val := os.Getenv("PCI_ALL_NAMESPACES"); val != "" {
		cfg.AllNamespaces = val == "true"
	}
	if val := os.Getenv("PCI_OUTPUT"); val != "" {
		cfg.Output = val
	}
	if val := os.Getenv("PCI_FORMAT"); val != "" {
		cfg.Format = val
	}
	if val := os.Getenv("PCI_DATA_DIR"); val != "" {
		cfg.DataDir = val
	}
	if val := os.Getenv("PCI_MAX_BODY"); val != "" {
		if n, err := strconv.ParseInt(val, 10, 64); err == nil && n > 0 {
			cfg.MaxBody = n
		}
	}
	if cfg.KubeContext == "" {
		cfg.KubeContext = cfg.Context
	}
	cfg.Verbose = os.Getenv("PCI_VERBOSE") == "true"
	return cfg
}
