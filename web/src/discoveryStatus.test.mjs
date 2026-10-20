/**
 * Lightweight assertion suite for discovery status labels (no test runner deps).
 * Run: node web/src/discoveryStatus.test.mjs
 */

function discoveryStripLabel(discovery, settings) {
  if (discovery && !discovery.offline && discovery.mode === 'cluster') {
    const modeHint =
      settings?.resolvedMode === 'in-cluster' || settings?.mode === 'in-cluster' ? 'in-cluster' : null
    const context =
      discovery.cluster?.context ||
      settings?.resolvedContext ||
      settings?.context ||
      modeHint ||
      'cluster'
    const complete = discovery.complete ? 'complete' : 'partial'
    return { label: `${context} · ${complete}`, tone: discovery.complete ? 'success' : 'warning' }
  }
  if (discovery?.status === 'not-configured' || settings?.configured === false) {
    return { label: 'not configured', tone: 'muted' }
  }
  if (discovery?.offline) {
    return { label: discovery.status || 'offline', tone: discovery.status === 'permission-denied' ? 'danger' : 'warning' }
  }
  if (settings?.error) {
    return { label: 'error', tone: 'danger' }
  }
  return { label: 'unknown', tone: 'muted' }
}

function assert(cond, msg) {
  if (!cond) throw new Error(msg)
}

assert(discoveryStripLabel(null, null).label === 'unknown', 'unknown when no data')
assert(discoveryStripLabel(null, { configured: false }).label === 'not configured', 'not configured')
assert(
  discoveryStripLabel({ mode: 'cluster', offline: true, status: 'permission-denied' }, null).tone === 'danger',
  'permission denied tone',
)
assert(
  discoveryStripLabel(
    { mode: 'cluster', offline: false, complete: true, cluster: { context: 'kind-kind' } },
    { resolvedMode: 'kubeconfig' },
  ).label === 'kind-kind · complete',
  'connected label',
)
assert(
  discoveryStripLabel({ mode: 'cluster', offline: false, complete: true }, { resolvedMode: 'in-cluster' }).label ===
    'in-cluster · complete',
  'in-cluster label',
)

console.log('discoveryStatus.test.mjs: ok')
