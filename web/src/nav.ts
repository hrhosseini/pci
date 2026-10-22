import type { Route } from './types'

export const NAV_ITEMS: { key: Route; label: string; icon: string; description: string }[] = [
  {
    key: 'overview',
    label: 'Overview',
    icon: 'dashboard',
    description: 'Cluster status and quick actions',
  },
  {
    key: 'topology',
    label: 'Topology',
    icon: 'topology',
    description: 'Modeled policy–workload relationships',
  },
  {
    key: 'policies',
    label: 'Policies',
    icon: 'policies',
    description: 'Live inventory and offline YAML analysis',
  },
  {
    key: 'compare',
    label: 'Compare',
    icon: 'comparison',
    description: 'Diff base vs proposed policies',
  },
  {
    key: 'reports',
    label: 'Reports',
    icon: 'reports',
    description: 'Findings, snapshots, and saved reports',
  },
]

export function severityTone(value: string): 'danger' | 'warning' | 'success' | 'neutral' {
  const level = (value || '').toLowerCase()
  if (level.includes('high') || level.includes('critical')) return 'danger'
  if (level.includes('medium') || level.includes('warn')) return 'warning'
  if (level.includes('low') || level.includes('info')) return 'success'
  return 'neutral'
}

export function severityLabel(value: string): string {
  const level = (value || '').trim()
  return level ? level : 'unspecified'
}
