import { Table, Thead, Tbody, Tr, Th, Td } from '@patternfly/react-table'
import { Tooltip, Label, Icon } from '@patternfly/react-core'
import { ExclamationTriangleIcon } from '@patternfly/react-icons'
import type { Matrix, Row } from './api'
import { cellClass, cellText, cellTooltip } from './cells'

export function MatrixTable({ matrix }: { matrix: Matrix }) {
  const { clusters, rows, groups } = matrix
  const totalCols = 2 + clusters.length

  // The backend now defines sections via matrix.groups; a key may appear in
  // several groups (rendered under each). Look rows up by key, in group order.
  const byKey = new Map<string, Row>()
  for (const r of rows) byKey.set(r.key, r)

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
      {groups.map((g, gi) => (
        <Tbody key={`${g.title}-${gi}`}>
          <Tr className="cc-group-row">
            <Th scope="rowgroup" colSpan={totalCols} className="cc-group-header">
              {g.title}
            </Th>
          </Tr>
          {g.keys.map((key) => {
            const row = byKey.get(key)
            if (!row) return null
            return (
              <Tr key={`${g.title}-${gi}-${key}`}>
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
            )
          })}
        </Tbody>
      ))}
    </Table>
  )
}
