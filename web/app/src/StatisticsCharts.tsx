import {
  Card,
  CardBody,
  CardTitle,
  EmptyState,
  EmptyStateBody,
  Flex,
  FlexItem,
  Gallery,
  Label,
} from '@patternfly/react-core'
import { ChartBarIcon } from '@patternfly/react-icons'
import { Chart, ChartAxis, ChartBar, ChartGroup, ChartLegend } from '@patternfly/react-charts/victory'
import type { ClusterInfo, MatrixGroup, Row } from './api'
import type { RowChart } from './statsCharts'
import { chartsFor } from './statsCharts'

// The Statistics page as bar charts, one card per row, clusters along the axis.
//
// Counts are the one thing in the matrix that a chart reads better than a table:
// the table answers "what is the number on that cluster", and the chart answers
// "which cluster is carrying the fleet". The two views show the same rows, in the
// same order, filtered by the same search.

// At most six ticks, every one an integer no larger than the biggest bar.
function countTicks(max: number): number[] {
  const step = Math.max(1, Math.ceil(max / 5))
  const ticks: number[] = []
  for (let t = 0; t <= max; t += step) ticks.push(t)
  return ticks
}

// Bars are horizontal because cluster names are words, not dates. Eight of them
// across an x-axis either collide or turn sideways, and both are worse to read
// than a list down the side.
function RowBarChart({ chart }: { chart: RowChart }) {
  const grouped = chart.series.length > 1
  const clusters = chart.series[0].points.length
  // Horizontal bars plot the first value at the bottom, so the order is reversed
  // to read top-down like the table it replaces.
  const order = chart.series[0].points.map((p) => p.cluster).reverse()
  // One row of bars per cluster, plus the axis and, when there are two series,
  // the legend under it. width and height only set the aspect ratio: Victory
  // scales the drawing to the card.
  const height = Math.max(150, clusters * (grouped ? 30 : 22) + (grouped ? 60 : 34))
  const maxValue = Math.max(...chart.series.flatMap((s) => s.points.map((p) => p.value)), 1)
  const longestName = Math.max(...order.map((name) => name.length), 8)

  return (
    <Chart
      ariaTitle={chart.title}
      horizontal
      width={520}
      height={height}
      // Left padding follows the longest cluster name, so a fleet of short names
      // does not leave a third of the card empty.
      padding={{ left: Math.min(170, longestName * 8.5 + 16), right: 46, top: 8, bottom: grouped ? 52 : 30 }}
      domainPadding={{ x: grouped ? [14, 14] : [10, 10] }}
      domain={{ y: [0, maxValue * 1.15] }}
      legendPosition="bottom-left"
      legendComponent={
        grouped ? <ChartLegend data={chart.series.map((s) => ({ name: s.name }))} /> : undefined
      }
      themeColor="multi-unordered"
    >
      <ChartAxis tickValues={order} />
      {/* Counts are whole numbers. Left to itself the axis picks fractional ticks
          and rounding them prints the same label twice ("1 1 2 2"). */}
      <ChartAxis dependentAxis showGrid tickValues={countTicks(maxValue)} />
      <ChartGroup offset={grouped ? 10 : 0} horizontal>
        {chart.series.map((series) => (
          <ChartBar
            key={series.name}
            name={series.name}
            barWidth={grouped ? 8 : 12}
            // The value sits at the end of its own bar. A tooltip hides the
            // numbers from anyone reading a printed page or a screenshot.
            labels={({ datum }: { datum: { y: number } }) => String(datum.y)}
            data={series.points
              .slice()
              .reverse()
              .map((p) => ({ name: series.name, x: p.cluster, y: p.value }))}
          />
        ))}
      </ChartGroup>
    </Chart>
  )
}

export function StatisticsCharts({
  groups,
  rows,
  clusters,
}: {
  groups: MatrixGroup[]
  rows: Map<string, Row>
  clusters: ClusterInfo[]
}) {
  return (
    <>
      {groups.map((group) => {
        const { charts, skipped } = chartsFor(group.keys, rows, clusters)
        if (charts.length === 0) return null
        return (
          <section key={group.title} className="cc-chart-group">
            <Flex
              alignItems={{ default: 'alignItemsCenter' }}
              spaceItems={{ default: 'spaceItemsSm' }}
              className="cc-chart-group-head"
            >
              <FlexItem>
                <span className="cc-chart-group-title">{group.title}</span>
              </FlexItem>
              {/* A row that holds versions or text has no chart. Saying which
                  ones is shorter than leaving the reader to compare the views. */}
              {skipped.length > 0 && (
                <FlexItem>
                  <Label isCompact variant="outline">
                    {skipped.length} row{skipped.length === 1 ? '' : 's'} not countable:{' '}
                    {skipped.join(', ')}
                  </Label>
                </FlexItem>
              )}
            </Flex>
            <Gallery hasGutter minWidths={{ default: '360px', lg: '480px' }}>
              {charts.map((chart) => (
                <Card key={chart.key} isCompact className="cc-chart-card">
                  <CardTitle>
                    <Flex justifyContent={{ default: 'justifyContentSpaceBetween' }}>
                      <FlexItem>{chart.title}</FlexItem>
                      <FlexItem>
                        <span className="cc-chart-total">{chart.total} fleet-wide</span>
                      </FlexItem>
                    </Flex>
                  </CardTitle>
                  <CardBody>
                    <RowBarChart chart={chart} />
                  </CardBody>
                </Card>
              ))}
            </Gallery>
          </section>
        )
      })}
      {groups.every((g) => chartsFor(g.keys, rows, clusters).charts.length === 0) && (
        <EmptyState titleText="Nothing to chart" headingLevel="h2" icon={ChartBarIcon}>
          <EmptyStateBody>
            None of these rows holds a count. Switch back to the table to read them.
          </EmptyStateBody>
        </EmptyState>
      )}
    </>
  )
}
