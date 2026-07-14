import { Table, Thead, Tbody, Tr, Th, Td } from '@patternfly/react-table'
import { Tooltip, Label, Icon } from '@patternfly/react-core'
import { ExclamationTriangleIcon } from '@patternfly/react-icons'
import type { Matrix, Row } from './api'
import { cellClass, cellText, cellTooltip } from './cells'

// Ordered row groups. "Operators" is the catch-all (operator + csi + anything else).
const GROUPS: { title: string; match: (r: Row) => boolean }[] = [
  { title: 'OpenShift', match: (r) => r.kind === 'openshift' },
  { title: 'Node', match: (r) => r.kind === 'nodes' },
  { title: 'Operators', match: () => true },
]

function groupRows(rows: Row[]): { title: string; rows: Row[] }[] {
  const remaining = [...rows]
  const out: { title: string; rows: Row[] }[] = []
  for (const g of GROUPS) {
    const picked: Row[] = []
    for (let i = remaining.length - 1; i >= 0; i--) {
      if (g.match(remaining[i])) {
        picked.push(remaining[i])
        remaining.splice(i, 1)
      }
    }
    picked.sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()))
    if (picked.length) out.push({ title: g.title, rows: picked })
  }
  return out
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
          <Th>Leader</Th>
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
              <Td dataLabel="Leader" className="cc-leader-version">
                {row.leader || '—'}
              </Td>
              {clusters.map((c) => {
                const cell = row.cells[c.name]
                const tip = cellTooltip(cell)
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
