import type { ClusterDiscoveryResponse, Graph, GraphEdge, GraphNode } from './types'

export type DiscoveryGraphMeta = {
  detail?: string
  namespace?: string
  policyTypes?: string[]
  ingressRules?: number
  egressRules?: number
  kind?: string
  labels?: Record<string, string>
}

export type DiscoveryGraphNode = GraphNode & { meta?: DiscoveryGraphMeta }
export type DiscoveryGraphEdge = GraphEdge & { kind?: 'selects' | 'isolates' | 'allows' | 'egress' }
export type DiscoveryGraph = {
  nodes: DiscoveryGraphNode[]
  edges: DiscoveryGraphEdge[]
  focusNamespaces: string[]
}

function parseSelector(selector?: string): Record<string, string> {
  const out: Record<string, string> = {}
  if (!selector?.trim()) return out
  for (const part of selector.split(',')) {
    const trimmed = part.trim()
    if (!trimmed) continue
    const eq = trimmed.indexOf('=')
    if (eq <= 0) continue
    out[trimmed.slice(0, eq).trim()] = trimmed.slice(eq + 1).trim()
  }
  return out
}

function labelsMatch(labels: Record<string, string> | undefined, selector: Record<string, string>): boolean {
  const keys = Object.keys(selector)
  if (keys.length === 0) return true
  if (!labels) return false
  return keys.every((key) => labels[key] === selector[key])
}

const WORKLOAD_KINDS = new Set(['Pod', 'Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet'])

/**
 * Build a visual topology from read-only discovery.
 * Focuses on namespaces that contain NetworkPolicies to keep the canvas readable.
 */
export function buildDiscoveryGraph(
  discovery: ClusterDiscoveryResponse | null | undefined,
  options?: { namespaceFilter?: string },
): DiscoveryGraph {
  if (!discovery || discovery.offline) {
    return { nodes: [], edges: [], focusNamespaces: [] }
  }

  const policies = discovery.networkPolicies ?? []
  const workloads = (discovery.workloads ?? []).filter((item) => WORKLOAD_KINDS.has(item.kind))

  let focusNamespaces = Array.from(new Set(policies.map((policy) => policy.namespace))).sort()
  if (options?.namespaceFilter && options.namespaceFilter !== 'all') {
    focusNamespaces = focusNamespaces.filter((namespace) => namespace === options.namespaceFilter)
  }
  if (focusNamespaces.length === 0 && options?.namespaceFilter && options.namespaceFilter !== 'all') {
    focusNamespaces = [options.namespaceFilter]
  }

  const nodes: DiscoveryGraphNode[] = []
  const edges: DiscoveryGraphEdge[] = []
  const seen = new Set<string>()

  const addNode = (node: DiscoveryGraphNode) => {
    if (seen.has(node.id)) return
    seen.add(node.id)
    nodes.push(node)
  }

  const viewWidth = 920
  const nsCount = Math.max(focusNamespaces.length, 1)

  focusNamespaces.forEach((namespace, nsIndex) => {
    const colWidth = viewWidth / nsCount
    const originX = nsIndex * colWidth + 40
    const nsId = `ns:${namespace}`
    addNode({
      id: nsId,
      label: namespace,
      type: 'namespace',
      x: Math.round(originX + colWidth / 2 - 40),
      y: 36,
      meta: { namespace, detail: 'Namespace scope for discovered NetworkPolicies' },
    })

    const nsPolicies = policies.filter((policy) => policy.namespace === namespace)
    const nsWorkloads = workloads.filter((workload) => workload.namespace === namespace)

    nsPolicies.forEach((policy, policyIndex) => {
      const policyId = `policy:${policy.namespace}/${policy.name}`
      const px = Math.round(originX + 48 + (policyIndex % 3) * 120)
      const py = 120 + Math.floor(policyIndex / 3) * 70
      const types = policy.policyTypes ?? []
      const isolates =
        (types.includes('Ingress') && policy.ingressRules === 0) ||
        (types.includes('Egress') && policy.egressRules === 0)
      addNode({
        id: policyId,
        label: policy.name,
        type: 'policy',
        x: px,
        y: py,
        meta: {
          namespace: policy.namespace,
          policyTypes: types,
          ingressRules: policy.ingressRules,
          egressRules: policy.egressRules,
          detail: isolates
            ? 'Isolating policy (empty rule list for a declared type)'
            : `Selector: ${policy.podSelector || '(all pods in namespace)'}`,
        },
      })
      edges.push({
        id: `${nsId}->${policyId}`,
        source: nsId,
        target: policyId,
        label: 'owns',
        kind: 'allows',
      })

      const selector = parseSelector(policy.podSelector)
      const matched = nsWorkloads.filter((workload) => labelsMatch(workload.labels, selector))
      const targets = matched.length > 0 ? matched : []

      targets.forEach((workload, workloadIndex) => {
        const workloadId = `wl:${workload.namespace}/${workload.kind}/${workload.name}`
        const wx = Math.round(originX + 40 + (workloadIndex % 4) * 100)
        const wy = 300 + Math.floor(workloadIndex / 4) * 78
        addNode({
          id: workloadId,
          label: workload.name.length > 22 ? `${workload.name.slice(0, 20)}…` : workload.name,
          type: 'workload',
          x: wx,
          y: wy,
          meta: {
            namespace: workload.namespace,
            kind: workload.kind,
            labels: workload.labels,
            detail: `${workload.kind} · ${workload.namespace}`,
          },
        })
        const edgeKind =
          isolates && policy.ingressRules === 0 && types.includes('Ingress')
            ? 'isolates'
            : policy.ingressRules > 0
              ? 'allows'
              : policy.egressRules > 0
                ? 'egress'
                : 'selects'
        edges.push({
          id: `${policyId}->${workloadId}`,
          source: policyId,
          target: workloadId,
          label: edgeKind,
          kind: edgeKind,
        })
      })
    })

    // Show unmatched workloads lightly when few exist (demo namespaces).
    if (nsPolicies.length > 0 && nsWorkloads.length > 0 && nsWorkloads.length <= 8) {
      nsWorkloads.forEach((workload, workloadIndex) => {
        const workloadId = `wl:${workload.namespace}/${workload.kind}/${workload.name}`
        if (seen.has(workloadId)) return
        addNode({
          id: workloadId,
          label: workload.name.length > 22 ? `${workload.name.slice(0, 20)}…` : workload.name,
          type: 'workload',
          x: Math.round(originX + 40 + (workloadIndex % 4) * 100),
          y: 300 + Math.floor(workloadIndex / 4) * 78,
          meta: {
            namespace: workload.namespace,
            kind: workload.kind,
            labels: workload.labels,
            detail: 'Workload in policy namespace (not selected by listed policies)',
          },
        })
      })
    }
  })

  // Stable fallback positions
  nodes.forEach((node, index) => {
    if (node.x == null) node.x = 80 + (index % 6) * 130
    if (node.y == null) node.y = 80 + Math.floor(index / 6) * 90
  })

  return { nodes, edges, focusNamespaces }
}

export function discoveryGraphAsGraph(graph: DiscoveryGraph): Graph {
  return {
    nodes: graph.nodes.map(({ id, label, type, x, y }) => ({ id, label, type, x, y })),
    edges: graph.edges.map(({ id, source, target, label }) => ({ id, source, target, label })),
  }
}
