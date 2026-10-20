# Multi-stage production image for PolicyChangeImpact (PCI).
# Builds the React UI and Go server; runs as non-root with an explicit data path.

FROM node:22-bookworm-slim AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.24-bookworm AS go-build
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev libsqlite3-dev \
  && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-build /src/web/dist /src/web/dist
ENV CGO_ENABLED=1
RUN go build -trimpath -ldflags="-s -w" -o /out/pci ./cmd/pci \
  && go build -trimpath -ldflags="-s -w" -o /out/pci-server ./cmd/pci-server

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libsqlite3-0 \
  && rm -rf /var/lib/apt/lists/* \
  && groupadd --gid 65532 pci \
  && useradd --uid 65532 --gid 65532 --home-dir /var/lib/pci --create-home --shell /usr/sbin/nologin pci \
  && mkdir -p /app/ui /var/lib/pci \
  && chown -R pci:pci /var/lib/pci

COPY --from=go-build /out/pci /usr/local/bin/pci
COPY --from=go-build /out/pci-server /usr/local/bin/pci-server
COPY --from=web-build /src/web/dist /app/ui
# Sample NetworkPolicy manifests for offline analysis in the UI (not live cluster data).
COPY --from=go-build /src/fixtures /app/fixtures

ENV PCI_ADDRESS=0.0.0.0 \
    PCI_PORT=8080 \
    PCI_KUBE_MODE=in-cluster \
    PCI_ALL_NAMESPACES=true \
    PCI_STATIC_DIR=/app/ui \
    PCI_DATA_DIR=/var/lib/pci/pci.db \
    PCI_SAMPLE_BASE=/app/fixtures/base \
    PCI_SAMPLE_PROPOSED=/app/fixtures/proposed \
    PCI_ALLOW_NON_LOOPBACK=1

USER pci:pci
WORKDIR /var/lib/pci
EXPOSE 8080
# Kubernetes readiness/liveness probes should hit /readyz and /healthz.
ENTRYPOINT ["/usr/local/bin/pci-server"]
