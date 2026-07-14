import { Table, Thead, Tbody, Tr, Th, Td } from '@patternfly/react-table'
import { Tooltip, Label, Icon } from '@patternfly/react-core'
import { ExclamationTriangleIcon } from '@patternfly/react-icons'
import type { Matrix, Row } from './api'
import { cellClass, cellText, cellTooltip } from './cells'

// Fixed display order of row groups; unexpected groups are appended last.
const GROUP_ORDER = ['OpenShift', 'Node', 'Certificate', 'OpenShift Virtualization', 'Operators']

function groupRows(rows: Row[]): { title: string; rows: Row[] }[] {
  const byGroup = new Map<string, Row[]>()
  for (const r of rows) {
    const g = r.group || 'Operators'
    const list = byGroup.get(g)
    if (list) list.push(r)
    else byGroup.set(g, [r])
  }
  const known = GROUP_ORDER.filter((g) => byGroup.has(g))
  const extra = [...byGroup.keys()].filter((g) => !GROUP_ORDER.includes(g)).sort()
  return [...known, ...extra].map((title) => ({
    title,
    rows: byGroup
      .get(title)!
      .slice()
      .sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase())),
  }))
}

export function MatrixTable({ matrix }: { matrix: Matrix }) {
  const { clusters, rows } = matrix
  const groups = groupRows(rows)
  const totalCols = 2 + clusters.length

  return (
    <Table aria-label="Cluster version drift matrix" variant="compact" borders gridBreakPoint="">
      <Thead>
        <Tr>
          <Th>Component</Th>
          <Th>Reference</Th>
          {clusters.map((c) => (
            <Th key={c.name} className="cc-col-head">
              <div className="cc-col-head-inner">
                <span>{c.name}</span>
                {c.stale && (
                  <Tooltip content={`Stale — last scraped ${new Date(c.time).toLocaleString()}`}>
                    <Label color="orange" isCompact>
                      stale
                    </Label>
                  </Tooltip>
                )}
                {c.error && (
                  <Tooltip content={c.error}>
                    <Icon status="warning" isInline>
                      <ExclamationTriangleIcon />
                    </Icon>
                  </Tooltip>
                )}
              </div>
            </Th>
          ))}
        </Tr>
      </Thead>
      {groups.map((g) => (
        <Tbody key={g.title}>
          <Tr className="cc-group-row">
            <Th scope="rowgroup" colSpan={totalCols} className="cc-group-header">
              {g.title}
            </Th>
          </Tr>
          {g.rows.map((row) => (
            <Tr key={row.key}>
              <Td dataLabel="Component">
                <div className="cc-component-name">{row.name}</div>
                <div className="cc-component-kind">{row.kind}</div>
              </Td>
              <Td dataLabel="Reference" className="cc-leader-version">
                {row.compare === 'expiry' ? '' : row.leader || '—'}
              </Td>
              {clusters.map((c) => {
                const cell = row.cells[c.name]
                const tip = cellTooltip(cell, row.leader)
                const content = <span>{cellText(cell)}</span>
                return (
                  <Td key={c.name} dataLabel={c.name} className={cellClass(cell)}>
                    {tip ? <Tooltip content={<div className="cc-tip">{tip}</div>}>{content}</Tooltip> : content}
                  </Td>
                )
              })}
            </Tr>
          ))}
        </Tbody>
      ))}
    </Table>
  )
}
