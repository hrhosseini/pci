import { useCallback, useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import './App.css'
import * as api from './api'
import {
  Badge,
  Button,
  ConnectionStrip,
  EmptyState,
  ErrorBanner,
  Icon,
  PageHeader,
  Panel,
  Skeleton,
} from './components'
import { NAV_ITEMS, severityTone } from './nav'
import { discoveryStripLabel, summarizeDiscoveryError } from './discoveryStatus'
import type {
  Analysis,
  CapabilitiesResponse,
  ClusterDiscoveryResponse,
  DiffReport,
  Explanation,
  Graph,
  HealthResponse,
  Report,
  Route,
  Snapshot,
  VerificationEvidence,
} from './types'

type AppData = {
  health: HealthResponse | null
  capabilities: CapabilitiesResponse | null
  analysis: Analysis | null
  graph: Graph | null
  diff: DiffReport | null
  snapshots: Snapshot[]
  report: Report | null
  verificationEvidence: VerificationEvidence[]
  verificationEnabled: boolean
  clusterDiscovery: ClusterDiscoveryResponse | null
  discoveryLoading: boolean
  discoveryError: string | null
  apiConnected: boolean
  bootLoading: boolean
  dataLoading: boolean
  error: string | null
  errorDetail?: string
  actionError: string | null
  selectedFinding: Explanation | null
}

const initialData: AppData = {
  health: null,
  capabilities: null,
  analysis: null,
  graph: null,
  diff: null,
  snapshots: [],
  report: null,
  verificationEvidence: [],
  verificationEnabled: false,
  clusterDiscovery: null,
  discoveryLoading: false,
  discoveryError: null,
  apiConnected: false,
  bootLoading: true,
  dataLoading: false,
  error: null,
  actionError: null,
  selectedFinding: null,
}

const DEFAULT_MANIFEST = `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: demo-allow
  namespace: default
spec:
  podSelector:
    matchLabels:
      app: api
  policyTypes:
    - Ingress
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: web
      ports:
        - protocol: TCP
          port: 8080
`

function App() {
  const [route, setRoute] = useState<Route>('dashboard')
  const [data, setData] = useState<AppData>(initialData)
  const [workspacePath, setWorkspacePath] = useState('fixtures/base')
  const [workspacePathDraft, setWorkspacePathDraft] = useState('fixtures/base')
  const [analysisPath, setAnalysisPath] = useState('fixtures/base')
  const [proposedPath, setProposedPath] = useState('fixtures/proposed')
  const [manifestText, setManifestText] = useState(DEFAULT_MANIFEST)
  const [namespaceFilter, setNamespaceFilter] = useState('all')
  const [searchText, setSearchText] = useState('')
  const [severityFilter, setSeverityFilter] = useState('all')
  const [changedOnly, setChangedOnly] = useState(false)
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)
  const [selectedFindingId, setSelectedFindingId] = useState<string | null>(null)
  const [policyQuery, setPolicyQuery] = useState('')
  const [expandedPolicy, setExpandedPolicy] = useState<string | null>(null)
  const [zoom, setZoom] = useState(1)
  const [pan, setPan] = useState({ x: 0, y: 0 })
  const [panning, setPanning] = useState(false)
  const panOrigin = useRef<{ x: number; y: number; panX: number; panY: number } | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const loadWorkspace = useCallback(async (path: string) => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    const { signal } = controller

    setData((prev) => ({
      ...prev,
      bootLoading: prev.health == null,
      dataLoading: true,
      error: null,
      errorDetail: undefined,
      actionError: null,
    }))

    try {
      const health = await api.getHealth(signal)
      const capabilities = await api.getCapabilities(signal)
      setData((prev) => ({
        ...prev,
        health,
        capabilities,
        apiConnected: Boolean(health.ok),
        bootLoading: false,
        verificationEnabled: Boolean(capabilities.verification?.enabled),
      }))

      const analysesResponse = await api.listAnalyses(signal)
      let analysis: Analysis | null = null
      if (analysesResponse.analyses?.[0]?.id) {
        analysis = await api.getAnalysis(analysesResponse.analyses[0].id, signal)
      }
      if (!analysis) {
        analysis = await api.createAnalysis({ path }, signal)
      }

      const [graph, snapshotsResponse, verification, diff] = await Promise.all([
        analysis.id ? api.getGraph(analysis.id, signal) : Promise.resolve(null),
        api.listSnapshots(signal),
        api.listVerifications(signal),
        api.postDiff(path, 'fixtures/proposed', signal).catch(() => null),
      ])

      const snapshots = await Promise.all(
        (snapshotsResponse.snapshots ?? []).slice(0, 8).map((item) => api.getSnapshot(item.id, signal)),
      )

      if (signal.aborted) return

      setData((prev) => ({
        ...prev,
        health,
        capabilities,
        analysis,
        graph,
        diff,
        snapshots,
        verificationEvidence: verification.evidence ?? [],
        verificationEnabled: capabilities.verification?.enabled ?? verification.verificationEnabled,
        apiConnected: Boolean(health.ok),
        bootLoading: false,
        dataLoading: false,
        error: null,
        selectedFinding: null,
      }))
      setSelectedNodeId(graph?.nodes[0]?.id ?? null)

      // Kick off read-only cluster discovery so the dashboard reflects the live cluster
      // (in-cluster ServiceAccount or local kubeconfig) without requiring a manual click first.
      if (capabilities.kubernetesSettings?.configured !== false) {
        void (async () => {
          setData((prev) => ({ ...prev, discoveryLoading: true, discoveryError: null }))
          try {
            const allNamespaces = capabilities.kubernetesSettings?.allNamespaces !== false
            const clusterDiscovery = await api.discoverCluster('local', {
              allNamespaces,
              namespace: allNamespaces ? undefined : capabilities.kubernetesSettings?.namespace,
            })
            if (signal.aborted) return
            if (clusterDiscovery.offline || clusterDiscovery.status === 'not-configured') {
              setData((prev) => ({
                ...prev,
                clusterDiscovery,
                discoveryLoading: false,
                discoveryError: summarizeDiscoveryError(
                  clusterDiscovery,
                  'Kubernetes discovery is unavailable. In-cluster installs need a ServiceAccount with read RBAC; local installs need kubeconfig flags.',
                ),
              }))
              return
            }
            setData((prev) => ({
              ...prev,
              clusterDiscovery,
              discoveryLoading: false,
              discoveryError: null,
            }))
          } catch (discoverError) {
            if (signal.aborted) return
            setData((prev) => ({
              ...prev,
              discoveryLoading: false,
              discoveryError:
                discoverError instanceof Error ? discoverError.message : 'Cluster discovery failed',
            }))
          }
        })()
      }
    } catch (error) {
      if (signal.aborted) return
      const message = error instanceof Error ? error.message : 'Request failed'
      const detail = error instanceof api.ApiRequestError ? error.detail : undefined
      setData((prev) => ({
        ...prev,
        apiConnected: false,
        bootLoading: false,
        dataLoading: false,
        error: message,
        errorDetail: detail,
      }))
    }
  }, [])

  useEffect(() => {
    void loadWorkspace(workspacePath)
    return () => abortRef.current?.abort()
  }, [loadWorkspace, workspacePath])

  const findings = data.analysis?.findings
  const policies = data.analysis?.policies
  const namespaces = useMemo(
    () => Array.from(new Set((policies ?? []).map((policy) => policy.namespace).filter(Boolean))).sort(),
    [policies],
  )

  const highFindings = (findings ?? []).filter((finding) =>
    ['high', 'critical'].includes((finding.severity || '').toLowerCase()),
  ).length

  const filteredFindings = useMemo(() => {
    const query = searchText.trim().toLowerCase()
    return (findings ?? []).filter((finding) => {
      const severityOk =
        severityFilter === 'all' || (finding.severity || '').toLowerCase().includes(severityFilter)
      const searchOk =
        !query ||
        finding.id.toLowerCase().includes(query) ||
        finding.message.toLowerCase().includes(query) ||
        (finding.kind || '').toLowerCase().includes(query)
      return severityOk && searchOk
    })
  }, [findings, searchText, severityFilter])

  const filteredPolicies = useMemo(() => {
    const query = policyQuery.trim().toLowerCase()
    return (policies ?? []).filter((policy) => {
      const nsOk = namespaceFilter === 'all' || policy.namespace === namespaceFilter
      const searchOk =
        !query ||
        policy.name.toLowerCase().includes(query) ||
        policy.namespace.toLowerCase().includes(query) ||
        policy.selector.toLowerCase().includes(query) ||
        policy.policyType.toLowerCase().includes(query)
      return nsOk && searchOk
    })
  }, [policies, namespaceFilter, policyQuery])

  const topologyNodes = useMemo(() => {
    if (!data.graph?.nodes) return []
    const query = searchText.trim().toLowerCase()
    return data.graph.nodes.filter((node) => {
      const namespaceMatch = namespaceFilter === 'all' || node.id.includes(namespaceFilter)
      const searchMatch = !query || node.label.toLowerCase().includes(query) || node.id.toLowerCase().includes(query)
      const changedMatch =
        !changedOnly ||
        Boolean(
          data.diff?.modified.some((updated) => node.id.includes(updated.key.split('/').pop() ?? '')) ||
            data.diff?.added.some((item) => node.id.includes(item.split('/').pop() ?? '')) ||
            data.diff?.removed.some((item) => node.id.includes(item.split('/').pop() ?? '')),
        )
      return namespaceMatch && searchMatch && changedMatch
    })
  }, [data.graph, data.diff, searchText, namespaceFilter, changedOnly])

  const topologyEdges = useMemo(() => {
    if (!data.graph?.edges) return []
    const ids = new Set(topologyNodes.map((node) => node.id))
    return data.graph.edges.filter((edge) => ids.has(edge.source) || ids.has(edge.target))
  }, [data.graph, topologyNodes])

  const selectedNode =
    topologyNodes.find((node) => node.id === selectedNodeId) ??
    data.graph?.nodes.find((node) => node.id === selectedNodeId) ??
    null

  const discovery = data.capabilities?.kubernetes
  const kubeSettings = data.capabilities?.kubernetesSettings
  const strip = discoveryStripLabel(
    data.clusterDiscovery ??
      (discovery
        ? {
            mode: discovery.mode,
            status: discovery.status,
            offline: discovery.offline,
            complete: !discovery.offline,
            warnings: discovery.warnings,
            errors: discovery.errors,
            cluster: { context: discovery.context, host: discovery.cluster },
          }
        : null),
    kubeSettings ?? null,
  )
  const discoveryLabel = strip.label
  const discoveryTone = strip.tone

  const verificationLabel = data.verificationEnabled
    ? data.capabilities?.verification?.runtimeProbeImplemented
      ? 'provider active'
      : 'enabled (no production probe)'
    : 'disabled'
  const verificationTone: 'success' | 'warning' | 'muted' | 'danger' = data.verificationEnabled
    ? data.capabilities?.verification?.runtimeProbeImplemented
      ? 'success'
      : 'warning'
    : 'muted'

  async function runAnalyzeManifest() {
    setData((prev) => ({ ...prev, actionError: null, dataLoading: true }))
    try {
      const analysis = await api.createAnalysis({ manifest: manifestText, name: 'manual-proposed' })
      const graph = analysis.id ? await api.getGraph(analysis.id) : null
      setData((prev) => ({
        ...prev,
        analysis,
        graph,
        dataLoading: false,
        selectedFinding: null,
      }))
      setSelectedNodeId(graph?.nodes[0]?.id ?? null)
      setRoute('results')
    } catch (error) {
      setData((prev) => ({
        ...prev,
        dataLoading: false,
        actionError: error instanceof Error ? error.message : 'Analysis failed',
      }))
    }
  }

  async function runCreateSnapshot() {
    setData((prev) => ({ ...prev, actionError: null }))
    try {
      const snapshot = await api.createSnapshot({ path: workspacePath, name: `snapshot-${Date.now()}` })
      setData((prev) => ({ ...prev, snapshots: [snapshot, ...prev.snapshots] }))
      setRoute('snapshots')
    } catch (error) {
      setData((prev) => ({
        ...prev,
        actionError: error instanceof Error ? error.message : 'Could not create snapshot',
      }))
    }
  }

  async function runDiff() {
    setData((prev) => ({ ...prev, actionError: null, dataLoading: true }))
    try {
      const diff = await api.postDiff(analysisPath, proposedPath)
      setData((prev) => ({ ...prev, diff, dataLoading: false }))
    } catch (error) {
      setData((prev) => ({
        ...prev,
        dataLoading: false,
        actionError: error instanceof Error ? error.message : 'Comparison failed',
      }))
    }
  }

  async function runClusterDiscover() {
    setData((prev) => ({ ...prev, discoveryLoading: true, discoveryError: null }))
    try {
      const clusterDiscovery = await api.discoverCluster('local', { allNamespaces: true })
      if (clusterDiscovery.offline || clusterDiscovery.status === 'not-configured') {
        setData((prev) => ({
          ...prev,
          clusterDiscovery,
          discoveryLoading: false,
          discoveryError: summarizeDiscoveryError(
            clusterDiscovery,
            'Kubernetes is not configured for the API process. Start the server with kubeconfig flags or PCI_KUBECONFIG / PCI_KUBE_CONTEXT.',
          ),
        }))
        return
      }
      setData((prev) => ({
        ...prev,
        clusterDiscovery,
        discoveryLoading: false,
        discoveryError: null,
      }))
    } catch (error) {
      setData((prev) => ({
        ...prev,
        discoveryLoading: false,
        discoveryError: error instanceof Error ? error.message : 'Cluster discovery failed',
      }))
    }
  }

  async function runReport() {
    setData((prev) => ({ ...prev, actionError: null, dataLoading: true }))
    try {
      const report = data.analysis?.id
        ? await api.createReport({ analysisId: data.analysis.id })
        : await api.createReport({ path: workspacePath })
      setData((prev) => ({ ...prev, report, dataLoading: false }))
    } catch (error) {
      setData((prev) => ({
        ...prev,
        dataLoading: false,
        actionError: error instanceof Error ? error.message : 'Report generation failed',
      }))
    }
  }

  async function runExplain(findingId: string) {
    if (!data.analysis?.id) return
    setSelectedFindingId(findingId)
    try {
      const explanation = await api.explainFinding(data.analysis.id, findingId)
      setData((prev) => ({ ...prev, selectedFinding: explanation, actionError: null }))
    } catch (error) {
      setData((prev) => ({
        ...prev,
        actionError: error instanceof Error ? error.message : 'Could not load explanation',
      }))
    }
  }

  function fitGraph() {
    setZoom(1)
    setPan({ x: 0, y: 0 })
  }

  function onGraphPointerDown(event: ReactPointerEvent<HTMLDivElement>) {
    if (event.button !== 0) return
    setPanning(true)
    panOrigin.current = { x: event.clientX, y: event.clientY, panX: pan.x, panY: pan.y }
    event.currentTarget.setPointerCapture(event.pointerId)
  }

  function onGraphPointerMove(event: ReactPointerEvent<HTMLDivElement>) {
    if (!panning || !panOrigin.current) return
    const dx = event.clientX - panOrigin.current.x
    const dy = event.clientY - panOrigin.current.y
    setPan({ x: panOrigin.current.panX + dx, y: panOrigin.current.panY + dy })
  }

  function onGraphPointerUp(event: ReactPointerEvent<HTMLDivElement>) {
    setPanning(false)
    panOrigin.current = null
    event.currentTarget.releasePointerCapture(event.pointerId)
  }

  const activeNav = NAV_ITEMS.find((item) => item.key === route)

  if (data.bootLoading) {
    return (
      <div className="boot-screen">
        <div className="boot-card">
          <h1>PolicyChangeImpact</h1>
          <p>Connecting to the local API and loading workspace data…</p>
          <Skeleton rows={4} />
        </div>
      </div>
    )
  }

  if (!data.apiConnected && data.error && !data.health) {
    return (
      <div className="boot-screen">
        <div className="boot-card">
          <h1>API unavailable</h1>
          <p>{data.error}</p>
          {data.errorDetail && (
            <details>
              <summary>Technical details</summary>
              <pre className="diff-pre">{data.errorDetail}</pre>
            </details>
          )}
          <Button variant="primary" onClick={() => void loadWorkspace(workspacePath)}>
            Retry connection
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">PCI</div>
          <div className="brand-copy">
            <strong>PolicyChangeImpact</strong>
            <span>NetworkPolicy analysis</span>
          </div>
        </div>
        <nav className="nav" aria-label="Primary">
          {NAV_ITEMS.map((item) => (
            <button
              key={item.key}
              type="button"
              className={`nav-item ${route === item.key ? 'active' : ''}`}
              onClick={() => setRoute(item.key)}
              aria-current={route === item.key ? 'page' : undefined}
            >
              <Icon name={item.icon} />
              {item.label}
            </button>
          ))}
        </nav>
        <div className="sidebar-foot">
          Offline-first analysis. Modeled predictions are not runtime proof.
        </div>
      </aside>

      <div className="main-column">
        <div className="topbar">
          <ConnectionStrip
            apiOk={data.apiConnected}
            apiLabel={data.apiConnected ? `connected (${data.health?.version ?? 'dev'})` : 'disconnected'}
            discoveryLabel={discoveryLabel}
            discoveryTone={discoveryTone}
            verificationLabel={verificationLabel}
            verificationTone={verificationTone}
          />
          <Button variant="secondary" onClick={() => void loadWorkspace(workspacePath)} disabled={data.dataLoading}>
            <Icon name="refresh" />
            Refresh
          </Button>
        </div>

        <main className="main-panel">
          {data.error && (
            <ErrorBanner
              title="Workspace load issue"
              message={data.error}
              detail={data.errorDetail}
              onRetry={() => void loadWorkspace(workspacePath)}
            />
          )}
          {data.actionError && (
            <ErrorBanner title="Action failed" message={data.actionError} onRetry={() => setData((prev) => ({ ...prev, actionError: null }))} />
          )}

          <PageHeader
            breadcrumbs={['PCI', activeNav?.label ?? route]}
            title={activeNav?.label ?? route}
            description={activeNav?.description}
            actions={
              route === 'snapshots' ? (
                <Button variant="primary" onClick={() => void runCreateSnapshot()} disabled={!data.apiConnected}>
                  Create snapshot
                </Button>
              ) : route === 'comparison' ? (
                <Button variant="primary" onClick={() => void runDiff()} disabled={data.dataLoading}>
                  Run comparison
                </Button>
              ) : route === 'reports' ? (
                <Button variant="primary" onClick={() => void runReport()} disabled={data.dataLoading}>
                  Generate report
                </Button>
              ) : undefined
            }
          />

          {data.dataLoading && !data.analysis ? <Skeleton rows={6} /> : null}

          {route === 'dashboard' && (
            <>
              <section className="metrics-grid">
                <article className="metric-card">
                  <span>Policies analyzed</span>
                  <strong>{data.analysis?.summary.totalPolicies ?? 0}</strong>
                  <em>From current offline analysis</em>
                </article>
                <article className="metric-card tone-warning">
                  <span>Default deny</span>
                  <strong>{data.analysis?.summary.defaultDeny ?? 0}</strong>
                  <em>Policies with default-deny posture</em>
                </article>
                <article className="metric-card tone-danger">
                  <span>High-severity findings</span>
                  <strong>{highFindings}</strong>
                  <em>Modeled severity, not runtime proof</em>
                </article>
                <article className={`metric-card ${data.clusterDiscovery && !data.clusterDiscovery.offline ? 'tone-success' : ''}`}>
                  <span>Kubernetes discovery</span>
                  <strong style={{ fontSize: '1.05rem' }}>{discoveryLabel}</strong>
                  <em>
                    {data.clusterDiscovery?.cluster?.context ||
                      kubeSettings?.resolvedContext ||
                      discovery?.warnings?.[0] ||
                      'Read-only discovery; not a live inventory for flows'}
                  </em>
                </article>
              </section>

              <Panel
                title="Cluster discovery"
                meta={
                  <div className="page-actions">
                    <Badge tone="muted">read-only</Badge>
                    <Button variant="primary" onClick={() => void runClusterDiscover()} disabled={data.discoveryLoading || !data.apiConnected}>
                      {data.discoveryLoading ? 'Discovering…' : 'Discover / Refresh'}
                    </Button>
                  </div>
                }
              >
                <p className="callout">
                  Discovery uses the API process kubeconfig (flags or <span className="mono">PCI_KUBECONFIG</span> / <span className="mono">PCI_KUBE_CONTEXT</span>).
                  It never applies or deletes cluster resources. Modeled analysis remains separate from live inventory.
                </p>
                {data.discoveryError && (
                  <ErrorBanner title="Discovery unavailable" message={data.discoveryError} onRetry={() => void runClusterDiscover()} />
                )}
                {data.discoveryLoading && <Skeleton rows={4} />}
                {!data.discoveryLoading && data.clusterDiscovery && !data.clusterDiscovery.offline && (
                  <>
                    <div className="metrics-grid">
                      <article className="metric-card">
                        <span>Context</span>
                        <strong style={{ fontSize: '1rem' }} className="mono">
                          {data.clusterDiscovery.cluster?.context || kubeSettings?.resolvedContext || '—'}
                        </strong>
                      </article>
                      <article className="metric-card">
                        <span>API host</span>
                        <strong style={{ fontSize: '0.85rem' }} className="mono">
                          {data.clusterDiscovery.cluster?.host || '—'}
                        </strong>
                      </article>
                      <article className="metric-card">
                        <span>Namespaces</span>
                        <strong>{data.clusterDiscovery.namespaces?.length ?? 0}</strong>
                      </article>
                      <article className="metric-card">
                        <span>NetworkPolicies</span>
                        <strong>{data.clusterDiscovery.networkPolicies?.length ?? 0}</strong>
                      </article>
                    </div>
                    <div className="split-2">
                      <div className="detail-card">
                        <h4>Namespaces</h4>
                        {(data.clusterDiscovery.namespaces?.length ?? 0) === 0 ? (
                          <p className="meta-line">None returned</p>
                        ) : (
                          <ul className="list-stack">
                            {(data.clusterDiscovery.namespaces ?? []).slice(0, 20).map((namespace) => (
                              <li key={namespace} className="mono meta-line">{namespace}</li>
                            ))}
                          </ul>
                        )}
                        {(data.clusterDiscovery.namespaces?.length ?? 0) > 20 && (
                          <p className="meta-line">Showing first 20 of {data.clusterDiscovery.namespaces?.length}</p>
                        )}
                      </div>
                      <div className="detail-card">
                        <h4>NetworkPolicies</h4>
                        {(data.clusterDiscovery.networkPolicies?.length ?? 0) === 0 ? (
                          <p className="meta-line">None returned in this discovery scope</p>
                        ) : (
                          <ul className="list-stack">
                            {(data.clusterDiscovery.networkPolicies ?? []).slice(0, 20).map((policy) => (
                              <li key={`${policy.namespace}/${policy.name}`} className="list-item">
                                <strong className="mono">{policy.namespace}/{policy.name}</strong>
                                <p className="meta-line">
                                  {(policy.policyTypes ?? []).join(', ') || 'types n/a'} · ingress {policy.ingressRules} · egress {policy.egressRules}
                                </p>
                              </li>
                            ))}
                          </ul>
                        )}
                      </div>
                    </div>
                    <div className="detail-card" style={{ marginTop: 12 }}>
                      <h4>Workloads (sample)</h4>
                      <p className="meta-line">
                        {(data.clusterDiscovery.workloads ?? []).filter((item) => item.kind === 'Pod').length} pods ·{' '}
                        {data.clusterDiscovery.workloads?.length ?? 0} workload records · collected{' '}
                        {data.clusterDiscovery.collectedAt ? new Date(data.clusterDiscovery.collectedAt).toLocaleString() : '—'}
                        {data.clusterDiscovery.complete ? '' : ' · incomplete'}
                      </p>
                      <ul className="list-stack">
                        {(data.clusterDiscovery.workloads ?? []).slice(0, 12).map((workload) => (
                          <li key={`${workload.kind}/${workload.namespace}/${workload.name}`} className="mono meta-line">
                            {workload.kind} {workload.namespace}/{workload.name}
                            {workload.hostNetwork ? ' (hostNetwork)' : ''}
                          </li>
                        ))}
                      </ul>
                    </div>
                    {(data.clusterDiscovery.warnings?.length ?? 0) > 0 && (
                      <p className="callout">{data.clusterDiscovery.warnings?.join(' ')}</p>
                    )}
                  </>
                )}
                {!data.discoveryLoading && !data.clusterDiscovery && !data.discoveryError && (
                  <EmptyState
                    title="No discovery run yet"
                    body="Click Discover / Refresh to query the cluster configured for the API process."
                    action={
                      <Button variant="primary" onClick={() => void runClusterDiscover()} disabled={!data.apiConnected}>
                        Discover cluster
                      </Button>
                    }
                  />
                )}
              </Panel>

              <section className="content-grid">
                <Panel
                  title="Modeled topology preview"
                  meta={<Badge tone="info">modeled</Badge>}
                >
                  {data.graph && data.graph.nodes.length > 0 ? (
                    <svg viewBox="0 0 420 220" className="graph-canvas" style={{ height: 240 }} aria-label="Network graph preview">
                      {data.graph.edges.map((edge) => {
                        const source = data.graph?.nodes.find((node) => node.id === edge.source)
                        const target = data.graph?.nodes.find((node) => node.id === edge.target)
                        if (!source || !target) return null
                        return (
                          <line
                            key={edge.id}
                            x1={source.x ?? 80}
                            y1={source.y ?? 80}
                            x2={target.x ?? 280}
                            y2={target.y ?? 140}
                            className="edge"
                          />
                        )
                      })}
                      {data.graph.nodes.map((node, index) => (
                        <g key={node.id}>
                          <circle
                            cx={node.x ?? 70 + (index % 3) * 120}
                            cy={node.y ?? 70 + (index % 2) * 80}
                            r={node.type === 'cluster' ? 26 : 20}
                            className={`node ${node.type}`}
                          />
                          <text
                            className="graph-label"
                            x={(node.x ?? 70 + (index % 3) * 120) - 18}
                            y={(node.y ?? 70 + (index % 2) * 80) + 4}
                          >
                            {node.label}
                          </text>
                        </g>
                      ))}
                    </svg>
                  ) : (
                    <EmptyState
                      title="No graph yet"
                      body="Run an analysis from Policies or refresh with a valid manifest path."
                      action={
                        <Button variant="secondary" onClick={() => setRoute('policies')}>
                          Open policies
                        </Button>
                      }
                    />
                  )}
                  <div className="page-actions" style={{ marginTop: 12 }}>
                    <Button variant="secondary" onClick={() => setRoute('topology')}>
                      Open topology
                    </Button>
                    <Button variant="secondary" onClick={() => setRoute('comparison')}>
                      Compare manifests
                    </Button>
                    <Button variant="secondary" onClick={() => setRoute('findings')}>
                      Inspect findings
                    </Button>
                  </div>
                </Panel>

                <Panel title="Recent findings" meta={<Badge tone="muted">{findings?.length ?? 0} total</Badge>}>
                  {(findings?.length ?? 0) === 0 ? (
                    <EmptyState title="No findings" body="The current analysis did not produce structured findings." />
                  ) : (
                    <ul className="finding-list">
                      {(findings ?? []).slice(0, 6).map((finding) => (
                        <li key={finding.id} className="list-item">
                          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                            <strong className="mono">{finding.id}</strong>
                            <Badge tone={severityTone(finding.severity)} title={`Severity: ${finding.severity}`}>
                              {finding.severity || 'unknown'}
                            </Badge>
                          </div>
                          <p className="meta-line">{finding.message}</p>
                          <Button variant="ghost" onClick={() => { setRoute('findings'); void runExplain(finding.id) }}>
                            Explain
                          </Button>
                        </li>
                      ))}
                    </ul>
                  )}
                </Panel>
              </section>

              <Panel title="Activity" meta={<Badge tone="muted">from API</Badge>}>
                <div className="activity-list">
                  <div className="activity-item">
                    <time>{data.analysis?.generatedAt ? new Date(data.analysis.generatedAt).toLocaleString() : '—'}</time>
                    <div>
                      <strong>Analysis</strong>
                      <div className="meta-line">{data.analysis?.path || 'No analysis loaded'}</div>
                    </div>
                  </div>
                  <div className="activity-item">
                    <time>diff</time>
                    <div>
                      <strong>Comparison</strong>
                      <div className="meta-line">
                        {data.diff
                          ? `${data.diff.added.length} added · ${data.diff.removed.length} removed · ${data.diff.modified.length} modified`
                          : 'Not loaded yet'}
                      </div>
                    </div>
                  </div>
                  <div className="activity-item">
                    <time>verify</time>
                    <div>
                      <strong>Runtime evidence</strong>
                      <div className="meta-line">
                        {data.verificationEvidence.length} stored observation
                        {data.verificationEvidence.length === 1 ? '' : 's'} · verification {data.verificationEnabled ? 'enabled' : 'disabled'}
                      </div>
                    </div>
                  </div>
                </div>
              </Panel>
            </>
          )}

          {route === 'topology' && (
            <div className="stacked-grid">
              <Panel title="Topology" meta={<Badge tone="info">modeled graph</Badge>}>
                <div className="toolbar">
                  <input value={searchText} onChange={(event) => setSearchText(event.target.value)} placeholder="Search nodes" aria-label="Search nodes" />
                  <select value={namespaceFilter} onChange={(event) => setNamespaceFilter(event.target.value)} aria-label="Namespace filter">
                    <option value="all">All namespaces</option>
                    {namespaces.map((namespace) => (
                      <option key={namespace} value={namespace}>
                        {namespace}
                      </option>
                    ))}
                  </select>
                  <label className="checkbox-row">
                    <input type="checkbox" checked={changedOnly} onChange={(event) => setChangedOnly(event.target.checked)} />
                    Changed only
                  </label>
                </div>
                <div
                  className={`graph-canvas ${panning ? 'panning' : ''}`}
                  onPointerDown={onGraphPointerDown}
                  onPointerMove={onGraphPointerMove}
                  onPointerUp={onGraphPointerUp}
                  onPointerLeave={onGraphPointerUp}
                >
                  <div className="graph-controls">
                    <Button variant="ghost" onClick={() => setZoom((value) => Math.min(2.5, value + 0.15))} title="Zoom in">
                      +
                    </Button>
                    <Button variant="ghost" onClick={() => setZoom((value) => Math.max(0.5, value - 0.15))} title="Zoom out">
                      −
                    </Button>
                    <Button variant="ghost" onClick={fitGraph} title="Fit to view">
                      Fit
                    </Button>
                  </div>
                  <svg viewBox="0 0 480 320" aria-label="Topology graph">
                    <g transform={`translate(${pan.x} ${pan.y}) scale(${zoom})`}>
                      {topologyEdges.map((edge) => {
                        const source = data.graph?.nodes.find((node) => node.id === edge.source)
                        const target = data.graph?.nodes.find((node) => node.id === edge.target)
                        if (!source || !target) return null
                        return (
                          <line
                            key={edge.id}
                            x1={source.x ?? 80}
                            y1={source.y ?? 80}
                            x2={target.x ?? 280}
                            y2={target.y ?? 160}
                            className="edge"
                          />
                        )
                      })}
                      {topologyNodes.map((node, index) => {
                        const cx = node.x ?? 80 + (index % 4) * 110
                        const cy = node.y ?? 70 + Math.floor(index / 4) * 90
                        return (
                          <g key={node.id} onClick={() => setSelectedNodeId(node.id)} style={{ cursor: 'pointer' }}>
                            <circle
                              cx={cx}
                              cy={cy}
                              r={node.type === 'cluster' ? 26 : 20}
                              className={`node ${node.type} ${selectedNodeId === node.id ? 'selected' : ''}`}
                            />
                            <text className="graph-label" x={cx - 16} y={cy + 4}>
                              {node.label}
                            </text>
                          </g>
                        )
                      })}
                    </g>
                  </svg>
                </div>
                <div className="legend" aria-label="Legend">
                  <span><i className="cluster" />Cluster</span>
                  <span><i className="policy" />Policy</span>
                  <span><i className="workload" />Workload</span>
                  <span><i className="namespace" />Namespace</span>
                </div>
                <div className="list-stack" style={{ marginTop: 12 }}>
                  {topologyNodes.length === 0 ? (
                    <EmptyState title="No matching nodes" body="Adjust filters or run an analysis that produces a graph." />
                  ) : (
                    topologyNodes.map((node) => (
                      <button
                        key={node.id}
                        type="button"
                        className={`graph-node-item ${selectedNodeId === node.id ? 'selected' : ''}`}
                        onClick={() => setSelectedNodeId(node.id)}
                      >
                        <span>{node.label}</span>
                        <small className="muted">{node.type}</small>
                      </button>
                    ))
                  )}
                </div>
              </Panel>
              <Panel title="Selection" meta={<Badge tone="muted">details</Badge>}>
                {selectedNode ? (
                  <div className="detail-card">
                    <h4>{selectedNode.label}</h4>
                    <p><strong>ID:</strong> <span className="mono">{selectedNode.id}</span></p>
                    <p><strong>Type:</strong> {selectedNode.type}</p>
                    <p>
                      <strong>Related policies:</strong>{' '}
                      {(policies ?? [])
                        .filter((policy) => policy.name.includes(selectedNode.label) || selectedNode.id.includes(policy.name))
                        .map((policy) => `${policy.namespace}/${policy.name}`)
                        .join(', ') || 'No direct policy matches'}
                    </p>
                    <p className="meta-line">
                      This graph is modeled from offline analysis. It does not represent live cluster traffic.
                    </p>
                  </div>
                ) : (
                  <EmptyState title="Select a node" body="Choose a node in the graph or list to inspect relationships." />
                )}
              </Panel>
            </div>
          )}

          {route === 'policies' && (
            <div className="split-2">
              <Panel title="Analyze manifest" meta={<Badge tone="muted">POST /analyses</Badge>}>
                <p className="callout">
                  Paste a NetworkPolicy YAML/JSON document. Analysis is offline and modeled — incomplete inputs yield unknown outcomes, not assumed allow/deny.
                </p>
                <textarea className="manifest-editor" value={manifestText} onChange={(event) => setManifestText(event.target.value)} aria-label="Policy manifest" />
                <Button variant="primary" onClick={() => void runAnalyzeManifest()} disabled={data.dataLoading}>
                  Analyze proposed manifest
                </Button>
              </Panel>
              <Panel title="Policy inventory" meta={<Badge tone="muted">{filteredPolicies.length}</Badge>}>
                <div className="toolbar">
                  <input value={policyQuery} onChange={(event) => setPolicyQuery(event.target.value)} placeholder="Search policies" aria-label="Search policies" />
                  <select value={namespaceFilter} onChange={(event) => setNamespaceFilter(event.target.value)} aria-label="Filter namespace">
                    <option value="all">All namespaces</option>
                    {namespaces.map((namespace) => (
                      <option key={namespace} value={namespace}>{namespace}</option>
                    ))}
                  </select>
                </div>
                {filteredPolicies.length === 0 ? (
                  <EmptyState title="No policies" body="Load an analysis or analyze a manifest to populate the inventory." />
                ) : (
                  <div className="policy-list">
                    {filteredPolicies.map((policy) => {
                      const key = `${policy.namespace}/${policy.name}`
                      const open = expandedPolicy === key
                      return (
                        <article key={key} className="policy-card">
                          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                            <h4>{key}</h4>
                            <div style={{ display: 'flex', gap: 6 }}>
                              {policy.defaultDeny && <Badge tone="warning">default deny</Badge>}
                              <Badge tone="info">{policy.policyType || 'types unknown'}</Badge>
                            </div>
                          </div>
                          <p className="meta-line">Selector: {policy.selector || '(empty — broad match)'}</p>
                          <p className="meta-line">
                            Ingress {policy.ingressRules} · Egress {policy.egressRules}
                          </p>
                          <Button variant="ghost" onClick={() => setExpandedPolicy(open ? null : key)}>
                            {open ? 'Hide details' : 'Show details'}
                          </Button>
                          {open && (
                            <div className="detail-card" style={{ marginTop: 10 }}>
                              <p><strong>Match labels:</strong> {(policy.matchLabels ?? []).join(', ') || 'None listed'}</p>
                              <p><strong>Related peers:</strong> {(policy.relatedPeers ?? []).join(', ') || 'None listed'}</p>
                            </div>
                          )}
                        </article>
                      )
                    })}
                  </div>
                )}
              </Panel>
            </div>
          )}

          {route === 'results' && (
            <Panel title="Analysis results" meta={<Badge tone="info">modeled</Badge>}>
              {!data.analysis ? (
                <EmptyState
                  title="No analysis loaded"
                  body="Refresh the workspace or analyze a manifest from Policies."
                  action={<Button variant="secondary" onClick={() => setRoute('policies')}>Go to policies</Button>}
                />
              ) : (
                <>
                  <div className="metrics-grid">
                    <article className="metric-card"><span>Path</span><strong style={{ fontSize: '1rem' }} className="mono">{data.analysis.path || 'manifest'}</strong></article>
                    <article className="metric-card"><span>Generated</span><strong style={{ fontSize: '1rem' }}>{new Date(data.analysis.generatedAt).toLocaleString()}</strong></article>
                    <article className="metric-card"><span>Policies</span><strong>{data.analysis.summary.totalPolicies}</strong></article>
                    <article className="metric-card"><span>Findings</span><strong>{data.analysis.summary.findingCount}</strong></article>
                  </div>
                  <div className="page-actions">
                    <Button variant="primary" onClick={() => void runCreateSnapshot()}>Save snapshot</Button>
                    <Button variant="secondary" onClick={() => setRoute('findings')}>View findings</Button>
                    <Button variant="secondary" onClick={() => setRoute('reports')}>Generate report</Button>
                  </div>
                  <p className="callout" style={{ marginTop: 16 }}>
                    <strong>Layer:</strong> These results are modeled from manifests. They are not live cluster observations and do not prove NetworkPolicy causation for runtime failures.
                  </p>
                </>
              )}
            </Panel>
          )}

          {route === 'findings' && (
            <div className="stacked-grid">
              <Panel title="Findings" meta={<Badge tone="muted">{filteredFindings.length}</Badge>}>
                <div className="toolbar">
                  <input value={searchText} onChange={(event) => setSearchText(event.target.value)} placeholder="Search findings" aria-label="Search findings" />
                  <select value={severityFilter} onChange={(event) => setSeverityFilter(event.target.value)} aria-label="Severity filter">
                    <option value="all">All severities</option>
                    <option value="critical">Critical</option>
                    <option value="high">High</option>
                    <option value="medium">Medium</option>
                    <option value="low">Low</option>
                  </select>
                </div>
                {filteredFindings.length === 0 ? (
                  <EmptyState title="No findings match" body="Try clearing filters or analyze a different manifest set." />
                ) : (
                  <div className="table-wrap">
                    <table className="data-table">
                      <thead>
                        <tr>
                          <th>ID</th>
                          <th>Severity</th>
                          <th>Kind</th>
                          <th>Message</th>
                        </tr>
                      </thead>
                      <tbody>
                        {filteredFindings.map((finding) => (
                          <tr
                            key={finding.id}
                            className={selectedFindingId === finding.id ? 'selected' : ''}
                            onClick={() => void runExplain(finding.id)}
                          >
                            <td className="mono">{finding.id}</td>
                            <td><Badge tone={severityTone(finding.severity)}>{finding.severity || 'n/a'}</Badge></td>
                            <td>{finding.kind || '—'}</td>
                            <td>{finding.message}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </Panel>
              <Panel title="Explanation" meta={<Badge tone="info">modeled</Badge>}>
                {data.selectedFinding ? (
                  <div className="detail-card">
                    <h4 className="mono">{data.selectedFinding.findingId}</h4>
                    <p><strong>Traffic status:</strong> {data.selectedFinding.trafficStatus}</p>
                    <p><strong>Summary:</strong> {data.selectedFinding.summary}</p>
                    <p><strong>Reason:</strong> {data.selectedFinding.reason}</p>
                    <p><strong>Applicable policies:</strong> {(data.selectedFinding.applicablePolicies ?? []).join(', ') || 'Not available'}</p>
                    <p><strong>Assumptions:</strong> {(data.selectedFinding.assumptions ?? []).join('; ') || 'None documented'}</p>
                    <p><strong>Unsupported:</strong> {(data.selectedFinding.unsupported ?? []).join('; ') || 'None listed'}</p>
                    <p className="meta-line">Explanations describe the static model. They are not verified runtime outcomes.</p>
                  </div>
                ) : (
                  <EmptyState title="Select a finding" body="Choose a row to load a structured explanation from the API." />
                )}
              </Panel>
            </div>
          )}

          {route === 'comparison' && (
            <Panel title="Base vs proposed" meta={<Badge tone="warning">config delta ≠ causation</Badge>}>
              <div className="toolbar">
                <label className="field">
                  Base path
                  <input value={analysisPath} onChange={(event) => setAnalysisPath(event.target.value)} />
                </label>
                <label className="field">
                  Proposed path
                  <input value={proposedPath} onChange={(event) => setProposedPath(event.target.value)} />
                </label>
              </div>
              <p className="callout">
                Semantic diffs show how policy configuration changed. A modified rule does not by itself prove a runtime connectivity failure.
              </p>
              {!data.diff ? (
                <EmptyState
                  title="No comparison loaded"
                  body="Run a comparison against the configured base and proposed paths."
                  action={<Button variant="primary" onClick={() => void runDiff()}>Run comparison</Button>}
                />
              ) : (
                <div className="split-2">
                  <div className="detail-card">
                    <h4>Added ({data.diff.added.length})</h4>
                    <p>{data.diff.added.length ? data.diff.added.join(', ') : 'None'}</p>
                    <h4>Removed ({data.diff.removed.length})</h4>
                    <p>{data.diff.removed.length ? data.diff.removed.join(', ') : 'None'}</p>
                  </div>
                  <div className="detail-card">
                    <h4>Modified ({data.diff.modified.length})</h4>
                    {data.diff.modified.length === 0 ? (
                      <p>None</p>
                    ) : (
                      <ul className="list-stack">
                        {data.diff.modified.map((entry) => (
                          <li key={entry.key} className="list-item">
                            <strong>{entry.key}</strong>
                            <p className="meta-line">{entry.reason}</p>
                            <p className="meta-line">Fields: {entry.changedFields.join(', ') || 'Not reported'}</p>
                            <details>
                              <summary>Exact field delta</summary>
                              {entry.fieldChanges.map((change) => (
                                <div key={change.field}>
                                  <strong className="mono">{change.field}</strong>
                                  <pre className="diff-pre">Before: {JSON.stringify(change.before, null, 2)}</pre>
                                  <pre className="diff-pre">After: {JSON.stringify(change.after, null, 2)}</pre>
                                </div>
                              ))}
                            </details>
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                </div>
              )}
            </Panel>
          )}

          {route === 'snapshots' && (
            <Panel title="Snapshots" meta={<Badge tone="muted">{data.snapshots.length}</Badge>}>
              {data.snapshots.length === 0 ? (
                <EmptyState
                  title="No snapshots stored"
                  body="Create a snapshot from the current analysis path to retain an offline baseline."
                  action={<Button variant="primary" onClick={() => void runCreateSnapshot()}>Create snapshot</Button>}
                />
              ) : (
                <ul className="list-stack">
                  {data.snapshots.map((snapshot) => (
                    <li key={snapshot.id} className="snapshot-row">
                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                        <strong>{snapshot.name || snapshot.id}</strong>
                        <Badge tone="muted">{snapshot.summary?.findingCount ?? 0} findings</Badge>
                      </div>
                      <p className="mono meta-line">{snapshot.id}</p>
                      <p className="meta-line">{snapshot.generatedAt || snapshot.path}</p>
                      <p className="meta-line">
                        Policies {snapshot.summary?.totalPolicies ?? '—'} · Default deny {snapshot.summary?.defaultDeny ?? '—'}
                      </p>
                    </li>
                  ))}
                </ul>
              )}
            </Panel>
          )}

          {route === 'verification' && (
            <Panel
              title="Runtime verification evidence"
              meta={<Badge tone={data.verificationEnabled ? 'warning' : 'muted'}>{data.verificationEnabled ? 'provider present' : 'disabled'}</Badge>}
            >
              <p className="callout">
                <strong>Layers stay separate.</strong> Modeled predictions, runtime observations, and NetworkPolicy causation are not the same.
                A failed connection does not prove NetworkPolicy caused the failure. This UI never fabricates probe results.
              </p>
              {!data.verificationEnabled && (
                <EmptyState
                  title="Runtime verification disabled"
                  body="No production probe provider is installed. The API accepts evidence contracts for tests only; normal operation returns VERIFICATION_DISABLED."
                />
              )}
              {data.verificationEvidence.length === 0 ? (
                <EmptyState
                  title="No observation evidence"
                  body="When a provider records evidence, it will appear here with provenance, timestamps, and limitations."
                />
              ) : (
                <ul className="list-stack">
                  {data.verificationEvidence.map((evidence) => (
                    <li key={evidence.id} className="evidence-card">
                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
                        <strong>{evidence.state.replace(/_/g, ' ')} · {evidence.provenance}</strong>
                        <Badge tone="info">{evidence.modelComparison}</Badge>
                      </div>
                      <p className="mono meta-line">
                        {evidence.sourceNamespace}/{evidence.sourceWorkload} → {evidence.destination}:{evidence.port}/{evidence.protocol}
                      </p>
                      <p className="meta-line">
                        {evidence.method} · {new Date(evidence.attemptedAt).toLocaleString()} · {evidence.outcome} · {api.formatDurationMs(evidence.duration)}
                        {evidence.timeoutMillis === undefined ? '' : ` · timeout ${evidence.timeoutMillis} ms`}
                      </p>
                      {(evidence.cluster || evidence.context || evidence.cni) && (
                        <p className="meta-line">Environment: {[evidence.cluster, evidence.context, evidence.cni].filter(Boolean).join(' / ')}</p>
                      )}
                      {evidence.discoveryCollectedAt && (
                        <p className="meta-line">
                          Discovery: {new Date(evidence.discoveryCollectedAt).toLocaleString()} ·{' '}
                          {evidence.discoveryComplete === undefined ? 'completeness unknown' : evidence.discoveryComplete ? 'complete' : 'incomplete'}
                        </p>
                      )}
                      <p className="meta-line">{evidence.limitations?.join(' ')}</p>
                      {evidence.errorClass && <p className="meta-line">Error class: {evidence.errorClass}</p>}
                    </li>
                  ))}
                </ul>
              )}
            </Panel>
          )}

          {route === 'reports' && (
            <Panel title="Report" meta={<Badge tone="muted">review artifact</Badge>}>
              {!data.report ? (
                <EmptyState
                  title="No report generated"
                  body="Generate a report from the current analysis or configured path. Reports are offline review artifacts."
                  action={<Button variant="primary" onClick={() => void runReport()}>Generate report</Button>}
                />
              ) : (
                <>
                  <div className="metrics-grid">
                    <article className="metric-card"><span>Policies</span><strong>{data.report.policyCount ?? data.report.summary?.totalPolicies ?? '—'}</strong></article>
                    <article className="metric-card"><span>Findings</span><strong>{data.report.findingCount ?? data.report.summary?.findingCount ?? '—'}</strong></article>
                    <article className="metric-card"><span>Generated</span><strong style={{ fontSize: '1rem' }}>{data.report.generatedAt ? new Date(data.report.generatedAt).toLocaleString() : '—'}</strong></article>
                    <article className="metric-card"><span>Analysis</span><strong style={{ fontSize: '0.9rem' }} className="mono">{data.report.analysisId || '—'}</strong></article>
                  </div>
                  {data.report.recommendation && <p className="callout">{data.report.recommendation}</p>}
                  <h3 style={{ fontSize: '0.95rem' }}>Raw report JSON</h3>
                  <pre className="json-box">{JSON.stringify(data.report, null, 2)}</pre>
                </>
              )}
            </Panel>
          )}

          {route === 'settings' && (
            <Panel title="Settings" meta={<Badge tone="muted">local</Badge>}>
              <div className="settings-grid">
                <label>
                  API base URL
                  <input value={api.API_BASE} readOnly title="Configured via Vite proxy or VITE_API_BASE" />
                </label>
                <label>
                  Server version
                  <input value={data.health?.version ?? 'unknown'} readOnly />
                </label>
                <label className="span-2">
                  Workspace analysis path
                  <div style={{ display: 'flex', gap: 8 }}>
                    <input
                      value={workspacePathDraft}
                      onChange={(event) => setWorkspacePathDraft(event.target.value)}
                      style={{ flex: 1 }}
                    />
                    <Button
                      variant="primary"
                      onClick={() => {
                        setAnalysisPath(workspacePathDraft)
                        setWorkspacePath(workspacePathDraft)
                      }}
                      disabled={!workspacePathDraft.trim() || workspacePathDraft === workspacePath}
                    >
                      Apply & reload
                    </Button>
                  </div>
                </label>
                <label>
                  SQLite persistence
                  <input value={data.health?.database?.sqlite ? 'available' : 'unavailable'} readOnly />
                </label>
                <label>
                  Connection mode
                  <input
                    value={
                      kubeSettings?.resolvedMode ||
                      kubeSettings?.mode ||
                      (kubeSettings?.inClusterEnv ? 'in-cluster (env)' : 'auto')
                    }
                    readOnly
                  />
                </label>
                <label>
                  Config source
                  <input value={kubeSettings?.source || kubeSettings?.resolvedKubeconfigPath || '(unresolved)'} readOnly />
                </label>
                <label>
                  Kubeconfig (configured)
                  <input
                    value={
                      kubeSettings?.resolvedMode === 'in-cluster'
                        ? '(in-cluster service account)'
                        : kubeSettings?.kubeconfigPath || kubeSettings?.resolvedKubeconfigPath || '(default rules)'
                    }
                    readOnly
                  />
                </label>
                <label>
                  Context (resolved)
                  <input value={kubeSettings?.resolvedContext || kubeSettings?.context || data.clusterDiscovery?.cluster?.context || '(current-context)'} readOnly />
                </label>
                <label>
                  Last discovery
                  <input
                    value={
                      data.clusterDiscovery?.collectedAt
                        ? new Date(data.clusterDiscovery.collectedAt).toLocaleString()
                        : 'not yet run'
                    }
                    readOnly
                  />
                </label>
                <label className="span-2">
                  Kubernetes discovery status
                  <input
                    value={
                      data.clusterDiscovery
                        ? `${data.clusterDiscovery.mode} / ${data.clusterDiscovery.status}${data.clusterDiscovery.offline ? ' (offline)' : ''}${data.clusterDiscovery.complete ? '' : ' · incomplete'}`
                        : discovery
                          ? `${discovery.mode} / ${discovery.status}${discovery.offline ? ' (offline)' : ''}`
                          : 'not reported'
                    }
                    readOnly
                  />
                </label>
                <label className="span-2">
                  Capabilities
                  <textarea
                    readOnly
                    rows={3}
                    value={(data.capabilities?.capabilities ?? []).join(', ') || 'none reported'}
                  />
                </label>
              </div>
              <div className="page-actions" style={{ marginTop: 16 }}>
                <Button variant="primary" onClick={() => void runClusterDiscover()} disabled={data.discoveryLoading || !data.apiConnected}>
                  {data.discoveryLoading ? 'Discovering…' : 'Discover / Refresh cluster'}
                </Button>
              </div>
              <p className="callout" style={{ marginTop: 16 }}>
                Changing the analysis path reloads offline workspace data. Kubernetes connectivity is configured on the API process, not in the browser.
                Helm/in-cluster installs use the Pod ServiceAccount (<span className="mono">PCI_KUBE_MODE=in-cluster</span>).
                Local development: <span className="mono">pci serve --kubeconfig ~/.kube/config --kube-context kind-kind --all-namespaces</span>.
                Discovery is read-only; controlled apply and runtime probes are not enabled by default.
              </p>
              {(discovery?.warnings?.length ?? 0) > 0 && (
                <div className="detail-card">
                  <h4>Discovery warnings</h4>
                  <ul>
                    {discovery?.warnings?.map((warning) => (
                      <li key={warning}>{warning}</li>
                    ))}
                  </ul>
                </div>
              )}
              {kubeSettings?.error && (
                <div className="detail-card">
                  <h4>Kubeconfig load error</h4>
                  <p className="meta-line">{kubeSettings.error}</p>
                </div>
              )}
            </Panel>
          )}
        </main>
      </div>
    </div>
  )
}

export default App
