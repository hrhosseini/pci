# PolicyChangeImpact (pci)

PolicyChangeImpact is an offline-first Kubernetes NetworkPolicy change-impact analyzer. It combines a Go analysis engine, SQLite persistence, an HTTP API, and a React UI.

Optional **read-only cluster discovery** uses either a local kubeconfig or an in-cluster ServiceAccount (Helm). Discovery never mutates cluster resources.

## Status (v0.1.0)

Working analysis product with API, UI, Helm chart, and container image. **Not** a multi-tenant SaaS: no API authentication, no production runtime probe provider, no controlled policy apply. Modeled predictions and observations stay distinct. See [docs/IMPLEMENTATION_STATUS.md](docs/IMPLEMENTATION_STATUS.md) and [docs/SECURITY.md](docs/SECURITY.md).

## Prerequisites

- Kubernetes 1.28+ (tested against client-go 0.32)
- Helm 3.14+
- Ability to create namespaces, Deployments, Services, ServiceAccounts, and (for default install) ClusterRoles

## Install from public OCI (no clone required)

```bash
helm install pci oci://ghcr.io/hrhosseini/charts/pci \
  --version 0.1.0 \
  --namespace pci-system \
  --create-namespace
```

Inspect the chart:

```bash
helm show chart oci://ghcr.io/hrhosseini/charts/pci --version 0.1.0
helm template pci oci://ghcr.io/hrhosseini/charts/pci --version 0.1.0 -n pci-system
```

Open the UI (same origin as `/api/v1`):

```bash
kubectl -n pci-system port-forward svc/pci 8080:80
# http://127.0.0.1:8080/
```

Published artifacts:

| Artifact | Reference |
|----------|-----------|
| Image | `ghcr.io/hrhosseini/pci:0.1.0` |
| Helm chart | `oci://ghcr.io/hrhosseini/charts/pci` version `0.1.0` |
| GitHub Release | https://github.com/hrhosseini/pci/releases/tag/v0.1.0 |

## Upgrade and uninstall

```bash
helm upgrade pci oci://ghcr.io/hrhosseini/charts/pci --version 0.1.0 -n pci-system
helm uninstall pci -n pci-system
```

## Important values

| Value | Default | Meaning |
|-------|---------|---------|
| `image.repository` / `image.tag` | `ghcr.io/hrhosseini/pci` / `0.1.0` | Container image |
| `replicaCount` | `1` | Must stay 1 with SQLite |
| `config.kubeMode` | `in-cluster` | ServiceAccount auth |
| `rbac.scope` | `cluster` | Read-only all-namespace discovery |
| `persistence.enabled` | `true` | PVC for `/var/lib/pci` |
| `ingress.enabled` | `false` | Off by default |
| `service.type` | `ClusterIP` | Not public by default |

Full chart docs: [charts/pci/README.md](charts/pci/README.md).

## Persistence and backup

SQLite lives on the PVC at `/var/lib/pci/pci.db`. Keep a single replica. Backup by stopping the Pod and copying `pci.db` (and `-wal`/`-shm` if present). Restore onto an empty volume before starting.

## RBAC and network security

- Chart RBAC is **get/list/watch** only; no NetworkPolicy writes; no cluster-admin.
- The HTTP API is **unauthenticated**. Prefer ClusterIP + port-forward, or an authenticated Ingress / NetworkPolicy.
- Troubleshooting permission errors: [docs/SECURITY.md](docs/SECURITY.md).

## Troubleshooting

| Symptom | Check |
|---------|--------|
| ImagePullBackOff | Package visibility on GHCR; `image.repository` / `image.tag` |
| CrashLoop / not ready | `kubectl -n pci-system logs deploy/pci`; `/readyz` needs SQLite path writable |
| Discovery permission-denied | ServiceAccount bindings; `kubectl auth can-i list networkpolicies --as=system:serviceaccount:pci-system:pci -A` |
| Empty UI / API errors | Port-forward to Service port 80; confirm `/healthz` and `/api/v1/health` |

## Local development

```bash
go test ./...
go run ./cmd/pci serve --kubeconfig ~/.kube/config --kube-context <ctx> --all-namespaces
cd web && npm ci && npm run dev
```

Connection mode (`--kube-mode` / `PCI_KUBE_MODE`): `auto` \| `in-cluster` \| `kubeconfig`. Credentials are never logged.

Build the image locally:

```bash
docker build -t ghcr.io/hrhosseini/pci:0.1.0 .
```

## Components

- CLI: `analyze`, `diff`, `connectivity`, `discover`, `serve`, …
- Static NetworkPolicy analysis and semantic diff
- Read-only Kubernetes discovery (paginated, scoped)
- SQLite-backed analyses/snapshots
- React UI consuming real API JSON (no demo fixtures as live data)

## Known limitations (v0.1.0)

- No API authentication
- No production runtime connectivity probes
- No policy apply/delete
- SQLite is single-replica only
- Discovery summaries are not a complete flow-evaluation inventory
- Optional kind/Calico live harness is opt-in and confirmation-gated ([docs/LIVE_VALIDATION.md](docs/LIVE_VALIDATION.md))
