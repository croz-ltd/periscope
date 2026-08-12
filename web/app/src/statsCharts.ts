import type { ClusterInfo, Row, TimelineRow } from './api'

// Turning Statistics rows into chart series.
//
// A statistics row carries whatever its extractor reported, as a string: a plain
// count ("24"), a pair ("109 PVC / 110 PV"), or something that is not a number at
// all ("4.17.12" for the release a cluster is offered). Only the numeric ones can
// be drawn, and this module decides which those are. It never guesses: a value
// that is not fully numeric is left to the table.

export interface ChartPoint {
  cluster: string // the column label, which is the banner name when there is one
  value: number
}

export interface ChartSeries {
  name: string // legend entry, and the only label when a row has one series
  points: ChartPoint[]
}

export interface RowChart {
  key: string
  title: string
  series: ChartSeries[]
  total: number // fleet sum of the first series, shown next to the title
}

// A value is a count only when the whole string is a number. parseFloat would
// read "4.17.12" as 4.17 and draw a chart of nonsense.
const COUNT = /^\d+$/
const PAIR = /^(\d+)\s*PVC\s*\/\s*(\d+)\s*PV$/

// A value plus its detail map: what a matrix cell and a timeline point have in
// common, and all these rules need.
interface Valued {
  version?: string
  extra?: Record<string, string>
}

function countOf(cell: Valued | undefined): number | null {
  const raw = cell?.version?.trim()
  if (!raw || !COUNT.test(raw)) return null
  return Number(raw)
}

// Volume rows report two numbers in one value. The extractor also puts them in
// extra, so read that first and fall back to the display string.
function pairOf(cell: Valued | undefined): [number, number] | null {
  if (cell?.extra) {
    const pvc = cell.extra.pvc
    const pv = cell.extra.pv
    if (pvc !== undefined && pv !== undefined && COUNT.test(pvc) && COUNT.test(pv)) {
      return [Number(pvc), Number(pv)]
    }
  }
  const match = cell?.version?.trim().match(PAIR)
  return match ? [Number(match[1]), Number(match[2])] : null
}

// The axis is keyed by the joined name, never by the console banner: two clusters
// can publish the same banner text, and a chart merges bars that share a key.
const labelOf = (cluster: ClusterInfo) => cluster.name

// chartFor returns the chart for one row, or null when the row holds nothing a
// bar chart can say. A row is chartable only when every cluster that reports a
// value reports the same shape, so a mixed row never renders half a chart.
export function chartFor(row: Row, clusters: ClusterInfo[]): RowChart | null {
  const counts: ChartPoint[] = []
  const pvcs: ChartPoint[] = []
  const pvs: ChartPoint[] = []

  for (const cluster of clusters) {
    const cell = row.cells[cluster.name]
    if (!cell || cell.state === 'not_installed') continue // absent is not zero
    const pair = pairOf(cell)
    if (pair) {
      pvcs.push({ cluster: labelOf(cluster), value: pair[0] })
      pvs.push({ cluster: labelOf(cluster), value: pair[1] })
      continue
    }
    const count = countOf(cell)
    if (count === null) return null // one unreadable value makes the row a table row
    counts.push({ cluster: labelOf(cluster), value: count })
  }

  const sum = (points: ChartPoint[]) => points.reduce((t, p) => t + p.value, 0)
  if (pvcs.length > 0 && counts.length === 0) {
    return {
      key: row.key,
      title: row.name,
      series: [
        { name: 'PVC', points: pvcs },
        { name: 'PV', points: pvs },
      ],
      total: sum(pvcs),
    }
  }
  if (counts.length > 0 && pvcs.length === 0) {
    return { key: row.key, title: row.name, series: [{ name: row.name, points: counts }], total: sum(counts) }
  }
  return null
}

export interface ChartsView {
  charts: RowChart[]
  skipped: string[] // names of rows no chart can show, so the UI can say so
}

// chartsFor walks the rows in the order the page groups them, so the charts read
// in the same order as the table they replace.
export function chartsFor(keys: string[], byKey: Map<string, Row>, clusters: ClusterInfo[]): ChartsView {
  const charts: RowChart[] = []
  const skipped: string[] = []
  const seen = new Set<string>()
  for (const key of keys) {
    if (seen.has(key)) continue
    seen.add(key)
    const row = byKey.get(key)
    if (!row) continue
    const chart = chartFor(row, clusters)
    if (chart && chart.series[0].points.length > 0) charts.push(chart)
    else skipped.push(row.name)
  }
  return { charts, skipped }
}

// A timeline series ready for a line chart: one line per cluster, oldest point
// first, and only the clusters whose values are countable.
export interface LinePoint {
  t: Date
  value: number
}

export interface LineSeries {
  cluster: string
  points: LinePoint[]
}

export interface RowTimeline {
  key: string
  title: string
  unit: string // "" for a plain count, "PVC" when a pair row is drawn on its claims
  series: LineSeries[]
}

// timelineFor reads one component's history into lines, dropping the clusters the
// reader hid and the values no chart can draw. A pair row is drawn on its claims,
// because eight clusters times two numbers is sixteen lines on one card.
export function timelineFor(row: TimelineRow, visible: Set<string>): RowTimeline | null {
  const series: LineSeries[] = []
  let unit = ''
  for (const s of row.series) {
    if (!visible.has(s.cluster)) continue
    const points: LinePoint[] = []
    for (const p of s.points) {
      const pair = pairOf(p)
      const value = pair ? pair[0] : countOf(p)
      if (value === null) {
        points.length = 0
        break // one unreadable value makes the whole line a guess
      }
      if (pair) unit = 'PVC'
      points.push({ t: new Date(p.t), value })
    }
    if (points.length > 0) series.push({ cluster: s.cluster, points })
  }
  if (series.length === 0) return null
  series.sort((a, b) => a.cluster.localeCompare(b.cluster))
  return { key: row.key, title: row.name, unit, series }
}
