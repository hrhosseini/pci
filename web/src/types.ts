export type Route = 'overview' | 'topology' | 'policies' | 'compare' | 'reports'

export type PolicySummary = {
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

export type Finding = {
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

export type Analysis = {
  id?: string
  path: string
  generatedAt: string
  summary: { totalPolicies: number; defaultDeny: number; findingCount: number }
  policies: PolicySummary[]
  findings: Finding[]
}

export type GraphNode = { id: string; label: string; type: string; x?: number; y?: number }
export type GraphEdge = { id: string; source: string; target: string; label: string }
export type Graph = { nodes: GraphNode[]; edges: GraphEdge[] }

export type PolicyFieldChange = { field: string; before: unknown; after: unknown }
export type DiffReport = {
  added: string[]
  removed: string[]
  modified: Array<{
    key: string
    old: string
    new: string
    reason: string
    changedFields: string[]
    fieldChanges: PolicyFieldChange[]
  }>
  findings: Finding[]
}

export type Snapshot = {
  id: string
  name: string
  path: string
  generatedAt: string
  summary: Analysis['summary']
  policies: PolicySummary[]
  findings: Finding[]
}

export type Explanation = {
  findingId: string
  trafficStatus: string
  summary: string
  direction?: string
  applicablePolicies?: string[]
  reason: string
  assumptions?: string[]
  unsupported?: string[]
}

export type Report = {
  analysisId?: string
  generatedAt?: string
  path?: string
  policyCount?: number
  findingCount?: number
  findings?: Finding[]
  graph?: Graph
  recommendation?: string
  summary?: Analysis['summary']
}

export type VerificationEvidence = {
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
  timeoutMillis?: number
  method: string
  errorClass?: string
  cluster?: string
  context?: string
  cni?: string
  analysisId?: string
  snapshotId?: string
  discoveryComplete?: boolean
  discoveryCollectedAt?: string
  limitations: string[]
  provenance: string
  modelComparison: string
}

export type HealthResponse = {
  ok: boolean
  service?: string
  version?: string
  database?: { sqlite: boolean }
}

export type KubernetesSettings = {
  mode?: string
  resolvedMode?: string
  source?: string
  kubeconfigPath?: string
  resolvedKubeconfigPath?: string
  context?: string
  resolvedContext?: string
  namespace?: string
  allNamespaces?: boolean
  configured?: boolean
  readOnly?: boolean
  inClusterEnv?: boolean
  error?: string
}

export type CapabilitiesResponse = {
  capabilities?: string[]
  status?: string
  samplePaths?: {
    base?: string
    proposed?: string
    baseAvailable?: boolean
    proposedAvailable?: boolean
  }
  kubernetes?: {
    mode: string
    status: string
    offline: boolean
    warnings?: string[]
    context?: string
    cluster?: string
    namespaces?: string[]
    pods?: string[]
    policies?: string[]
    errors?: string[]
  }
  kubernetesSettings?: KubernetesSettings
  verification?: {
    enabled: boolean
    provider?: string
    runtimeProbeImplemented: boolean
  }
}

export type ClusterDiscoveryResponse = {
  id?: string
  name?: string
  mode: string
  status: string
  offline: boolean
  complete: boolean
  collectedAt?: string
  namespace?: string
  namespaces?: string[]
  namespaceLabels?: Record<string, Record<string, string>>
  workloads?: Array<{
    namespace: string
    name: string
    kind: string
    labels?: Record<string, string>
    selector?: string
    hostNetwork?: boolean
  }>
  networkPolicies?: Array<{
    namespace: string
    name: string
    podSelector?: string
    policyTypes?: string[]
    ingressRules: number
    egressRules: number
  }>
  incomplete?: string[]
  warnings?: string[]
  errors?: string[]
  cluster?: {
    host?: string
    context?: string
    namespace?: string
    allNamespaces?: boolean
  }
  kubernetesSettings?: KubernetesSettings
}

export type ApiError = {
  code?: string
  message: string
  status?: number
  detail?: string
}
