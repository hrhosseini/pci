package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDemoKubeconfig(t *testing.T, dir, currentContext string) string {
	t.Helper()
	file := filepath.Join(dir, "config")
	contents := `apiVersion: v1
kind: Config
current-context: ` + currentContext + `
contexts:
- name: localctx
  context:
    cluster: local
    user: local
- name: otherctx
  context:
    cluster: local
    user: local
clusters:
- name: local
  cluster:
    server: https://example.invalid:6443
users:
- name: local
  user:
    token: demo-token
`
	if err := os.WriteFile(file, []byte(contents), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return file
}

func TestLoadConfigWithMetaKubeconfigMode(t *testing.T) {
	file := writeDemoKubeconfig(t, t.TempDir(), "localctx")
	cfg, meta, err := LoadConfigWithMeta(KubeModeKubeconfig, file, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if meta.Mode != KubeModeKubeconfig {
		t.Fatalf("mode: %#v", meta)
	}
	if meta.Context != "localctx" {
		t.Fatalf("context: %#v", meta)
	}
	if !strings.HasPrefix(cfg.Host, "https://") {
		t.Fatalf("host: %q", cfg.Host)
	}
}

func TestLoadConfigWithMetaRespectsExplicitContext(t *testing.T) {
	file := writeDemoKubeconfig(t, t.TempDir(), "localctx")
	_, meta, err := LoadConfigWithMeta(KubeModeAuto, file, "otherctx")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if meta.Context != "otherctx" || meta.Path != file {
		t.Fatalf("meta: %#v", meta)
	}
}

func TestLoadConfigWithMetaRejectsMissingExplicitPath(t *testing.T) {
	_, _, err := LoadConfigWithMeta(KubeModeAuto, filepath.Join(t.TempDir(), "missing"), "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadConfigWithMetaRejectsInvalidMode(t *testing.T) {
	_, _, err := LoadConfigWithMeta("bogus", "", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadConfigWithMetaInClusterModeFailsOutsideCluster(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	_, meta, err := LoadConfigWithMeta(KubeModeInCluster, "", "")
	if err == nil {
		t.Fatal("expected in-cluster failure outside a pod")
	}
	if meta.Mode != KubeModeInCluster {
		t.Fatalf("meta: %#v", meta)
	}
}

func TestLoadConfigWithMetaAutoPrefersExplicitKubeconfigOverInClusterEnv(t *testing.T) {
	file := writeDemoKubeconfig(t, t.TempDir(), "localctx")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	cfg, meta, err := LoadConfigWithMeta(KubeModeAuto, file, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if meta.Mode != KubeModeKubeconfig {
		t.Fatalf("expected kubeconfig mode when path is explicit, got %#v", meta)
	}
	if !strings.HasPrefix(cfg.Host, "https://example.invalid") {
		t.Fatalf("host: %q", cfg.Host)
	}
}

func TestLoadFromEnvInClusterDefaults(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.96.0.1")
	t.Setenv("PCI_ADDRESS", "")
	t.Setenv("PCI_DATA_DIR", "")
	t.Setenv("PCI_STATIC_DIR", "")
	t.Setenv("PCI_ALL_NAMESPACES", "")
	cfg := LoadFromEnv()
	if cfg.Address != "0.0.0.0" {
		t.Fatalf("address: %q", cfg.Address)
	}
	if cfg.DataDir != "/var/lib/pci/pci.db" {
		t.Fatalf("dataDir: %q", cfg.DataDir)
	}
	if cfg.StaticDir != "/app/ui" {
		t.Fatalf("staticDir: %q", cfg.StaticDir)
	}
	if !cfg.AllNamespaces {
		t.Fatal("expected AllNamespaces true in cluster")
	}
}

func TestInClusterEnvironment(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	if InClusterEnvironment() {
		t.Fatal("expected false")
	}
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	if !InClusterEnvironment() {
		t.Fatal("expected true")
	}
}
