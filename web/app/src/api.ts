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
  // info compare: the value is shown as-is, with no drift judgement
  | 'info'

export type CompareKind = 'version' | 'match' | 'expiry' | 'info'

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
  label?: string // console banner text, shown instead of name
  color?: string
  bgColor?: string
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
  at?: string // set when this is history rather than the live fleet
}

// fetchMatrix loads the live matrix, or the matrix as it stood at `at`.
export async function fetchMatrix(at?: string): Promise<Matrix> {
  const url = at ? `/api/matrix?at=${encodeURIComponent(at)}` : '/api/matrix'
  const res = await fetch(url, { headers: { Accept: 'application/json' } })
  if (!res.ok) throw new Error(`GET ${url} failed: ${res.status} ${res.statusText}`)
  return (await res.json()) as Matrix
}

export type ChangeKind = 'added' | 'removed' | 'updated' | 'joined' | 'unreachable' | 'recovered'

export interface Change {
  time: string
  cluster: string
  kind: ChangeKind
  key?: string
  name?: string
  group?: string
  compare?: CompareKind
  from?: string
  to?: string
}

export interface ChangeDay {
  date: string // YYYY-MM-DD in the browser's zone
  count: number
  clusters: number
}

export interface ChangeCalendar {
  days: ChangeDay[]
  first?: string // oldest snapshot on record, bounds the calendar
  last?: string
}

export interface ChangeQuery {
  from?: Date
  to?: Date
  cluster?: string
  limit?: number
}

export async function fetchChanges(q: ChangeQuery = {}): Promise<Change[]> {
  const params = new URLSearchParams()
  if (q.from) params.set('from', q.from.toISOString())
  if (q.to) params.set('to', q.to.toISOString())
  if (q.cluster) params.set('cluster', q.cluster)
  if (q.limit) params.set('limit', String(q.limit))

  const res = await fetch(`/api/changes?${params}`, { headers: { Accept: 'application/json' } })
  if (!res.ok) throw new Error(`GET /api/changes failed: ${res.status} ${res.statusText}`)
  return ((await res.json()) as { changes: Change[] }).changes ?? []
}

// The calendar buckets by day, so the server is told which day the reader is in
// rather than assuming UTC. getTimezoneOffset counts west of UTC, hence the flip.
export async function fetchChangeCalendar(from: Date, to: Date): Promise<ChangeCalendar> {
  const params = new URLSearchParams({
    from: from.toISOString(),
    to: to.toISOString(),
    tzOffset: String(-new Date().getTimezoneOffset()),
  })
  const res = await fetch(`/api/changes/calendar?${params}`, { headers: { Accept: 'application/json' } })
  if (!res.ok) throw new Error(`GET /api/changes/calendar failed: ${res.status} ${res.statusText}`)
  return (await res.json()) as ChangeCalendar
}

export async function triggerRefresh(): Promise<void> {
  const res = await fetch('/api/refresh', { method: 'POST' })
  if (!res.ok && res.status !== 202) {
    throw new Error(`POST /api/refresh failed: ${res.status}`)
  }
}

// fetchVersion returns the version stamped into the server binary, or "" when
// the endpoint is unavailable (older server, or the UI served standalone).
export async function fetchVersion(): Promise<string> {
  try {
    const res = await fetch('/api/version', { headers: { Accept: 'application/json' } })
    if (!res.ok) return ''
    const body = (await res.json()) as { version?: string }
    return body.version ?? ''
  } catch {
    return ''
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
