import type { ClusterDiscoveryResponse, KubernetesSettings } from './types'

export type DiscoveryTone = 'success' | 'warning' | 'muted' | 'danger'

export function discoveryStripLabel(
  discovery: ClusterDiscoveryResponse | null,
  settings?: KubernetesSettings | null,
): { label: string; tone: DiscoveryTone } {
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
  // Never claim connected when discovery has not run or status is unknown.
  return { label: 'unknown', tone: 'muted' }
}

export function summarizeDiscoveryError(discovery: ClusterDiscoveryResponse | null, fallback?: string): string {
  if (discovery?.warnings?.length) return discovery.warnings.join(' ')
  if (discovery?.errors?.length) return discovery.errors.join(' ')
  if (discovery?.kubernetesSettings?.error) return discovery.kubernetesSettings.error
  return fallback || 'Discovery did not return cluster resources.'
}
