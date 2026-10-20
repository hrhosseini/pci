import type {
  Analysis,
  CapabilitiesResponse,
  ClusterDiscoveryResponse,
  DiffReport,
  Explanation,
  Graph,
  HealthResponse,
  Report,
  Snapshot,
  VerificationEvidence,
} from './types'

/** Vite proxies `/api` to the Go server in development. Override with VITE_API_BASE. */
export const API_BASE =
  (typeof import.meta !== 'undefined' && import.meta.env?.VITE_API_BASE) || '/api/v1'

export class ApiRequestError extends Error {
  status: number
  code?: string
  detail?: string

  constructor(message: string, status: number, code?: string, detail?: string) {
    super(message)
    this.name = 'ApiRequestError'
    this.status = status
    this.code = code
    this.detail = detail
  }
}

function friendlyHttpMessage(status: number): string {
  if (status === 0) return 'Could not reach the PCI API. Is the server running on loopback?'
  if (status === 404) return 'The requested resource was not found.'
  if (status === 400) return 'The request was rejected as invalid.'
  if (status === 403) return 'The request was not authorized.'
  if (status === 503) return 'The service is unavailable for this operation.'
  if (status >= 500) return 'The API returned a server error.'
  return `Request failed with status ${status}.`
}

export async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  let response: Response
  try {
    response = await fetch(url, {
      ...init,
      headers: {
        Accept: 'application/json',
        ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
        ...(init?.headers || {}),
      },
    })
  } catch {
    throw new ApiRequestError(
      'Could not reach the PCI API. Confirm the Go server is listening on 127.0.0.1 and the Vite proxy (or VITE_API_BASE) is configured.',
      0,
    )
  }

  const text = await response.text().catch(() => '')
  if (!response.ok) {
    let code: string | undefined
    let message = friendlyHttpMessage(response.status)
    let detail: string | undefined
    if (text) {
      try {
        const parsed = JSON.parse(text) as { code?: string; message?: string }
        code = parsed.code
        if (parsed.message) message = parsed.message
        detail = text.length > 400 ? `${text.slice(0, 400)}…` : text
      } catch {
        detail = text.length > 400 ? `${text.slice(0, 400)}…` : text
      }
    }
    throw new ApiRequestError(message, response.status, code, detail)
  }

  if (!text) {
    throw new ApiRequestError('The API returned an empty response.', response.status)
  }
  try {
    return JSON.parse(text) as T
  } catch {
    throw new ApiRequestError('The API returned invalid JSON.', response.status, undefined, text.slice(0, 200))
  }
}

export function getHealth(signal?: AbortSignal) {
  return fetchJSON<HealthResponse>(`${API_BASE}/health`, { signal })
}

export function getCapabilities(signal?: AbortSignal) {
  return fetchJSON<CapabilitiesResponse>(`${API_BASE}/capabilities`, { signal })
}

export function listAnalyses(signal?: AbortSignal) {
  return fetchJSON<{ analyses: Array<{ id: string; kind: string; name?: string; path?: string; createdAt?: string }> }>(
    `${API_BASE}/analyses`,
    { signal },
  )
}

export function getAnalysis(id: string, signal?: AbortSignal) {
  return fetchJSON<Analysis>(`${API_BASE}/analyses/${id}`, { signal })
}

export function createAnalysis(body: { path?: string; manifest?: string; name?: string }, signal?: AbortSignal) {
  return fetchJSON<Analysis>(`${API_BASE}/analyses`, {
    method: 'POST',
    body: JSON.stringify(body),
    signal,
  })
}

export function getGraph(analysisId: string, signal?: AbortSignal) {
  return fetchJSON<Graph>(`${API_BASE}/analyses/${analysisId}/graph`, { signal })
}

export function getFindings(analysisId: string, signal?: AbortSignal) {
  return fetchJSON<{ findings: FindingLike[] }>(`${API_BASE}/analyses/${analysisId}/findings`, { signal })
}

type FindingLike = Analysis['findings'][number]

export function explainFinding(analysisId: string, findingId: string, signal?: AbortSignal) {
  return fetchJSON<Explanation>(`${API_BASE}/analyses/${analysisId}/explain/${findingId}`, { signal })
}

export function postDiff(basePath: string, proposedPath: string, signal?: AbortSignal) {
  return fetchJSON<DiffReport>(`${API_BASE}/diff`, {
    method: 'POST',
    body: JSON.stringify({ basePath, proposedPath }),
    signal,
  })
}

export function listSnapshots(signal?: AbortSignal) {
  return fetchJSON<{ snapshots: Array<{ id: string; name: string; path: string; createdAt: string }> }>(
    `${API_BASE}/snapshots`,
    { signal },
  )
}

export function getSnapshot(id: string, signal?: AbortSignal) {
  return fetchJSON<Snapshot>(`${API_BASE}/snapshots/${id}`, { signal })
}

export function createSnapshot(body: { path?: string; manifest?: string; name?: string }, signal?: AbortSignal) {
  return fetchJSON<Snapshot>(`${API_BASE}/snapshots`, {
    method: 'POST',
    body: JSON.stringify(body),
    signal,
  })
}

export function createReport(body: { path?: string; analysisId?: string; name?: string }, signal?: AbortSignal) {
  return fetchJSON<Report>(`${API_BASE}/reports`, {
    method: 'POST',
    body: JSON.stringify(body),
    signal,
  })
}

export function listVerifications(signal?: AbortSignal) {
  return fetchJSON<{ evidence: VerificationEvidence[]; verificationEnabled: boolean }>(
    `${API_BASE}/verifications`,
    { signal },
  )
}

/** Read-only cluster discovery. Never mutates cluster state. */
export function discoverCluster(
  clusterId = 'local',
  options?: { allNamespaces?: boolean; namespace?: string },
  signal?: AbortSignal,
) {
  const params = new URLSearchParams()
  if (options?.allNamespaces === true) params.set('allNamespaces', 'true')
  if (options?.allNamespaces === false) params.set('allNamespaces', 'false')
  if (options?.namespace) params.set('namespace', options.namespace)
  const query = params.toString()
  const path = `${API_BASE}/clusters/${encodeURIComponent(clusterId)}/discover${query ? `?${query}` : ''}`
  return fetchJSON<ClusterDiscoveryResponse>(path, { signal })
}

export function formatDurationMs(durationNs: number): string {
  if (!Number.isFinite(durationNs)) return '—'
  return `${(durationNs / 1_000_000).toFixed(1)} ms`
}
