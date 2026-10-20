# PCI Helm Chart

Deploys PolicyChangeImpact with in-cluster ServiceAccount authentication, read-only RBAC, optional PVC for SQLite, and same-origin UI served by the Go API.

## Public install (OCI)

```bash
helm install pci oci://ghcr.io/hrhosseini/charts/pci \
  --version 0.1.0 \
  --namespace pci-system \
  --create-namespace
```

Inspect without installing:

```bash
helm show chart oci://ghcr.io/hrhosseini/charts/pci --version 0.1.0
helm template pci oci://ghcr.io/hrhosseini/charts/pci --version 0.1.0 -n pci-system
```

Port-forward the UI/API:

```bash
kubectl -n pci-system port-forward svc/pci 8080:80
# open http://127.0.0.1:8080/
```

## Upgrade / rollback / uninstall

```bash
helm upgrade pci oci://ghcr.io/hrhosseini/charts/pci --version 0.1.0 -n pci-system
helm rollback pci -n pci-system
helm uninstall pci -n pci-system
```

Uninstall does not delete the PVC by default on some clusters; delete `pci-data` manually if you intend to drop stored analyses/snapshots.

## Persistence and backup

- Default: PVC at `/var/lib/pci` with SQLite file `/var/lib/pci/pci.db`.
- **`replicaCount` must remain `1`**. SQLite is not safe for multi-writer replicas.
- Backup: scale to 0 or stop the Pod, then copy the PVC contents / `pci.db*` files (including WAL/SHM if present).
- Restore: restore files onto an empty PVC before starting the Pod.

## Values highlights

| Key | Default | Notes |
|-----|---------|-------|
| `image.repository` | `ghcr.io/hrhosseini/pci` | Published image |
| `image.tag` | `0.1.0` | Keep aligned with chart `appVersion` |
| `replicaCount` | `1` | Required for SQLite |
| `config.kubeMode` | `in-cluster` | Uses the chart ServiceAccount; no kubeconfig mount |
| `rbac.scope` | `cluster` | ClusterRole for all-namespace discovery |
| `rbac.scope=namespace` | — | Requires `config.allNamespaces=false` |
| `persistence.enabled` | `true` | Durable analyses/snapshots |
| `ingress.enabled` | `false` | Optional; no controller assumed |
| `service.type` | `ClusterIP` | Not exposed to the internet by default |

## RBAC

Default rules are **get/list/watch** only on namespaces, pods, services, endpoints, apps/batch workloads, and NetworkPolicies. The chart never grants create/update/patch/delete on NetworkPolicies and never creates cluster-admin bindings.

See [docs/SECURITY.md](../../docs/SECURITY.md) for permission troubleshooting.

## Security notes

- The API has no authentication. Keep `service.type=ClusterIP` and use port-forward or an authenticated Ingress.
- `PCI_ALLOW_NON_LOOPBACK=1` is set so the Pod can receive Service traffic.
- TLS verification is never disabled by the chart.
