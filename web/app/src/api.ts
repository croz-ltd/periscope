export type CellState =
  // version compare
  | 'leader'
  | 'behind'
  | 'unknown'
  | 'not_installed'
  // match compare
  | 'match'
  | 'mismatch'
  // expiry compare
  | 'expiry_ok'
  | 'expiry_warn'
  | 'expiry_crit'

export type CompareKind = 'version' | 'match' | 'expiry'

export interface Cell {
  cluster: string
  version?: string
  state: CellState
  severity: number // version: gap score; expiry: days remaining (may be negative)
  gapKind?: string
  namespace?: string
  extra?: Record<string, string>
}

export interface Row {
  key: string
  name: string
  group: string
  compare: CompareKind
  kind: string
  leader: string // reference value: fleet-max (version), common value (match), or "" (expiry)
  cells: Record<string, Cell>
}

export interface ClusterInfo {
  name: string
  time: string
  ok: boolean
  error?: string
  stale: boolean
}

export interface MatrixGroup {
  title: string
  keys: string[] // component keys in render order; a key may appear in several groups
}

export interface Page {
  id: string // e.g. "compare" | "statistics"
  title: string
  groups: MatrixGroup[]
}

export interface Matrix {
  clusters: ClusterInfo[]
  rows: Row[]
  pages: Page[]
  warning?: string // set when custom grouping was ignored (e.g. bad ConfigMap)
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

export interface User {
  user: string
  email: string
}

// fetchUser returns the signed-in user (empty when running without oauth-proxy,
// or when the endpoint is unavailable). Never throws; an empty user hides the
// user area entirely.
export async function fetchUser(): Promise<User> {
  try {
    const res = await fetch('/api/user', { headers: { Accept: 'application/json' } })
    if (!res.ok) return { user: '', email: '' }
    return (await res.json()) as User
  } catch {
    return { user: '', email: '' }
  }
}
