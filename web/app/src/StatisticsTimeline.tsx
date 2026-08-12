import { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Bullseye,
  Card,
  CardBody,
  CardTitle,
  EmptyState,
  EmptyStateBody,
  Flex,
  FlexItem,
  Gallery,
  Spinner,
} from '@patternfly/react-core'
import { ChartLineIcon } from '@patternfly/react-icons'
import { Chart, ChartAxis, ChartGroup, ChartLegend, ChartLine } from '@patternfly/react-charts/victory'
import type { ClusterInfo, MatrixGroup, Row, Timeline, TimelineDays } from './api'
import { fetchTimeline } from './api'
import type { RowTimeline } from './statsCharts'
import { chartFor, timelineFor } from './statsCharts'

// The Statistics page as history: one line per cluster, one card per countable
// row, over a window the reader picks.
//
// The bar charts answer which cluster carries the fleet today. These answer what
// changed: nodes added last week, virtual machines piling up, a volume count
// climbing since Tuesday. Both read the same rows, and a row that holds something
// other than a count belongs to neither.

// Axis labels: a one or two day window is read by hour, anything longer by date.
function tickFormatter(days: TimelineDays): (t: Date) => string {
  const hour = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' })
  const date = new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' })
  return days <= 2 ? (t) => hour.format(t) : (t) => date.format(t)
}

function RowLineChart({ chart, days }: { chart: RowTimeline; days: TimelineDays }) {
  const format = tickFormatter(days)
  const maxValue = Math.max(...chart.series.flatMap((s) => s.points.map((p) => p.value)), 1)
  // Room for the plot, plus a legend that wraps to as many rows as the fleet
  // needs. The rows are measured in the same units as the drawing, so the space
  // has to be reserved here: a legend that runs past the viewBox is cut off.
  const legendRows = Math.ceil(chart.series.length / 3)
  const legendHeight = legendRows * 34
  const height = 200 + legendHeight

  return (
    <Chart
      ariaTitle={chart.title}
      width={520}
      height={height}
      padding={{ left: 54, right: 24, top: 12, bottom: 40 + legendHeight }}
      domain={{ y: [0, maxValue * 1.15] }}
      legendPosition="bottom-left"
      legendComponent={
        <ChartLegend data={chart.series.map((s) => ({ name: s.cluster }))} itemsPerRow={3} />
      }
      themeColor="multi-unordered"
      scale={{ x: 'time' }}
    >
      <ChartAxis tickCount={4} tickFormat={format} />
      <ChartAxis dependentAxis showGrid tickCount={4} tickFormat={(t: number) => String(Math.round(t))} />
      <ChartGroup>
        {chart.series.map((series) => (
          <ChartLine
            key={series.cluster}
            name={series.cluster}
            interpolation="stepAfter"
            data={series.points.map((p) => ({ x: p.t, y: p.value }))}
          />
        ))}
      </ChartGroup>
    </Chart>
  )
}

export function StatisticsTimeline({
  groups,
  rows,
  clusters,
  days,
  at,
}: {
  groups: MatrixGroup[]
  rows: Map<string, Row>
  clusters: ClusterInfo[]
  days: TimelineDays
  at: string | null
}) {
  const [timeline, setTimeline] = useState<Timeline | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  // Only the rows a bar chart can draw have a history worth a line, so the same
  // rule picks the keys, and the request asks for exactly those.
  const keys = groups
    .flatMap((g) => g.keys)
    .filter((key, i, all) => all.indexOf(key) === i)
    .filter((key) => {
      const row = rows.get(key)
      return row ? chartFor(row, clusters) !== null : false
    })
  const wanted = keys.join(',')

  const load = useCallback(async () => {
    if (keys.length === 0) {
      setTimeline(null)
      setLoading(false)
      return
    }
    setLoading(true)
    try {
      setError(null)
      setTimeline(await fetchTimeline(wanted.split(','), days, at ?? undefined))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
    // wanted is the key list as one string, so the effect reruns when the set
    // changes rather than on every render that rebuilds the array.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wanted, days, at])

  useEffect(() => {
    void load()
  }, [load])

  if (loading) {
    return (
      <Bullseye style={{ minHeight: '30vh' }}>
        <Spinner aria-label="Loading history" />
      </Bullseye>
    )
  }
  if (error) {
    return (
      <Alert variant="danger" title="Cannot read the history" isInline>
        {error}
      </Alert>
    )
  }

  const visible = new Set(clusters.map((c) => c.name))
  const charts = (timeline?.rows ?? [])
    .map((row) => timelineFor(row, visible))
    .filter((chart): chart is RowTimeline => chart !== null)

  if (charts.length === 0) {
    return (
      <EmptyState titleText="No history to show" headingLevel="h2" icon={ChartLineIcon}>
        <EmptyStateBody>
          {keys.length === 0
            ? 'None of these rows holds a count. Switch back to the table to read them.'
            : 'The store holds no scrapes inside this window. Try a shorter one.'}
        </EmptyStateBody>
      </EmptyState>
    )
  }

  return (
    <>
      {/* A window longer than the stored history is not an error, but a line that
          starts halfway across the card needs explaining. */}
      {timeline?.stale && (
        <Alert
          variant="info"
          isInline
          isPlain
          title="The fleet has less history than this window"
          className="cc-timeline-note"
        >
          Lines start when the first scrape inside the window was recorded.
        </Alert>
      )}
      <Gallery hasGutter minWidths={{ default: '360px', lg: '480px' }}>
        {charts.map((chart) => (
          <Card key={chart.key} isCompact className="cc-chart-card">
            <CardTitle>
              <Flex justifyContent={{ default: 'justifyContentSpaceBetween' }}>
                <FlexItem>{chart.title}</FlexItem>
                {chart.unit && (
                  <FlexItem>
                    <span className="cc-chart-total">{chart.unit}</span>
                  </FlexItem>
                )}
              </Flex>
            </CardTitle>
            <CardBody>
              <RowLineChart chart={chart} days={days} />
            </CardBody>
          </Card>
        ))}
      </Gallery>
    </>
  )
}
