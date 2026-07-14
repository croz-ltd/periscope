export type CellState = 'leader' | 'behind' | 'unknown' | 'not_installed'

export interface Cell {
  cluster: string
  version?: string
  state: CellState
  severity: number
  gapKind?: string
  namespace?: string
  extra?: Record<string, string>
}

export interface Row {
  key: string
  name: string
  kind: string
  leader: string
  cells: Record<string, Cell>
}

export interface ClusterInfo {
  name: string
  time: string
  ok: boolean
  error?: string
  stale: boolean
}

export interface Matrix {
  clusters: ClusterInfo[]
  rows: Row[]
}

export async function fetchMatrix(): Promise<Matrix> {
  const res = await fetch('/api/matrix', { headers: { Accept: 'application/json' } })
  if (!res.ok) throw new Error(`GET /api/matrix failed: ${res.status} ${res.statusText}`)
  return (await res.json()) as Matrix
}

export async function triggerRefresh(): Promise<void> {
  const res = await fetch('/api/refresh', { method: 'POST' })
  if (!res.ok && res.status !== 202) {
    throw new Error(`POST /api/refresh failed: ${res.status}`)
  }
}
