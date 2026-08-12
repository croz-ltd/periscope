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
  keys: string[] // component keys in render order; a key can appear in several groups
}

export interface Page {
  id: string // for example "compare" | "statistics"
  title: string
  groups: MatrixGroup[]
}

export interface Matrix {
  clusters: ClusterInfo[]
  rows: Row[]
  pages: Page[]
  warning?: string // set when custom grouping was ignored (for example bad ConfigMap)
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
  counters: number // how many of those were counter updates
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
  includeCounters?: boolean
}

export interface ChangeFeed {
  changes: Change[]
  hiddenCounters: number // counter updates left out, only meaningful when they are excluded
}

// Counters are excluded by the server, not here: the limit must be spent on
// rows that will be shown, or a day of counter churn returns a full page that
// filters down to nothing.
export async function fetchChanges(q: ChangeQuery = {}): Promise<ChangeFeed> {
  const params = new URLSearchParams()
  if (q.from) params.set('from', q.from.toISOString())
  if (q.to) params.set('to', q.to.toISOString())
  if (q.cluster) params.set('cluster', q.cluster)
  if (q.limit) params.set('limit', String(q.limit))
  if (!q.includeCounters) params.set('counters', 'false')

  const res = await fetch(`/api/changes?${params}`, { headers: { Accept: 'application/json' } })
  if (!res.ok) throw new Error(`GET /api/changes failed: ${res.status} ${res.statusText}`)
  const body = (await res.json()) as { changes?: Change[]; hiddenCounters?: number }
  return { changes: body.changes ?? [], hiddenCounters: body.hiddenCounters ?? 0 }
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

export interface TimelinePoint {
  t: string // RFC3339
  version: string
  extra?: Record<string, string>
}

export interface TimelineSeries {
  cluster: string
  points: TimelinePoint[]
}

export interface TimelineRow {
  key: string
  name: string
  series: TimelineSeries[]
}

export interface Timeline {
  from: string
  to: string
  days: number
  step: string
  rows: TimelineRow[]
  stale?: boolean // the window reaches further back than the stored history
}

// The timeframes the server accepts. The step is chosen with the window, so this
// is a fixed set rather than a free number of days.
export const TIMELINE_DAYS = [1, 2, 5, 7, 14, 30] as const
export type TimelineDays = (typeof TIMELINE_DAYS)[number]

// fetchTimeline reads the history of several components in one request. `at`
// bounds the end of the window, so a timeline follows time travel.
export async function fetchTimeline(
  keys: string[],
  days: TimelineDays,
  at?: string,
): Promise<Timeline> {
  const params = new URLSearchParams({ key: keys.join(','), days: String(days) })
  if (at) params.set('at', at)
  const res = await fetch(`/api/timeline?${params}`, { headers: { Accept: 'application/json' } })
  if (!res.ok) throw new Error(`GET /api/timeline failed: ${res.status} ${res.statusText}`)
  return (await res.json()) as Timeline
}

export async function triggerRefresh(): Promise<void> {
  const res = await fetch('/api/refresh', { method: 'POST' })
  if (!res.ok && res.status !== 202) {
    throw new Error(`POST /api/refresh failed: ${res.status}`)
  }
}

// HubInfo is what the hub says about itself: the running build, and the namespace
// and label it reads its cluster Secrets from. The Docs page quotes the last two
// back in the commands that register a cluster.
export interface HubInfo {
  version: string
  namespace: string
  clusterLabel: string
}

// The defaults match the charts, and they are what an older server or a UI served
// on its own falls back to.
const HUB_DEFAULTS: HubInfo = {
  version: '',
  namespace: 'periscope',
  clusterLabel: 'periscope.io/cluster=true',
}

// fetchHubInfo never throws. Every field has a usable default, and a Docs page
// that renders nothing is worse than one quoting the standard namespace.
export async function fetchHubInfo(): Promise<HubInfo> {
  try {
    const res = await fetch('/api/version', { headers: { Accept: 'application/json' } })
    if (!res.ok) return HUB_DEFAULTS
    const body = (await res.json()) as Partial<HubInfo>
    return {
      version: body.version ?? '',
      namespace: body.namespace || HUB_DEFAULTS.namespace,
      clusterLabel: body.clusterLabel || HUB_DEFAULTS.clusterLabel,
    }
  } catch {
    return HUB_DEFAULTS
  }
}

// fetchVersion returns the version stamped into the server binary, or "" when the
// endpoint is unavailable (older server, or the UI served standalone).
export async function fetchVersion(): Promise<string> {
  return (await fetchHubInfo()).version
}

export interface User {
  user: string
  email: string
}

// fetchUser returns the signed-in user (empty when running without oauth-proxy,
// or when the endpoint is unavailable). It never throws. An empty user hides
// the user area entirely.
export async function fetchUser(): Promise<User> {
  try {
    const res = await fetch('/api/user', { headers: { Accept: 'application/json' } })
    if (!res.ok) return { user: '', email: '' }
    return (await res.json()) as User
  } catch {
    return { user: '', email: '' }
  }
}
