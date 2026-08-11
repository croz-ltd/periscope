// Which cluster columns this browser leaves out of the matrix. A fleet wider
// than the screen is a reading problem, not a fleet problem, so the choice lives
// in localStorage and is never sent to the server: the reference version, the
// exports and the metrics keep covering every joined cluster, and a colleague
// opening the same URL still sees all of them.

const KEY = 'cc.hiddenClusters'

export function getHiddenClusters(): string[] {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter((v): v is string => typeof v === 'string')
  } catch {
    // A preference we cannot read must not hide anything: showing the whole
    // fleet is the honest failure, showing nothing is not.
    return []
  }
}

export function saveHiddenClusters(names: string[]): void {
  try {
    if (names.length === 0) localStorage.removeItem(KEY)
    else localStorage.setItem(KEY, JSON.stringify(names))
  } catch {
    // Private-mode browsers can refuse to store; the session still works.
  }
}

// visibleClusters drops the hidden columns. Names of clusters that have since
// left the fleet stay in the preference harmlessly, so unjoining and rejoining a
// cluster keeps the choice the reader made.
export function visibleClusters<T extends { name: string }>(clusters: T[], hidden: string[]): T[] {
  if (hidden.length === 0) return clusters
  const set = new Set(hidden)
  return clusters.filter((c) => !set.has(c.name))
}
