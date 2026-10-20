# PCI Web UI

React + TypeScript UI for PolicyChangeImpact.

## Local development

1. Start the API with Kubernetes discovery configured:

```bash
# from repo root
go run ./cmd/pci serve --kubeconfig ~/.kube/config --kube-context kind-kind --all-namespaces
```

2. Start Vite (proxies `/api` → `127.0.0.1:8080`):

```bash
npm install
npm run dev
```

Dashboard auto-runs read-only discovery on load. **Discover / Refresh** re-fetches `GET /api/v1/clusters/local/discover`.

## Production

The container image embeds `web/dist` at `/app/ui`. `pci-server` serves the SPA and `/api/v1` on the same origin (Helm Service/Ingress). Do not use the Vite dev server in production.

```bash
npm run build   # outputs dist/
npm test        # discovery status label checks
npm run lint
```
