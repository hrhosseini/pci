# CLI Reference

## Supported commands

### `pci analyze`
Analyze a directory or file containing Kubernetes NetworkPolicy YAML/JSON manifests.

Example:
```bash
go run ./cmd/pci analyze --path ./fixtures/base
```

### `pci diff`
Compare baseline and proposed policy states.

Example:
```bash
go run ./cmd/pci diff --base ./fixtures/base --proposed ./fixtures/proposed
```

### `pci explain`
Explain a structured finding using a JSON analysis payload.

Example:
```bash
go run ./cmd/pci explain finding-001 --analysis /tmp/finding-analysis.json
```

### `pci snapshot`
Create or inspect local snapshots of a path or manifest.

Example:
```bash
go run ./cmd/pci snapshot create --path ./fixtures/base
```

### `pci compare`
Compare two snapshots or manifest paths.

Example:
```bash
go run ./cmd/pci compare --before ./fixtures/base --after ./fixtures/proposed
```

### `pci verify`
Validate a constrained JSON source-workload/destination/protocol/port/timeout request. It reports `probeExecuted: false` because no production runtime provider is configured. It never accepts or executes shell commands.

### `pci connectivity`
Evaluate a flow against a provided NetworkPolicy manifest set and JSON query. The query must include endpoint identities, label completeness, and `policyInventoryComplete`.

Example query and run:
```bash
cat > /tmp/pci-query.json <<'EOF'
{
  "source": {
    "namespace": "backend",
    "name": "web",
    "labels": {"app": "web"},
    "labelsComplete": true,
    "namespaceLabelsKnown": true
  },
  "destination": {
    "namespace": "backend",
    "name": "api",
    "labels": {"app": "api"},
    "labelsComplete": true,
    "namespaceLabelsKnown": true
  },
  "protocol": "TCP",
  "port": 8080,
  "policyInventoryComplete": true
}
EOF
go run ./cmd/pci connectivity --manifest ./fixtures/base/network.yaml --query /tmp/pci-query.json
```

### `pci connectivity-diff`
Compare the same flow against baseline and proposed policy manifest sets, reporting `newly_allowed`, `newly_denied`, `uncertain`, or `unchanged` as a model result.

```bash
go run ./cmd/pci connectivity-diff \
  --baseline ./fixtures/base/network.yaml \
  --proposed ./fixtures/proposed/network.yaml \
  --query /tmp/pci-query.json
```

### `pci graph`
Serialize the modeled connectivity graph as JSON.

### `pci report`
Render a report object from an analysis JSON file.

### `pci doctor`
Return a basic configuration and runtime readiness check.

### `pci serve`
Run the local HTTP backend used by the React UI. Persistent kubeconfig flags are honored (unlike earlier builds that reset to `config.Default()`).

```bash
# Use ~/.kube/config current-context (for example kind-kind when selected)
pci serve --all-namespaces

# Or pin kubeconfig + context explicitly
pci serve --kubeconfig ~/.kube/config --kube-context kind-kind --all-namespaces
```

Environment equivalent for `pci-server`:

```bash
export PCI_KUBECONFIG=$HOME/.kube/config
export PCI_KUBE_CONTEXT=kind-kind
export PCI_ALL_NAMESPACES=true
go run ./cmd/pci-server
```

Discovery remains read-only via `GET /api/v1/clusters/{id}/discover`.

### `pci version`
Display version and available capabilities.

### `pci completion`
Generate shell completion scripts for the configured shell.
