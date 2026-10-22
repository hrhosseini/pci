import { useCallback, useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import './App.css'
import * as api from './api'
import {
  ActionCard,
  Badge,
  Button,
  Callout,
  ConnectionStrip,
  EmptyState,
  ErrorBanner,
  Icon,
  PageHeader,
  Panel,
  Skeleton,
  StatTile,
} from './components'
import { NAV_ITEMS, severityLabel, severityTone } from './nav'
import { discoveryStripLabel, summarizeDiscoveryError } from './discoveryStatus'
import { buildDiscoveryGraph, type DiscoveryGraphNode } from './discoveryGraph'
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
} from './types'

type AppData = {
  health: HealthResponse | null
  capabilities: CapabilitiesResponse | null
  analysis: Analysis | null
  graph: Graph | null
  diff: DiffReport | null
  snapshots: Snapshot[]
  report: Report | null
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

function App() {
  const [route, setRoute] = useState<Route>('overview')
  const [data, setData] = useState<AppData>(initialData)
  const [workspacePath] = useState('fixtures/base')
  const [analysisPath, setAnalysisPath] = useState('fixtures/base')
  const [proposedPath, setProposedPath] = useState('fixtures/proposed')
  const [manifestText, setManifestText] = useState('')
  const [baseManifest, setBaseManifest] = useState('')
  const [proposedManifest, setProposedManifest] = useState('')
  const [compareMode, setCompareMode] = useState<'paths' | 'manifests'>('paths')
  const [namespaceFilter, setNamespaceFilter] = useState('all')
  const [searchText, setSearchText] = useState('')
  const [severityFilter, setSeverityFilter] = useState('all')
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)
  const [selectedFindingId, setSelectedFindingId] = useState<string | null>(null)
  const [policyQuery, setPolicyQuery] = useState('')
  const [expandedPolicy, setExpandedPolicy] = useState<string | null>(null)
  const [expandedFinding, setExpandedFinding] = useState<string | null>(null)
  const [zoom, setZoom] = useState(1)
  const [pan, setPan] = useState({ x: 0, y: 0 })
  const [panning, setPanning] = useState(false)
  const [graphSource, setGraphSource] = useState<'discovery' | 'analysis'>('discovery')
  const [reportsTab, setReportsTab] = useState<'findings' | 'snapshots' | 'report'>('findings')
  const panOrigin = useRef<{ x: number; y: number; panX: number; panY: number } | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const sampleBase = data.capabilities?.samplePaths?.base || workspacePath
  const sampleProposed = data.capabilities?.samplePaths?.proposed || proposedPath
  const sampleAvailable = Boolean(data.capabilities?.samplePaths?.baseAvailable)

  const loadWorkspace = useCallback(async (path: string) => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    const { signal } = controller

    try {
      // Fetch before any setState so mount useEffect does not update state synchronously.
      const health = await api.getHealth(signal)
      const capabilities = await api.getCapabilities(signal)
      if (signal.aborted) return

      const sampleBasePath = capabilities.samplePaths?.base || path
      const sampleProposedPath = capabilities.samplePaths?.proposed || 'fixtures/proposed'

      setData((prev) => ({
        ...prev,
        health,
        capabilities,
        apiConnected: Boolean(health.ok),
        bootLoading: false,
        dataLoading: true,
        error: null,
        errorDetail: undefined,
        actionError: null,
      }))

      if (
        capabilities.samplePaths?.baseAvailable &&
        capabilities.samplePaths.base &&
        (path === 'fixtures/base' || path.startsWith('fixtures/'))
      ) {
        setAnalysisPath(capabilities.samplePaths.base)
        if (capabilities.samplePaths.proposed) {
          setProposedPath(capabilities.samplePaths.proposed)
        }
      }

      const analysesResponse = await api.listAnalyses(signal)
      let analysis: Analysis | null = null
      let analysisLoadError: string | null = null
      if (analysesResponse.analyses?.[0]?.id) {
        analysis = await api.getAnalysis(analysesResponse.analyses[0].id, signal)
      }
      if (!analysis && capabilities.samplePaths?.baseAvailable !== false) {
        try {
          analysis = await api.createAnalysis({ path: sampleBasePath }, signal)
        } catch (analysisError) {
          analysisLoadError =
            analysisError instanceof Error
              ? analysisError.message
              : 'Offline sample analysis is unavailable'
        }
      }

      const [graph, snapshotsResponse, diff] = await Promise.all([
        analysis?.id ? api.getGraph(analysis.id, signal) : Promise.resolve(null),
        api.listSnapshots(signal),
        api
          .postDiff({ basePath: sampleBasePath, proposedPath: sampleProposedPath }, signal)
          .catch(() => null),
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
        apiConnected: Boolean(health.ok),
        bootLoading: false,
        dataLoading: false,
        error: null,
        actionError: analysisLoadError,
        selectedFinding: null,
      }))
      setSelectedNodeId(graph?.nodes[0]?.id ?? null)

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

  // Initial API bootstrap. Subsequent loads go through Refresh / Discover / Analyze actions.
  useEffect(() => {
    // oxlint-disable-next-line react/set-state-in-effect -- async workspace bootstrap on mount
    void loadWorkspace(workspacePath)
    return () => abortRef.current?.abort()
  }, [loadWorkspace, workspacePath])

  const findings = data.analysis?.findings
  const policies = data.analysis?.policies
  const liveDiscovery = Boolean(data.clusterDiscovery && !data.clusterDiscovery.offline)

  const namespaces = useMemo(() => {
    const fromAnalysis = (policies ?? []).map((policy) => policy.namespace)
    const fromDiscovery = [
      ...(data.clusterDiscovery?.namespaces ?? []),
      ...(data.clusterDiscovery?.networkPolicies ?? []).map((policy) => policy.namespace),
    ]
    return Array.from(new Set([...fromAnalysis, ...fromDiscovery].filter(Boolean))).sort()
  }, [policies, data.clusterDiscovery])

  const liveDiscoveryGraph = useMemo(
    () => buildDiscoveryGraph(data.clusterDiscovery, { namespaceFilter }),
    [data.clusterDiscovery, namespaceFilter],
  )

  const hasLiveDiscoveryGraph =
    liveDiscovery && liveDiscoveryGraph.nodes.length > 0

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

  const discoveryPolicies = useMemo(() => {
    const list = data.clusterDiscovery?.networkPolicies ?? []
    if (namespaceFilter === 'all') return list
    return list.filter((policy) => policy.namespace === namespaceFilter)
  }, [data.clusterDiscovery, namespaceFilter])

  const useDiscoveryTopology = graphSource === 'discovery' && hasLiveDiscoveryGraph

  const topologyNodes = useMemo(() => {
    const query = searchText.trim().toLowerCase()
    if (useDiscoveryTopology) {
      return liveDiscoveryGraph.nodes.filter((node) => {
        return !query || node.label.toLowerCase().includes(query) || node.id.toLowerCase().includes(query)
      })
    }
    if (!data.graph?.nodes) return []
    return data.graph.nodes.filter((node) => {
      const namespaceMatch = namespaceFilter === 'all' || node.id.includes(namespaceFilter)
      const searchMatch = !query || node.label.toLowerCase().includes(query) || node.id.toLowerCase().includes(query)
      return namespaceMatch && searchMatch
    })
  }, [useDiscoveryTopology, liveDiscoveryGraph.nodes, data.graph, searchText, namespaceFilter])

  const topologyEdges = useMemo(() => {
    const ids = new Set(topologyNodes.map((node) => node.id))
    if (useDiscoveryTopology) {
      return liveDiscoveryGraph.edges.filter((edge) => ids.has(edge.source) && ids.has(edge.target))
    }
    if (!data.graph?.edges) return []
    return data.graph.edges.filter((edge) => ids.has(edge.source) || ids.has(edge.target))
  }, [useDiscoveryTopology, liveDiscoveryGraph.edges, data.graph, topologyNodes])

  const selectedNode =
    (topologyNodes.find((node) => node.id === selectedNodeId) as DiscoveryGraphNode | undefined) ??
    (data.graph?.nodes.find((node) => node.id === selectedNodeId) as DiscoveryGraphNode | undefined) ??
    null

  const nodeById = useMemo(() => {
    const map = new Map<string, DiscoveryGraphNode>()
    for (const node of topologyNodes) map.set(node.id, node as DiscoveryGraphNode)
    return map
  }, [topologyNodes])

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

  const podCount = liveDiscovery
    ? (data.clusterDiscovery?.workloads ?? []).filter((item) => item.kind === 'Pod').length
    : null
  const workloadCount = liveDiscovery ? (data.clusterDiscovery?.workloads?.length ?? 0) : null
  const namespaceCount = liveDiscovery ? (data.clusterDiscovery?.namespaces?.length ?? 0) : null
  const networkPolicyCount = liveDiscovery
    ? (data.clusterDiscovery?.networkPolicies?.length ?? 0)
    : null
  const lastDiscoveryAt = data.clusterDiscovery?.collectedAt
    ? new Date(data.clusterDiscovery.collectedAt).toLocaleString()
    : null

  async function runAnalyzeManifest() {
    if (!manifestText.trim()) {
      setData((prev) => ({ ...prev, actionError: 'Paste a NetworkPolicy YAML/JSON document first.' }))
      return
    }
    setData((prev) => ({ ...prev, actionError: null, dataLoading: true }))
    try {
      const analysis = await api.createAnalysis({ manifest: manifestText, name: 'manual-analysis' })
      const graph = analysis.id ? await api.getGraph(analysis.id) : null
      setData((prev) => ({
        ...prev,
        analysis,
        graph,
        dataLoading: false,
        selectedFinding: null,
      }))
      setSelectedNodeId(graph?.nodes[0]?.id ?? null)
      setRoute('policies')
    } catch (error) {
      setData((prev) => ({
        ...prev,
        dataLoading: false,
        actionError: error instanceof Error ? error.message : 'Analysis failed',
      }))
    }
  }

  async function runAnalyzeSample() {
    setData((prev) => ({ ...prev, actionError: null, dataLoading: true }))
    try {
      const analysis = await api.createAnalysis({ path: sampleBase, name: 'sample-fixture' })
      const graph = analysis.id ? await api.getGraph(analysis.id) : null
      setData((prev) => ({
        ...prev,
        analysis,
        graph,
        dataLoading: false,
        selectedFinding: null,
      }))
      setSelectedNodeId(graph?.nodes[0]?.id ?? null)
    } catch (error) {
      setData((prev) => ({
        ...prev,
        dataLoading: false,
        actionError: error instanceof Error ? error.message : 'Could not load sample fixture',
      }))
    }
  }

  async function runCreateSnapshot() {
    setData((prev) => ({ ...prev, actionError: null }))
    try {
      const snapshot = await api.createSnapshot({
        path: data.analysis?.path || workspacePath,
        name: `snapshot-${Date.now()}`,
      })
      setData((prev) => ({ ...prev, snapshots: [snapshot, ...prev.snapshots] }))
      setReportsTab('snapshots')
      setRoute('reports')
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
      const diff =
        compareMode === 'manifests'
          ? await api.postDiff({
              baseManifest: baseManifest.trim(),
              proposedManifest: proposedManifest.trim(),
            })
          : await api.postDiff({ basePath: analysisPath, proposedPath })
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
      setGraphSource('discovery')
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
      setReportsTab('report')
      setRoute('reports')
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
    setExpandedFinding(findingId)
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

  if (data.bootLoading) {
    return (
      <div className="boot-screen">
        <div className="boot-card">
          <h1>PCI</h1>
          <p>Connecting to the API and loading workspace data…</p>
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
            <span>NetworkPolicy inspector</span>
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
              <span className="nav-item-copy">
                <span className="nav-item-label">{item.label}</span>
                <span className="nav-item-desc">{item.description}</span>
              </span>
            </button>
          ))}
        </nav>
        <div className="sidebar-foot">
          Read-only discovery and offline modeling. Not a live network controller.
        </div>
      </aside>

      <div className="main-column">
        <div className="topbar">
          <ConnectionStrip
            apiOk={data.apiConnected}
            apiLabel={data.apiConnected ? `connected (${data.health?.version ?? 'dev'})` : 'disconnected'}
            discoveryLabel={strip.label}
            discoveryTone={strip.tone}
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
            <ErrorBanner
              title="Action failed"
              message={data.actionError}
              onRetry={() => setData((prev) => ({ ...prev, actionError: null }))}
            />
          )}

          {route === 'overview' && (
            <>
              <PageHeader
                breadcrumbs={['PCI', 'Overview']}
                title="Cluster Overview"
                description="مشاهده و تحلیل NetworkPolicyهای کلاستر"
                actions={
                  <Button
                    variant="primary"
                    onClick={() => void runClusterDiscover()}
                    disabled={data.discoveryLoading || !data.apiConnected}
                  >
                    <Icon name="refresh" />
                    {data.discoveryLoading ? 'Discovering…' : 'Discover / Refresh'}
                  </Button>
                }
              />

              <section className="status-bar" aria-live="polite">
                <div>
                  <span className="status-label">Cluster connection</span>
                  <strong>{strip.label}</strong>
                </div>
                <div>
                  <span className="status-label">Last successful discovery</span>
                  <strong>{lastDiscoveryAt ?? 'Not yet available'}</strong>
                </div>
                <div>
                  <span className="status-label">Mode</span>
                  <strong>
                    {kubeSettings?.resolvedMode ||
                      kubeSettings?.mode ||
                      (kubeSettings?.inClusterEnv ? 'in-cluster' : 'auto')}
                  </strong>
                </div>
              </section>

              {data.discoveryError && (
                <ErrorBanner
                  title="Discovery unavailable"
                  message={data.discoveryError}
                  onRetry={() => void runClusterDiscover()}
                />
              )}
              {data.discoveryLoading && <Skeleton rows={3} />}

              <section className="metrics-grid">
                <StatTile
                  label="Namespaces"
                  value={namespaceCount ?? '—'}
                  available={namespaceCount != null}
                  hint={liveDiscovery ? 'From live discovery' : undefined}
                />
                <StatTile
                  label="NetworkPolicies"
                  value={networkPolicyCount ?? '—'}
                  available={networkPolicyCount != null}
                  hint={liveDiscovery ? 'From live discovery' : undefined}
                />
                <StatTile
                  label="Pods"
                  value={podCount ?? '—'}
                  available={podCount != null}
                  hint={liveDiscovery ? 'Discovered Pod workloads' : undefined}
                />
                <StatTile
                  label="Workloads"
                  value={workloadCount ?? '—'}
                  available={workloadCount != null}
                  hint={liveDiscovery ? 'Pods and controllers' : undefined}
                />
              </section>

              <Panel title="What you can do">
                <div className="action-grid">
                  <ActionCard
                    title="Cluster Discovery"
                    body="مشاهده منابع و policyهای کلاستر"
                    badge="live"
                    action={
                      <Button variant="primary" onClick={() => void runClusterDiscover()} disabled={data.discoveryLoading}>
                        Discover
                      </Button>
                    }
                  />
                  <ActionCard
                    title="Topology"
                    body="مشاهده روابط مدل‌شده policy و workload"
                    badge="modeled"
                    action={
                      <Button
                        variant="secondary"
                        onClick={() => {
                          setGraphSource(hasLiveDiscoveryGraph ? 'discovery' : 'analysis')
                          setRoute('topology')
                        }}
                      >
                        Open topology
                      </Button>
                    }
                  />
                  <ActionCard
                    title="Offline Analysis"
                    body="بررسی YAML/JSON و مشاهده findings"
                    badge="offline"
                    action={
                      <Button variant="secondary" onClick={() => setRoute('policies')}>
                        Analyze policies
                      </Button>
                    }
                  />
                  <ActionCard
                    title="Comparison"
                    body="بررسی تفاوت دو مجموعه policy"
                    badge="diff"
                    action={
                      <Button variant="secondary" onClick={() => setRoute('compare')}>
                        Compare policies
                      </Button>
                    }
                  />
                  <ActionCard
                    title="Reports"
                    body="مشاهده و ذخیره نتایج تحلیل"
                    badge="saved"
                    action={
                      <Button variant="secondary" onClick={() => setRoute('reports')}>
                        View reports
                      </Button>
                    }
                  />
                </div>
              </Panel>

              {liveDiscovery && (
                <Panel
                  title="Recently discovered policies"
                  meta={<Badge tone="success">{discoveryPolicies.length} live</Badge>}
                >
                  {discoveryPolicies.length === 0 ? (
                    <EmptyState
                      title="No NetworkPolicies in this scope"
                      body="Try another namespace filter or refresh discovery."
                      action={
                        <Button variant="secondary" onClick={() => void runClusterDiscover()}>
                          Refresh
                        </Button>
                      }
                    />
                  ) : (
                    <div className="table-wrap">
                      <table className="data-table">
                        <thead>
                          <tr>
                            <th>Namespace</th>
                            <th>Name</th>
                            <th>Types</th>
                            <th>Rules</th>
                          </tr>
                        </thead>
                        <tbody>
                          {discoveryPolicies.slice(0, 25).map((policy) => (
                            <tr
                              key={`${policy.namespace}/${policy.name}`}
                              className="clickable-row"
                              onClick={() => {
                                setNamespaceFilter(policy.namespace)
                                setGraphSource('discovery')
                                setRoute('topology')
                              }}
                            >
                              <td className="mono">{policy.namespace}</td>
                              <td className="mono">{policy.name}</td>
                              <td>{(policy.policyTypes ?? []).join(', ') || '—'}</td>
                              <td>
                                ingress {policy.ingressRules} · egress {policy.egressRules}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </Panel>
              )}

              {!data.discoveryLoading && !liveDiscovery && !data.discoveryError && (
                <EmptyState
                  title="No live discovery yet"
                  body="Run Discover to query the cluster configured for the API process (read-only)."
                  action={
                    <Button variant="primary" onClick={() => void runClusterDiscover()} disabled={!data.apiConnected}>
                      Discover cluster
                    </Button>
                  }
                />
              )}

              <Panel title="About PCI" meta={<Badge tone="muted">limits</Badge>}>
                <ul className="limits-list">
                  <li>PCI discovers cluster resources read-only; it never applies or deletes NetworkPolicies.</li>
                  <li>Topology edges model NetworkPolicy selectors — not real traffic paths or connection success.</li>
                  <li>Offline analysis reflects the YAML/JSON you provide, not necessarily the live cluster.</li>
                  <li>Current verification does not run production connectivity probes.</li>
                </ul>
              </Panel>
            </>
          )}

          {route === 'topology' && (
            <>
              <PageHeader
                breadcrumbs={['PCI', 'Topology']}
                title="Topology"
                description="Modeled relationships between namespaces, NetworkPolicies, and workloads"
                actions={
                  hasLiveDiscoveryGraph || liveDiscovery ? (
                    <Button variant="secondary" onClick={() => void runClusterDiscover()} disabled={data.discoveryLoading}>
                      {data.discoveryLoading ? 'Refreshing…' : 'Refresh discovery'}
                    </Button>
                  ) : undefined
                }
              />
              <Callout tone="warning">
                روابط بر اساس selectorهای NetworkPolicy مدل‌سازی شده‌اند. این گراف مسیر واقعی ترافیک یا موفقیت اتصال را
                نشان نمی‌دهد.
              </Callout>
              <div className="topology-layout">
                <Panel
                  title="Graph"
                  meta={
                    <Badge tone={useDiscoveryTopology ? 'success' : 'info'}>
                      {useDiscoveryTopology ? 'Live discovery' : 'Offline analysis'}
                    </Badge>
                  }
                >
                  <div className="toolbar">
                    <div className="segmented" role="group" aria-label="Graph source">
                      <button
                        type="button"
                        className={graphSource === 'discovery' ? 'active' : ''}
                        onClick={() => setGraphSource('discovery')}
                        disabled={!hasLiveDiscoveryGraph}
                        title={hasLiveDiscoveryGraph ? 'From discovered NetworkPolicies' : 'Run Discover first'}
                      >
                        Live discovery
                      </button>
                      <button
                        type="button"
                        className={graphSource === 'analysis' ? 'active' : ''}
                        onClick={() => setGraphSource('analysis')}
                      >
                        Offline analysis
                      </button>
                    </div>
                    {useDiscoveryTopology && (
                      <select
                        value={namespaceFilter}
                        onChange={(event) => setNamespaceFilter(event.target.value)}
                        aria-label="Namespace"
                      >
                        <option value="all">All policy namespaces</option>
                        {namespaces.map((namespace) => (
                          <option key={namespace} value={namespace}>
                            {namespace}
                          </option>
                        ))}
                      </select>
                    )}
                    <input
                      value={searchText}
                      onChange={(event) => setSearchText(event.target.value)}
                      placeholder="Search nodes"
                      aria-label="Search nodes"
                    />
                    <span className="meta-line">
                      {namespaceFilter === 'all' ? 'All namespaces' : namespaceFilter} · {topologyNodes.length} nodes ·{' '}
                      {topologyEdges.length} edges
                    </span>
                  </div>
                  <div
                    className={`graph-canvas topology-stage ${panning ? 'panning' : ''}`}
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
                    {topologyNodes.length === 0 ? (
                      <div className="topology-empty">
                        <EmptyState
                          title={graphSource === 'discovery' ? 'No discovery graph' : 'No analysis graph'}
                          body={
                            graphSource === 'discovery'
                              ? 'Run Discover, or choose another namespace that contains NetworkPolicies.'
                              : 'Analyze policies first, or switch to Live discovery.'
                          }
                          action={
                            graphSource === 'discovery' ? (
                              <Button variant="primary" onClick={() => void runClusterDiscover()}>
                                Discover / Refresh
                              </Button>
                            ) : (
                              <Button variant="secondary" onClick={() => setRoute('policies')}>
                                Analyze policies
                              </Button>
                            )
                          }
                        />
                      </div>
                    ) : (
                      <svg viewBox="0 0 960 560" aria-label="Topology graph" className="topology-svg">
                        <defs>
                          <marker id="arrow-allows" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
                            <path d="M 0 0 L 10 5 L 0 10 z" className="edge-marker allows" />
                          </marker>
                          <marker id="arrow-isolates" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
                            <path d="M 0 0 L 10 5 L 0 10 z" className="edge-marker isolates" />
                          </marker>
                          <marker id="arrow-egress" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
                            <path d="M 0 0 L 10 5 L 0 10 z" className="edge-marker egress" />
                          </marker>
                          <marker id="arrow-selects" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
                            <path d="M 0 0 L 10 5 L 0 10 z" className="edge-marker selects" />
                          </marker>
                        </defs>
                        <g transform={`translate(${pan.x} ${pan.y}) scale(${zoom})`}>
                          {topologyEdges.map((edge, index) => {
                            const source = nodeById.get(edge.source)
                            const target = nodeById.get(edge.target)
                            if (!source || !target) return null
                            const kind = 'kind' in edge ? edge.kind : undefined
                            const x1 = source.x ?? 80
                            const y1 = source.y ?? 80
                            const x2 = target.x ?? 280
                            const y2 = target.y ?? 160
                            const mx = (x1 + x2) / 2
                            const my = (y1 + y2) / 2 - 18
                            return (
                              <g key={edge.id} className="edge-group" style={{ animationDelay: `${index * 40}ms` }}>
                                <path
                                  d={`M ${x1} ${y1} Q ${mx} ${my} ${x2} ${y2}`}
                                  className={`edge flow ${kind || 'selects'}`}
                                  markerEnd={`url(#arrow-${kind || 'selects'})`}
                                />
                              </g>
                            )
                          })}
                          {topologyNodes.map((node, index) => {
                            const cx = node.x ?? 80 + (index % 4) * 110
                            const cy = node.y ?? 70 + Math.floor(index / 4) * 90
                            const radius = node.type === 'namespace' ? 28 : node.type === 'policy' ? 22 : 18
                            return (
                              <g
                                key={node.id}
                                className="node-group"
                                style={{ animationDelay: `${80 + index * 35}ms` }}
                                onClick={() => setSelectedNodeId(node.id)}
                              >
                                <circle
                                  cx={cx}
                                  cy={cy}
                                  r={radius + 6}
                                  className={`node-halo ${node.type} ${selectedNodeId === node.id ? 'selected' : ''}`}
                                />
                                <circle
                                  cx={cx}
                                  cy={cy}
                                  r={radius}
                                  className={`node ${node.type} ${selectedNodeId === node.id ? 'selected' : ''}`}
                                />
                                <text className="graph-label" x={cx} y={cy + radius + 16} textAnchor="middle">
                                  {node.label}
                                </text>
                              </g>
                            )
                          })}
                        </g>
                      </svg>
                    )}
                  </div>
                  <div className="legend" aria-label="Legend">
                    <span>
                      <i className="namespace" />
                      Namespace
                    </span>
                    <span>
                      <i className="policy" />
                      Policy
                    </span>
                    <span>
                      <i className="workload" />
                      Workload
                    </span>
                    <span className="legend-edge allows">selector match</span>
                    <span className="legend-edge isolates">isolates</span>
                  </div>
                </Panel>
                <Panel title="Selection" meta={<Badge tone="muted">details</Badge>}>
                  {selectedNode ? (
                    <div className="detail-card">
                      <h4>{selectedNode.label}</h4>
                      <p>
                        <strong>Type:</strong> {selectedNode.type}
                      </p>
                      <p>
                        <strong>ID:</strong> <span className="mono">{selectedNode.id}</span>
                      </p>
                      {selectedNode.meta?.namespace && (
                        <p>
                          <strong>Namespace:</strong> {selectedNode.meta.namespace}
                        </p>
                      )}
                      {selectedNode.meta?.kind && (
                        <p>
                          <strong>Kind:</strong> {selectedNode.meta.kind}
                        </p>
                      )}
                      {selectedNode.meta?.policyTypes && (
                        <p>
                          <strong>Policy types:</strong> {selectedNode.meta.policyTypes.join(', ') || '—'}
                        </p>
                      )}
                      {selectedNode.meta?.ingressRules != null && (
                        <p>
                          <strong>Rules:</strong> ingress {selectedNode.meta.ingressRules} · egress{' '}
                          {selectedNode.meta.egressRules ?? 0}
                        </p>
                      )}
                      {selectedNode.meta?.detail && <p className="meta-line">{selectedNode.meta.detail}</p>}
                      <Callout tone="muted">
                        {useDiscoveryTopology
                          ? 'Live discovery: selector-modeled relationships only.'
                          : 'Offline analysis graph from manifests — not live cluster traffic.'}
                      </Callout>
                    </div>
                  ) : (
                    <EmptyState title="Select a node" body="Click a node in the graph to inspect details." />
                  )}
                </Panel>
              </div>
            </>
          )}

          {route === 'policies' && (
            <>
              <PageHeader
                breadcrumbs={['PCI', 'Policies']}
                title="Policies"
                description="Live inventory from discovery and offline YAML/JSON analysis"
              />
              <Callout tone="info">
                Offline analysis uses the YAML/JSON you provide and does not necessarily reflect the live cluster.
              </Callout>
              <div className="split-2">
                <Panel title="Offline analysis" meta={<Badge tone="muted">POST /analyses</Badge>}>
                  <p className="meta-line">Paste NetworkPolicy YAML or JSON, then analyze.</p>
                  <textarea
                    className="manifest-editor"
                    value={manifestText}
                    onChange={(event) => setManifestText(event.target.value)}
                    aria-label="Policy manifest"
                    placeholder="apiVersion: networking.k8s.io/v1&#10;kind: NetworkPolicy&#10;..."
                  />
                  <div className="page-actions">
                    <Button variant="primary" onClick={() => void runAnalyzeManifest()} disabled={data.dataLoading}>
                      Analyze policies
                    </Button>
                    {(sampleAvailable || sampleBase) && (
                      <Button variant="secondary" onClick={() => void runAnalyzeSample()} disabled={data.dataLoading}>
                        Load sample fixture
                      </Button>
                    )}
                  </div>
                </Panel>
                <Panel
                  title="Analysis results"
                  meta={
                    <Badge tone="info">
                      {data.analysis ? `${data.analysis.summary.totalPolicies} policies` : 'none'}
                    </Badge>
                  }
                >
                  {!data.analysis ? (
                    <EmptyState
                      title="No analysis yet"
                      body="Paste a manifest and analyze, or load the project sample fixture."
                      action={
                        <Button variant="secondary" onClick={() => void runAnalyzeSample()} disabled={data.dataLoading}>
                          Load sample fixture
                        </Button>
                      }
                    />
                  ) : (
                    <>
                      <div className="metrics-grid compact">
                        <StatTile label="Policies" value={data.analysis.summary.totalPolicies} hint={data.analysis.path || 'manifest'} />
                        <StatTile label="Findings" value={data.analysis.summary.findingCount} />
                        <StatTile label="Default deny" value={data.analysis.summary.defaultDeny} />
                        <StatTile
                          label="Generated"
                          value={new Date(data.analysis.generatedAt).toLocaleString()}
                        />
                      </div>
                      <div className="page-actions">
                        <Button variant="secondary" onClick={() => { setReportsTab('findings'); setRoute('reports') }}>
                          View findings
                        </Button>
                        <Button variant="secondary" onClick={() => void runCreateSnapshot()}>
                          Save snapshot
                        </Button>
                        <Button
                          variant="secondary"
                          onClick={() => {
                            setGraphSource('analysis')
                            setRoute('topology')
                          }}
                        >
                          Open graph
                        </Button>
                      </div>
                    </>
                  )}
                </Panel>
              </div>

              {liveDiscovery && (
                <Panel
                  title="Discovered NetworkPolicies"
                  meta={<Badge tone="success">{discoveryPolicies.length} live</Badge>}
                >
                  {discoveryPolicies.length === 0 ? (
                    <EmptyState title="None in scope" body="No NetworkPolicies returned for the current discovery." />
                  ) : (
                    <div className="table-wrap">
                      <table className="data-table">
                        <thead>
                          <tr>
                            <th>Namespace / name</th>
                            <th>Types</th>
                            <th>Ingress</th>
                            <th>Egress</th>
                          </tr>
                        </thead>
                        <tbody>
                          {discoveryPolicies.map((policy) => (
                            <tr key={`${policy.namespace}/${policy.name}`}>
                              <td className="mono">
                                {policy.namespace}/{policy.name}
                              </td>
                              <td>{(policy.policyTypes ?? []).join(', ') || '—'}</td>
                              <td>{policy.ingressRules}</td>
                              <td>{policy.egressRules}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </Panel>
              )}

              <Panel title="Offline policy inventory" meta={<Badge tone="muted">{filteredPolicies.length}</Badge>}>
                <div className="toolbar">
                  <input
                    value={policyQuery}
                    onChange={(event) => setPolicyQuery(event.target.value)}
                    placeholder="Search policies"
                    aria-label="Search policies"
                  />
                  <select
                    value={namespaceFilter}
                    onChange={(event) => setNamespaceFilter(event.target.value)}
                    aria-label="Filter namespace"
                  >
                    <option value="all">All namespaces</option>
                    {namespaces.map((namespace) => (
                      <option key={namespace} value={namespace}>
                        {namespace}
                      </option>
                    ))}
                  </select>
                </div>
                {filteredPolicies.length === 0 ? (
                  <EmptyState title="No policies" body="Analyze a manifest or load a sample to populate this list." />
                ) : (
                  <div className="policy-list">
                    {filteredPolicies.map((policy) => {
                      const key = `${policy.namespace}/${policy.name}`
                      const open = expandedPolicy === key
                      return (
                        <article key={key} className="policy-card">
                          <div className="policy-card-head">
                            <h4>{key}</h4>
                            <div className="page-actions">
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
                              <p>
                                <strong>Match labels:</strong>{' '}
                                {(policy.matchLabels ?? []).join(', ') || 'None listed'}
                              </p>
                              <p>
                                <strong>Related peers:</strong>{' '}
                                {(policy.relatedPeers ?? []).join(', ') || 'None listed'}
                              </p>
                            </div>
                          )}
                        </article>
                      )
                    })}
                  </div>
                )}
              </Panel>
            </>
          )}

          {route === 'compare' && (
            <>
              <PageHeader
                breadcrumbs={['PCI', 'Compare']}
                title="Compare"
                description="Diff a base policy set against a proposed set"
                actions={
                  <Button variant="primary" onClick={() => void runDiff()} disabled={data.dataLoading}>
                    Run comparison
                  </Button>
                }
              />
              <Callout tone="warning">
                این مقایسه تغییرات policy را نشان می‌دهد؛ به‌تنهایی علت قطع ترافیک را اثبات نمی‌کند.
              </Callout>
              <Panel title="Inputs">
                <div className="segmented" role="group" aria-label="Compare input mode">
                  <button
                    type="button"
                    className={compareMode === 'paths' ? 'active' : ''}
                    onClick={() => setCompareMode('paths')}
                  >
                    1. Paths
                  </button>
                  <button
                    type="button"
                    className={compareMode === 'manifests' ? 'active' : ''}
                    onClick={() => setCompareMode('manifests')}
                  >
                    2. Paste YAML
                  </button>
                </div>
                {compareMode === 'paths' ? (
                  <div className="toolbar" style={{ marginTop: 12 }}>
                    <label className="field">
                      Base path
                      <input value={analysisPath} onChange={(event) => setAnalysisPath(event.target.value)} />
                    </label>
                    <label className="field">
                      Proposed path
                      <input value={proposedPath} onChange={(event) => setProposedPath(event.target.value)} />
                    </label>
                    {sampleAvailable && (
                      <Button
                        variant="ghost"
                        onClick={() => {
                          setAnalysisPath(sampleBase)
                          setProposedPath(sampleProposed)
                        }}
                      >
                        Use sample paths
                      </Button>
                    )}
                  </div>
                ) : (
                  <div className="split-2" style={{ marginTop: 12 }}>
                    <label className="field">
                      Base (YAML/JSON)
                      <textarea
                        className="manifest-editor short"
                        value={baseManifest}
                        onChange={(event) => setBaseManifest(event.target.value)}
                        aria-label="Base manifest"
                      />
                    </label>
                    <label className="field">
                      Proposed (YAML/JSON)
                      <textarea
                        className="manifest-editor short"
                        value={proposedManifest}
                        onChange={(event) => setProposedManifest(event.target.value)}
                        aria-label="Proposed manifest"
                      />
                    </label>
                  </div>
                )}
                <p className="meta-line" style={{ marginTop: 8 }}>
                  3. Run comparison → 4. Review added / removed / modified fields below.
                </p>
              </Panel>
              <Panel title="Changes" meta={<Badge tone="warning">config delta ≠ causation</Badge>}>
                {!data.diff ? (
                  <EmptyState
                    title="No comparison yet"
                    body="Choose base and proposed inputs, then run comparison."
                    action={
                      <Button variant="primary" onClick={() => void runDiff()}>
                        Run comparison
                      </Button>
                    }
                  />
                ) : (
                  <div className="split-2">
                    <div className="detail-card">
                      <h4>Added ({data.diff.added.length})</h4>
                      <p>{data.diff.added.length ? data.diff.added.join(', ') : 'None'}</p>
                      <h4>Removed ({data.diff.removed.length})</h4>
                      <p>{data.diff.removed.length ? data.diff.removed.join(', ') : 'None'}</p>
                      {(data.diff.findings?.length ?? 0) > 0 && (
                        <>
                          <h4>Comparison findings ({data.diff.findings.length})</h4>
                          <ul className="list-stack">
                            {data.diff.findings.map((finding) => (
                              <li key={finding.id} className="list-item">
                                <Badge tone={severityTone(finding.severity)}>{severityLabel(finding.severity)}</Badge>{' '}
                                {finding.message}
                              </li>
                            ))}
                          </ul>
                        </>
                      )}
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
                              <p className="meta-line">
                                Fields: {entry.changedFields.join(', ') || 'Not reported'}
                              </p>
                              <details>
                                <summary>Field path before / after</summary>
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
            </>
          )}

          {route === 'reports' && (
            <>
              <PageHeader
                breadcrumbs={['PCI', 'Reports']}
                title="Reports"
                description="Findings from the current analysis, listed snapshots, and the last generated report"
                actions={
                  <div className="page-actions">
                    <Button variant="secondary" onClick={() => void runCreateSnapshot()} disabled={!data.apiConnected}>
                      Save snapshot
                    </Button>
                    <Button variant="primary" onClick={() => void runReport()} disabled={data.dataLoading}>
                      Generate report
                    </Button>
                  </div>
                }
              />
              <Callout tone="muted">
                Findings come from the current offline analysis. Snapshots are loaded from{' '}
                <span className="mono">GET /snapshots</span>. There is no report list API — Generate report shows
                only the report created in this session (and stores it server-side when SQLite is available).
              </Callout>
              <div className="segmented" role="tablist" aria-label="Reports sections">
                <button
                  type="button"
                  role="tab"
                  aria-selected={reportsTab === 'findings'}
                  className={reportsTab === 'findings' ? 'active' : ''}
                  onClick={() => setReportsTab('findings')}
                >
                  Findings
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={reportsTab === 'snapshots'}
                  className={reportsTab === 'snapshots' ? 'active' : ''}
                  onClick={() => setReportsTab('snapshots')}
                >
                  Snapshots
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={reportsTab === 'report'}
                  className={reportsTab === 'report' ? 'active' : ''}
                  onClick={() => setReportsTab('report')}
                >
                  Report
                </button>
              </div>

              {reportsTab === 'findings' && (
                <div className="topology-layout" style={{ marginTop: 16 }}>
                  <Panel title="Findings" meta={<Badge tone="muted">{filteredFindings.length}</Badge>}>
                    <div className="toolbar">
                      <input
                        value={searchText}
                        onChange={(event) => setSearchText(event.target.value)}
                        placeholder="Search findings"
                        aria-label="Search findings"
                      />
                      <select
                        value={severityFilter}
                        onChange={(event) => setSeverityFilter(event.target.value)}
                        aria-label="Severity filter"
                      >
                        <option value="all">All severities</option>
                        <option value="critical">Critical</option>
                        <option value="high">High</option>
                        <option value="medium">Medium</option>
                        <option value="low">Low</option>
                      </select>
                    </div>
                    {filteredFindings.length === 0 ? (
                      <EmptyState
                        title="No findings"
                        body="Analyze policies first to produce modeled findings."
                        action={
                          <Button variant="secondary" onClick={() => setRoute('policies')}>
                            Go to Policies
                          </Button>
                        }
                      />
                    ) : (
                      <ul className="finding-list">
                        {filteredFindings.map((finding) => {
                          const open = expandedFinding === finding.id
                          return (
                            <li key={finding.id} className="list-item">
                              <div className="policy-card-head">
                                <strong>{finding.message}</strong>
                                <Badge tone={severityTone(finding.severity)} title={`Severity: ${finding.severity}`}>
                                  Severity: {severityLabel(finding.severity)}
                                </Badge>
                              </div>
                              <p className="meta-line mono">
                                {finding.id} · {finding.kind || 'finding'}
                              </p>
                              <Button
                                variant="ghost"
                                onClick={() => {
                                  if (open) {
                                    setExpandedFinding(null)
                                  } else {
                                    void runExplain(finding.id)
                                  }
                                }}
                              >
                                {open ? 'Hide explanation' : 'Show explanation'}
                              </Button>
                              {open && data.selectedFinding?.findingId === finding.id && (
                                <div className="detail-card" style={{ marginTop: 10 }}>
                                  <p>
                                    <strong>Traffic status:</strong> {data.selectedFinding.trafficStatus}
                                  </p>
                                  <p>
                                    <strong>Summary:</strong> {data.selectedFinding.summary}
                                  </p>
                                  <p>
                                    <strong>Reason:</strong> {data.selectedFinding.reason}
                                  </p>
                                  <p className="meta-line">
                                    Modeled explanation — not a verified runtime outcome.
                                  </p>
                                </div>
                              )}
                            </li>
                          )
                        })}
                      </ul>
                    )}
                  </Panel>
                  <Panel title="Selected explanation" meta={<Badge tone="info">modeled</Badge>}>
                    {data.selectedFinding && selectedFindingId ? (
                      <div className="detail-card">
                        <h4 className="mono">{data.selectedFinding.findingId}</h4>
                        <p>
                          <strong>Summary:</strong> {data.selectedFinding.summary}
                        </p>
                        <p>
                          <strong>Reason:</strong> {data.selectedFinding.reason}
                        </p>
                        <p>
                          <strong>Applicable policies:</strong>{' '}
                          {(data.selectedFinding.applicablePolicies ?? []).join(', ') || 'Not available'}
                        </p>
                        <details>
                          <summary>Assumptions & unsupported</summary>
                          <p>{(data.selectedFinding.assumptions ?? []).join('; ') || 'None documented'}</p>
                          <p>{(data.selectedFinding.unsupported ?? []).join('; ') || 'None listed'}</p>
                        </details>
                      </div>
                    ) : (
                      <EmptyState title="Select a finding" body="Open a finding to load its API explanation." />
                    )}
                  </Panel>
                </div>
              )}

              {reportsTab === 'snapshots' && (
                <Panel
                  title="Snapshots from API"
                  meta={<Badge tone="muted">{data.snapshots.length} loaded</Badge>}
                >
                  <p className="meta-line" style={{ marginBottom: 12 }}>
                    Showing snapshots returned by the API (up to 8 on load), not a complete archive browser.
                  </p>
                  {data.snapshots.length === 0 ? (
                    <EmptyState
                      title="No snapshots returned"
                      body="Save a snapshot from the current analysis, then Refresh to reload the list from the API."
                      action={
                        <Button variant="primary" onClick={() => void runCreateSnapshot()}>
                          Save snapshot
                        </Button>
                      }
                    />
                  ) : (
                    <ul className="list-stack">
                      {data.snapshots.map((snapshot) => (
                        <li key={snapshot.id} className="snapshot-row">
                          <div className="policy-card-head">
                            <strong>{snapshot.name || snapshot.id}</strong>
                            <Badge tone="muted">{snapshot.summary?.findingCount ?? 0} findings</Badge>
                          </div>
                          <p className="mono meta-line">{snapshot.id}</p>
                          <p className="meta-line">
                            {snapshot.generatedAt
                              ? new Date(snapshot.generatedAt).toLocaleString()
                              : snapshot.path}{' '}
                            · Policies {snapshot.summary?.totalPolicies ?? '—'}
                          </p>
                        </li>
                      ))}
                    </ul>
                  )}
                </Panel>
              )}

              {reportsTab === 'report' && (
                <Panel title="Last generated report (this session)" meta={<Badge tone="muted">POST only</Badge>}>
                  {!data.report ? (
                    <EmptyState
                      title="No report in this session"
                      body="PCI cannot list historical reports yet. Generate one from the current analysis to view it here."
                      action={
                        <Button variant="primary" onClick={() => void runReport()}>
                          Generate report
                        </Button>
                      }
                    />
                  ) : (
                    <>
                      <div className="metrics-grid">
                        <StatTile
                          label="Policies"
                          value={data.report.policyCount ?? data.report.summary?.totalPolicies ?? '—'}
                        />
                        <StatTile
                          label="Findings"
                          value={data.report.findingCount ?? data.report.summary?.findingCount ?? '—'}
                        />
                        <StatTile
                          label="Generated"
                          value={
                            data.report.generatedAt
                              ? new Date(data.report.generatedAt).toLocaleString()
                              : '—'
                          }
                        />
                        <StatTile label="Analysis" value={data.report.analysisId || '—'} />
                      </div>
                      {data.report.recommendation && <Callout tone="info">{data.report.recommendation}</Callout>}
                      <details>
                        <summary>Raw report JSON</summary>
                        <pre className="json-box">{JSON.stringify(data.report, null, 2)}</pre>
                      </details>
                    </>
                  )}
                </Panel>
              )}
            </>
          )}
        </main>
      </div>
    </div>
  )
}

export default App
