import { useCallback, useEffect, useMemo, useState } from 'react'
import './App.css'

const API_BASE = (typeof import.meta !== 'undefined' && import.meta.env && import.meta.env.VITE_API_BASE) || 'http://127.0.0.1:8080/api/v1'

type Route = 'dashboard' | 'topology' | 'policies' | 'results' | 'findings' | 'comparison' | 'snapshots' | 'verification' | 'reports' | 'settings'

type PolicySummary = {
  namespace: string
  name: string
  policyType: string
  selector: string
  ingressRules: number
  egressRules: number
  defaultDeny: boolean
  matchLabels: string[]
  relatedPeers: string[]
}

type Finding = {
  id: string
  kind: string
  message: string
  severity: string
  confidence: string
  source?: string
  target?: string
  relevantPolicies?: string[]
  evidence?: string[]
  assumptions?: string[]
}

type Analysis = {
  id?: string
  path: string
  generatedAt: string
  summary: { totalPolicies: number; defaultDeny: number; findingCount: number }
  policies: PolicySummary[]
  findings: Finding[]
}

type GraphNode = { id: string; label: string; type: string; x?: number; y?: number }
type GraphEdge = { id: string; source: string; target: string; label: string }
type Graph = { nodes: GraphNode[]; edges: GraphEdge[] }
type PolicyFieldChange = { field: string; before: unknown; after: unknown }
type DiffReport = { added: string[]; removed: string[]; modified: Array<{ key: string; old: string; new: string; reason: string; changedFields: string[]; fieldChanges: PolicyFieldChange[] }>; findings: Finding[] }
type Snapshot = { id: string; name: string; path: string; generatedAt: string; summary: Analysis['summary']; policies: PolicySummary[]; findings: Finding[] }
type Explanation = { findingId: string; trafficStatus: string; summary: string; direction?: string; applicablePolicies?: string[]; reason: string; assumptions?: string[]; unsupported?: string[] }
type Report = { analysisId?: string; generatedAt?: string; path?: string; policyCount?: number; findingCount?: number; findings?: Finding[]; graph?: Graph; recommendation?: string; summary?: Analysis['summary'] }
type VerificationEvidence = {
  id: string
  sourceNamespace: string
  sourceWorkload: string
  destination: string
  protocol: string
  port: number
  attemptedAt: string
  duration: number
  outcome: 'success' | 'failure' | 'timeout' | 'unknown'
  state: 'observed_success' | 'observed_failure' | 'unknown'
  method: string
  errorClass?: string
  cluster?: string
  context?: string
  limitations: string[]
  provenance: string
  modelComparison: string
}

type AppState = {
  health: { ok: boolean; service?: string; version?: string; database?: { sqlite: boolean } } | null
  capabilities: { capabilities?: string[]; status?: string; kubernetes?: { mode: string; status: string; offline: boolean; warnings?: string[] }; verification?: { enabled: boolean; provider?: string; runtimeProbeImplemented: boolean } } | null
  analyses: Analysis[]
  diff: DiffReport | null
  snapshots: Snapshot[]
  report: Report | null
  graph: Graph | null
  selectedFinding: Explanation | null
  verificationEvidence: VerificationEvidence[]
  verificationEnabled: boolean
  connected: boolean
  loading: boolean
  error: string | null
}

const initialState: AppState = {
  health: null,
  capabilities: null,
  analyses: [],
  diff: null,
  snapshots: [],
  report: null,
  graph: null,
  selectedFinding: null,
  verificationEvidence: [],
  verificationEnabled: false,
  connected: false,
  loading: true,
  error: null,
}

const navItems: { label: string; key: Route }[] = [
  { label: 'Dashboard', key: 'dashboard' },
  { label: 'Topology', key: 'topology' },
  { label: 'Policies', key: 'policies' },
  { label: 'Results', key: 'results' },
  { label: 'Findings', key: 'findings' },
  { label: 'Comparison', key: 'comparison' },
  { label: 'Snapshots', key: 'snapshots' },
  { label: 'Verification', key: 'verification' },
  { label: 'Reports', key: 'reports' },
  { label: 'Settings', key: 'settings' },
]

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...(init?.headers || {}) },
  })
  if (!response.ok) {
    const body = await response.text().catch(() => '')
    throw new Error(body || `request failed with status ${response.status}`)
  }
  return response.json() as Promise<T>
}

function getSeverityClass(value: string) {
  const level = (value || '').toLowerCase()
  if (level.includes('high') || level.includes('critical')) return 'danger'
  if (level.includes('medium')) return 'warning'
  return 'good'
}

function App() {
  const [route, setRoute] = useState<Route>('dashboard')
  const [state, setState] = useState<AppState>(initialState)
  const [analysisPath, setAnalysisPath] = useState('fixtures/base')
  const [manifestText, setManifestText] = useState(`apiVersion: networking.k8s.io/v1
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
`)
  const [namespaceFilter, setNamespaceFilter] = useState('all')
  const [searchText, setSearchText] = useState('')
  const [changedOnly, setChangedOnly] = useState(false)
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)

  const loadData = useCallback(async () => {
    setState((prev) => ({ ...prev, loading: true, error: null }))
    try {
      const health = await fetchJSON<{ ok: boolean; service?: string; version?: string; database?: { sqlite: boolean } }>(`${API_BASE}/health`)
      const capabilities = await fetchJSON<{ capabilities?: string[]; status?: string; kubernetes?: { mode: string; status: string; offline: boolean; warnings?: string[] }; verification?: { enabled: boolean; provider?: string; runtimeProbeImplemented: boolean } }>(`${API_BASE}/capabilities`)
      const analysesResponse = await fetchJSON<{ analyses: Array<{ id: string; kind: string; name?: string; path?: string; createdAt?: string }> }>(`${API_BASE}/analyses`)
      const storedAnalyses = analysesResponse.analyses ?? []
      let analysis: Analysis | null = null
      if (storedAnalyses[0]) {
        analysis = await fetchJSON<Analysis>(`${API_BASE}/analyses/${storedAnalyses[0].id}`)
      }
      if (!analysis) {
        const created = await fetchJSON<Analysis>(`${API_BASE}/analyses`, {
          method: 'POST',
          body: JSON.stringify({ path: analysisPath }),
        })
        analysis = created
      }
      const graph = await fetchJSON<Graph>(`${API_BASE}/analyses/${analysis.id}/graph`)
      await fetchJSON<{ findings: Finding[] }>(`${API_BASE}/analyses/${analysis.id}/findings`)
      const diff = await fetchJSON<DiffReport>(`${API_BASE}/diff`, {
        method: 'POST',
        body: JSON.stringify({ basePath: 'fixtures/base', proposedPath: 'fixtures/proposed' }),
      })
      const snapshotsResponse = await fetchJSON<{ snapshots: Array<{ id: string; name: string; path: string; createdAt: string }> }>(`${API_BASE}/snapshots`)
      const snapshotDetails = await Promise.all(
        snapshotsResponse.snapshots.slice(0, 5).map(async (snapshot) => fetchJSON<Snapshot>(`${API_BASE}/snapshots/${snapshot.id}`)),
      )
      const report = await fetchJSON<Report>(`${API_BASE}/reports`, {
        method: 'POST',
        body: JSON.stringify({ path: 'fixtures/base' }),
      })
      const verification = await fetchJSON<{ evidence: VerificationEvidence[]; verificationEnabled: boolean }>(`${API_BASE}/verifications`)

      setState({
        health,
        capabilities,
        analyses: [analysis],
        diff,
        snapshots: snapshotDetails,
        report,
        graph,
        selectedFinding: null,
        verificationEvidence: verification.evidence ?? [],
        verificationEnabled: capabilities.verification?.enabled ?? verification.verificationEnabled,
        connected: health.ok,
        loading: false,
        error: null,
      })
      setSelectedNodeId(graph.nodes[0]?.id ?? null)
    } catch (error) {
      setState((prev) => ({ ...prev, connected: false, loading: false, error: error instanceof Error ? error.message : 'request failed' }))
    }
  }, [analysisPath])

  useEffect(() => {
    void loadData()
  }, [loadData])

  const currentAnalysis = state.analyses[0] ?? null
  const currentFindingList = currentAnalysis?.findings ?? []

  const topologyNodes = useMemo(() => {
    if (!state.graph?.nodes) return []
    const query = searchText.trim().toLowerCase()
    return state.graph.nodes.filter((node) => {
      const namespaceMatch = namespaceFilter === 'all' || node.id.includes(namespaceFilter)
      const searchMatch = !query || node.label.toLowerCase().includes(query) || node.id.toLowerCase().includes(query)
      const changedMatch = !changedOnly || (state.diff?.modified.some((updated) => node.id.includes(updated.key.split('/').pop() ?? '')) || state.diff?.added.some((item) => node.id.includes(item.split('/').pop() ?? '')) || state.diff?.removed.some((item) => node.id.includes(item.split('/').pop() ?? '')))
      return namespaceMatch && searchMatch && changedMatch
    })
  }, [state.graph, searchText, namespaceFilter, changedOnly, state.diff])

  const topologyEdges = useMemo(() => {
    if (!state.graph?.edges) return []
    const ids = new Set(topologyNodes.map((node) => node.id))
    return state.graph.edges.filter((edge) => ids.has(edge.source) && ids.has(edge.target))
  }, [state.graph, topologyNodes])

  const selectedNode = topologyNodes.find((node) => node.id === selectedNodeId) ?? state.graph?.nodes.find((node) => node.id === selectedNodeId) ?? null

  async function analyzeManifest(manifest: string, name?: string) {
    const resp = await fetchJSON<Analysis>(`${API_BASE}/analyses`, {
      method: 'POST',
      body: JSON.stringify({ manifest, name: name ?? 'manual-proposed' }),
    })
    const graph = await fetchJSON<Graph>(`${API_BASE}/analyses/${resp.id}/graph`)
    setState((prev) => ({
      ...prev,
      analyses: [resp],
      graph,
      connected: true,
      loading: false,
      error: null,
    }))
    setSelectedNodeId(graph.nodes[0]?.id ?? null)
    return resp
  }

  async function createSnapshot() {
    const snapshot = await fetchJSON<Snapshot>(`${API_BASE}/snapshots`, {
      method: 'POST',
      body: JSON.stringify({ path: analysisPath }),
    })
    setState((prev) => ({ ...prev, snapshots: [snapshot, ...prev.snapshots] }))
  }

  async function explainFinding(findingId: string) {
    const analysisId = currentAnalysis?.id
    if (!analysisId) return
    const explanation = await fetchJSON<Explanation>(`${API_BASE}/analyses/${analysisId}/explain/${findingId}`)
    setState((prev) => ({ ...prev, selectedFinding: explanation }))
  }

  const summaryCards = [
    { label: 'Policies analyzed', value: String(currentAnalysis?.summary.totalPolicies ?? 0), tone: 'neutral' },
    { label: 'Default deny', value: String(currentAnalysis?.summary.defaultDeny ?? 0), tone: 'warning' },
    { label: 'High-risk exposure', value: String(currentFindingList.filter((finding) => ['high', 'critical'].includes((finding.severity || '').toLowerCase())).length), tone: 'danger' },
    { label: 'Plan confidence', value: state.capabilities?.kubernetes?.offline ? 'Offline' : 'Ready', tone: 'good' },
  ]

  const namespaces = Array.from(new Set((currentAnalysis?.policies ?? []).map((policy) => policy.namespace))).filter(Boolean)

  const renderDashboard = () => (
    <>
      <header className="topbar">
        <div>
          <p className="eyebrow">{state.health?.ok ? 'cluster: offline-ready' : 'cluster: disconnected'}</p>
          <h2>Change impact overview</h2>
        </div>
        <button className="primary" onClick={() => void loadData()}>Refresh</button>
      </header>
      <section className="metrics-grid">
        {summaryCards.map((card) => (
          <article key={card.label} className={`metric-card ${card.tone}`}>
            <span>{card.label}</span>
            <strong>{card.value}</strong>
          </article>
        ))}
      </section>
      <section className="content-grid">
        <article className="panel">
          <div className="panel-header">
            <h3>Modeled connectivity</h3>
            <span className="status-chip">{state.connected ? 'read-only' : 'offline'}</span>
          </div>
          {state.graph ? (
            <svg viewBox="0 0 420 220" className="graph" aria-label="Network graph">
              {state.graph.nodes.map((node, index) => (
                <g key={node.id} onClick={() => setSelectedNodeId(node.id)} style={{ cursor: 'pointer' }}>
                  <circle cx={node.x ?? 60 + (index % 3) * 110} cy={node.y ?? 70 + (index % 2) * 90} r={node.type === 'cluster' ? 28 : 24} className={`node ${node.type}`} />
                  <text x={(node.x ?? 60 + (index % 3) * 110) - 18} y={(node.y ?? 70 + (index % 2) * 90) + 4}>{node.label}</text>
                </g>
              ))}
              {state.graph.edges.map((edge) => (
                <line key={edge.id} x1={(state.graph?.nodes.find((node) => node.id === edge.source)?.x ?? 60)} y1={(state.graph?.nodes.find((node) => node.id === edge.source)?.y ?? 60)} x2={(state.graph?.nodes.find((node) => node.id === edge.target)?.x ?? 300)} y2={(state.graph?.nodes.find((node) => node.id === edge.target)?.y ?? 140)} className="edge" />
              ))}
            </svg>
          ) : (
            <div className="empty-state">No graph is available yet.</div>
          )}
        </article>
        <article className="panel">
          <div className="panel-header">
            <h3>Findings</h3>
            <span className="status-chip muted">structured</span>
          </div>
          <ul className="finding-list">
            {currentFindingList.length === 0 ? <li className="empty-state">No findings were detected.</li> : currentFindingList.map((finding) => (
              <li key={finding.id} className={`finding ${getSeverityClass(finding.severity)}`}>
                <button className="inline-button" onClick={() => void explainFinding(finding.id)}>{finding.id}</button>
                <span>{finding.message}</span>
                <small>{finding.evidence?.join(', ') || 'manifest-derived evidence'}</small>
              </li>
            ))}
          </ul>
        </article>
      </section>
    </>
  )

  const renderTopology = () => (
    <div className="stacked-grid">
      <div className="panel">
        <div className="toolbar">
          <input value={searchText} onChange={(event) => setSearchText(event.target.value)} placeholder="Search node or policy" />
          <select value={namespaceFilter} onChange={(event) => setNamespaceFilter(event.target.value)}>
            <option value="all">All namespaces</option>
            {namespaces.map((namespace) => <option key={namespace} value={namespace}>{namespace}</option>)}
          </select>
          <label className="checkbox-row"><input type="checkbox" checked={changedOnly} onChange={(event) => setChangedOnly(event.target.checked)} /> Changed only</label>
        </div>
        <svg viewBox="0 0 420 220" className="graph" aria-label="Topology graph">
          {topologyEdges.map((edge) => {
            const source = state.graph?.nodes.find((node) => node.id === edge.source)
            const target = state.graph?.nodes.find((node) => node.id === edge.target)
            if (!source || !target) return null
            return <line key={edge.id} x1={source.x ?? 80} y1={source.y ?? 80} x2={target.x ?? 300} y2={target.y ?? 140} className="edge" />
          })}
          {topologyNodes.map((node, index) => (
            <g key={node.id} onClick={() => setSelectedNodeId(node.id)} style={{ cursor: 'pointer' }}>
              <circle cx={node.x ?? 80 + (index % 3) * 120} cy={node.y ?? 90 + (index % 2) * 70} r={node.type === 'cluster' ? 28 : 22} className={`node ${node.type}`} />
              <text x={(node.x ?? 80 + (index % 3) * 120) - 16} y={(node.y ?? 90 + (index % 2) * 70) + 4}>{node.label}</text>
            </g>
          ))}
        </svg>
        <div className="graph-list">
          {topologyNodes.length === 0 ? <div className="empty-state">No graph nodes match the current filter.</div> : topologyNodes.map((node) => (
            <button key={node.id} className={`graph-node-item ${selectedNodeId === node.id ? 'selected' : ''}`} onClick={() => setSelectedNodeId(node.id)}>
              <span>{node.label}</span>
              <small>{node.type}</small>
            </button>
          ))}
        </div>
      </div>
      <div className="panel detail-panel">
        <div className="panel-header">
          <h3>Selected relationship</h3>
          <span className="status-chip muted">modeled</span>
        </div>
        {selectedNode ? (
          <>
            <h4>{selectedNode.label}</h4>
            <p><strong>Type:</strong> {selectedNode.type}</p>
            <p><strong>Relevant policies:</strong> {currentAnalysis?.policies.filter((policy) => policy.name.includes(selectedNode.label) || policy.selector.includes(selectedNode.label)).map((policy) => policy.name).join(', ') || 'No direct policy matches'}</p>
            <p><strong>Modeled permissions:</strong> {currentAnalysis?.policies.filter((policy) => policy.namespace === selectedNode.label || policy.name.includes(selectedNode.label)).map((policy) => `${policy.policyType} ${policy.selector}`).join('; ') || 'No explicit permissions modeled'}</p>
            <p><strong>Uncertainty:</strong> {currentAnalysis?.findings.filter((finding) => finding.message.includes(selectedNode.label)).length ? 'Some policy impact remains inferred from manifest selectors.' : 'No additional uncertainty flagged for this node.'}</p>
          </>
        ) : <div className="empty-state">Select a node to inspect details.</div>}
      </div>
    </div>
  )

  const renderPolicies = () => (
    <div className="panel">
      <div className="panel-header">
        <h3>Policy editor</h3>
        <button className="primary" onClick={() => void analyzeManifest(manifestText, 'manual-proposed')}>Analyze proposed manifest</button>
      </div>
      <textarea className="manifest-editor" value={manifestText} onChange={(event) => setManifestText(event.target.value)} />
      <div className="policy-list">
        {(currentAnalysis?.policies ?? []).map((policy) => (
          <article key={`${policy.namespace}/${policy.name}`} className="policy-card">
            <h4>{policy.namespace}/{policy.name}</h4>
            <p><strong>Type:</strong> {policy.policyType}</p>
            <p><strong>Selector:</strong> {policy.selector || '(empty selector; broad match)'}</p>
            <p><strong>Rules:</strong> {policy.ingressRules} ingress / {policy.egressRules} egress</p>
          </article>
        ))}
      </div>
    </div>
  )

  const renderResults = () => (
    <div className="panel">
      <div className="panel-header">
        <h3>Analysis results</h3>
        <button className="primary" onClick={() => void createSnapshot()}>Save snapshot</button>
      </div>
      <div className="result-grid">
        {currentAnalysis ? (
          <>
            <div><strong>Path:</strong> {currentAnalysis.path}</div>
            <div><strong>Generated:</strong> {currentAnalysis.generatedAt}</div>
            <div><strong>Policies:</strong> {currentAnalysis.summary.totalPolicies}</div>
            <div><strong>Findings:</strong> {currentAnalysis.summary.findingCount}</div>
          </>
        ) : <div className="empty-state">No analysis is available.</div>}
      </div>
    </div>
  )

  const renderFindings = () => (
    <div className="panel">
      <div className="panel-header">
        <h3>Finding explanations</h3>
      </div>
      <div className="finding-detail-list">
        {currentFindingList.map((finding) => (
          <button key={finding.id} className="finding-card" onClick={() => void explainFinding(finding.id)}>
            <strong>{finding.id}</strong>
            <span>{finding.message}</span>
            <small>{finding.severity}</small>
          </button>
        ))}
      </div>
      {state.selectedFinding && (
        <div className="detail-card">
          <h4>{state.selectedFinding.findingId}</h4>
          <p><strong>Status:</strong> {state.selectedFinding.trafficStatus}</p>
          <p><strong>Reason:</strong> {state.selectedFinding.reason}</p>
          <p><strong>Applicable policies:</strong> {(state.selectedFinding.applicablePolicies ?? []).join(', ') || 'Not available'}</p>
          <p><strong>Assumptions:</strong> {(state.selectedFinding.assumptions ?? []).join('; ') || 'None documented'}</p>
        </div>
      )}
    </div>
  )

  const renderComparison = () => (
    <div className="panel">
      <div className="panel-header">
        <h3>Policy comparison</h3>
      </div>
      {state.diff ? (
        <div className="comparison-grid">
          <div><strong>Added:</strong> {state.diff.added.length ? state.diff.added.join(', ') : 'None'}</div>
          <div><strong>Removed:</strong> {state.diff.removed.length ? state.diff.removed.join(', ') : 'None'}</div>
          <div>
            <strong>Modified policies:</strong>
            {state.diff.modified.length ? (
              <ul>
                {state.diff.modified.map((entry) => (
                  <li key={entry.key}>
                    <strong>{entry.key}</strong>
                    <span>{entry.reason}</span>
                    <small>Changed fields: {entry.changedFields.join(', ') || 'Not reported'}</small>
                    <details>
                      <summary>Exact policy field delta</summary>
                      {entry.fieldChanges.map((change) => (
                        <div key={change.field}>
                          <strong>{change.field}</strong>
                          <pre>Before: {JSON.stringify(change.before, null, 2) ?? 'null'}</pre>
                          <pre>After: {JSON.stringify(change.after, null, 2) ?? 'null'}</pre>
                        </div>
                      ))}
                    </details>
                  </li>
                ))}
              </ul>
            ) : <span>None</span>}
          </div>
        </div>
      ) : <div className="empty-state">No diff is available yet.</div>}
    </div>
  )

  const renderSnapshots = () => (
    <div className="panel">
      <div className="panel-header">
        <h3>Snapshots</h3>
      </div>
      <ul className="list-panel">
        {state.snapshots.length === 0 ? <li className="empty-state">No snapshots were stored.</li> : state.snapshots.map((snapshot) => (
          <li key={snapshot.id} className="list-item">
            <strong>{snapshot.name}</strong>
            <span>{snapshot.id}</span>
            <small>{snapshot.generatedAt || snapshot.path}</small>
          </li>
        ))}
      </ul>
    </div>
  )

  const renderVerification = () => (
    <div className="panel">
      <div className="panel-header">
        <h3>Verification</h3>
        <span className={`status-chip ${state.verificationEnabled ? 'good' : 'muted'}`}>{state.verificationEnabled ? 'provider configured' : 'disabled'}</span>
      </div>
      <p>Modeled policy predictions and runtime observations are separate. A connection failure alone does not prove NetworkPolicy causation.</p>
      {!state.verificationEnabled && <div className="empty-state">No runtime probe provider is configured. No probe is executed and no evidence is fabricated.</div>}
      {state.verificationEvidence.length === 0 ? (
        <div className="empty-state">No runtime evidence has been recorded.</div>
      ) : (
        <ul className="list-panel">
          {state.verificationEvidence.map((evidence) => (
            <li key={evidence.id} className="list-item">
              <strong>{evidence.state} · {evidence.provenance}</strong>
              <span>{evidence.sourceNamespace}/{evidence.sourceWorkload} → {evidence.destination}:{evidence.port}/{evidence.protocol}</span>
              <small>{evidence.method} · {new Date(evidence.attemptedAt).toLocaleString()} · {evidence.outcome} · {(evidence.duration / 1_000_000).toFixed(1)} ms</small>
              {(evidence.cluster || evidence.context) && <small>Cluster/context: {[evidence.cluster, evidence.context].filter(Boolean).join(' / ')}</small>}
              <small>Model comparison: {evidence.modelComparison}. {evidence.limitations.join(' ')}</small>
              {evidence.errorClass && <small>Error classification: {evidence.errorClass}</small>}
            </li>
          ))}
        </ul>
      )}
    </div>
  )

  const renderReports = () => (
    <div className="panel">
      <div className="panel-header">
        <h3>Report</h3>
      </div>
      {state.report ? (
        <pre className="json-box">{JSON.stringify(state.report, null, 2)}</pre>
      ) : <div className="empty-state">No report is available.</div>}
    </div>
  )

  const renderSettings = () => (
    <div className="panel">
      <div className="panel-header">
        <h3>Settings</h3>
      </div>
      <div className="settings-grid">
        <label>API base URL<input value={API_BASE} readOnly /></label>
        <label>Analysis path<input value={analysisPath} onChange={(event) => setAnalysisPath(event.target.value)} /></label>
        <label>Mode <input value={state.capabilities?.kubernetes?.offline ? 'offline-only' : 'service-ready'} readOnly /></label>
      </div>
    </div>
  )

  if (state.loading) return <div className="shell"><div className="panel">Loading PolicyChangeImpact…</div></div>
  if (state.error) return <div className="shell"><div className="panel error-panel"><h3>Disconnected</h3><p>{state.error}</p><button className="primary" onClick={() => void loadData()}>Retry</button></div></div>

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand-wrap">
          <div className="brand-mark">PCI</div>
          <div>
            <p className="eyebrow">Kubernetes policy impact</p>
            <h1>PolicyChangeImpact</h1>
          </div>
        </div>
        <nav className="nav">
          {navItems.map((item) => (
            <button key={item.key} onClick={() => setRoute(item.key)} className={`nav-item ${route === item.key ? 'active' : ''}`}>
              {item.label}
            </button>
          ))}
        </nav>
      </aside>
      <main className="main-panel">
        {route === 'dashboard' && renderDashboard()}
        {route === 'topology' && renderTopology()}
        {route === 'policies' && renderPolicies()}
        {route === 'results' && renderResults()}
        {route === 'findings' && renderFindings()}
        {route === 'comparison' && renderComparison()}
        {route === 'snapshots' && renderSnapshots()}
        {route === 'verification' && renderVerification()}
        {route === 'reports' && renderReports()}
        {route === 'settings' && renderSettings()}
      </main>
    </div>
  )
}

export default App
