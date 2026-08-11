import type { MatrixGroup, Row } from './api'

// Client-side view helpers for the matrix pages: which components a search shows
// and how many there are. The whole matrix is already in the browser, so
// filtering as you type needs no round trip and no server support.

export function rowsByKey(rows: Row[]): Map<string, Row> {
  const byKey = new Map<string, Row>()
  for (const r of rows) byKey.set(r.key, r)
  return byKey
}

// A component matches on its display name, its stable key or its kind. The key
// is searchable because it is what the grouping ConfigMap, the exports and the
// metrics call the row, so a name found in one of those still finds the row here.
function matches(row: Row, needle: string): boolean {
  return (
    row.name.toLowerCase().includes(needle) ||
    row.key.toLowerCase().includes(needle) ||
    row.kind.toLowerCase().includes(needle)
  )
}

// filterGroups keeps the matching rows and drops the groups left empty, so a
// search never leaves a page of bare section headers behind.
export function filterGroups(
  groups: MatrixGroup[],
  byKey: Map<string, Row>,
  query: string,
): MatrixGroup[] {
  const needle = query.trim().toLowerCase()
  if (!needle) return groups
  const out: MatrixGroup[] = []
  for (const g of groups) {
    const keys = g.keys.filter((k) => {
      const row = byKey.get(k)
      return row ? matches(row, needle) : false
    })
    if (keys.length > 0) out.push({ title: g.title, keys })
  }
  return out
}

// countComponents counts distinct rows, because a key may be listed under
// several groups and counting entries would report more components than exist.
export function countComponents(groups: MatrixGroup[]): number {
  const seen = new Set<string>()
  for (const g of groups) for (const k of g.keys) seen.add(k)
  return seen.size
}
