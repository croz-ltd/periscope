import { Table, Thead, Tbody, Tr, Th, Td } from '@patternfly/react-table'
import {
  ClipboardCopy,
  Content,
  Divider,
  EmptyState,
  EmptyStateBody,
  Title,
} from '@patternfly/react-core'
import { CubesIcon } from '@patternfly/react-icons'
import type { Row } from './api'
import { AddClusterButton } from './AddCluster'

// Distinct components (unique by key), sorted by display name, the reference of
// keys to use when writing custom groups.
function distinctByKey(rows: Row[]): Row[] {
  const byKey = new Map<string, Row>()
  for (const r of rows) if (!byKey.has(r.key)) byKey.set(r.key, r)
  return [...byKey.values()].sort((a, b) =>
    a.name.toLowerCase().localeCompare(b.name.toLowerCase()),
  )
}

export function Docs({ rows, onAddCluster }: { rows: Row[]; onAddCluster: () => void }) {
  const components = distinctByKey(rows)

  return (
    <>
      <section className="cc-doc-section">
        <Title headingLevel="h2" className="cc-doc-title">
          Add a cluster
        </Title>
        <Content component="p" className="cc-prose">
          A cluster joins by giving this hub a read-only token. The manifests that create it are
          served by the hub itself, so it takes two commands and no chart. The wizard walks them,
          filled in with this hub's address, namespace and label.
        </Content>
        <AddClusterButton onClick={onAddCluster} />
      </section>

      <Divider className="cc-doc-divider" />

      <section className="cc-doc-section">
        <Title headingLevel="h2" className="cc-doc-title">
          Component reference
        </Title>
        <Content component="p" className="cc-prose">
          These are the component <strong>keys</strong> discovered across your clusters. List them
          in the <code>periscope-groups</code> ConfigMap (<code>groups.yaml</code>) to build custom
          matrix groups. Changes are picked up on the next Refresh.
        </Content>

      {components.length === 0 ? (
        <EmptyState titleText="No components yet" headingLevel="h2" icon={CubesIcon}>
          <EmptyStateBody>Once clusters are scraped, their components appear here.</EmptyStateBody>
        </EmptyState>
      ) : (
        <Table aria-label="Component reference" variant="compact" borders>
          <Thead>
            <Tr>
              <Th>Display name</Th>
              <Th>Key</Th>
              <Th>Kind</Th>
              <Th>Group</Th>
            </Tr>
          </Thead>
          <Tbody>
            {components.map((r) => (
              <Tr key={r.key}>
                <Td dataLabel="Display name">{r.name}</Td>
                <Td dataLabel="Key">
                  <ClipboardCopy
                    isReadOnly
                    hoverTip="Copy"
                    clickTip="Copied"
                    variant="inline-compact"
                  >
                    {r.key}
                  </ClipboardCopy>
                </Td>
                <Td dataLabel="Kind">{r.kind}</Td>
                <Td dataLabel="Group">{r.group}</Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      )}
      </section>
    </>
  )
}
