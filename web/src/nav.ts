import type { Route } from './types'

export const NAV_ITEMS: { key: Route; label: string; icon: string; description: string }[] = [
  { key: 'dashboard', label: 'Dashboard', icon: 'dashboard', description: 'Overview of modeled policy impact' },
  { key: 'topology', label: 'Topology', icon: 'topology', description: 'Modeled graph of policies and relationships' },
  { key: 'policies', label: 'Policies', icon: 'policies', description: 'Policy inventory and manifest analysis' },
  { key: 'results', label: 'Results', icon: 'results', description: 'Analysis summary and metadata' },
  { key: 'findings', label: 'Findings', icon: 'findings', description: 'Structured findings with explanations' },
  { key: 'comparison', label: 'Comparison', icon: 'comparison', description: 'Base versus proposed semantic diff' },
  { key: 'snapshots', label: 'Snapshots', icon: 'snapshots', description: 'Saved offline policy snapshots' },
  { key: 'verification', label: 'Verification', icon: 'verification', description: 'Runtime observation evidence' },
  { key: 'reports', label: 'Reports', icon: 'reports', description: 'Review-oriented analysis reports' },
  { key: 'settings', label: 'Settings', icon: 'settings', description: 'API and analysis configuration' },
]

export function severityTone(value: string): 'danger' | 'warning' | 'success' | 'neutral' {
  const level = (value || '').toLowerCase()
  if (level.includes('high') || level.includes('critical')) return 'danger'
  if (level.includes('medium') || level.includes('warn')) return 'warning'
  if (level.includes('low') || level.includes('info')) return 'success'
  return 'neutral'
}
