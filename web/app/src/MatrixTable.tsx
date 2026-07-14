import { Table, Thead, Tbody, Tr, Th, Td } from '@patternfly/react-table'
import { Tooltip, Label, Icon } from '@patternfly/react-core'
import { ExclamationTriangleIcon } from '@patternfly/react-icons'
import type { Matrix } from './api'
import { cellClass, cellText, cellTooltip } from './cells'

export function MatrixTable({ matrix }: { matrix: Matrix }) {
  const { clusters, rows } = matrix

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
      <Tbody>
        {rows.map((row) => (
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
    </Table>
  )
}
