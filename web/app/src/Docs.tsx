import { useEffect, useMemo, useState } from 'react'
import { Table, Thead, Tbody, Tr, Th, Td } from '@patternfly/react-table'
import {
  Alert,
  Button,
  ClipboardCopy,
  Content,
  Divider,
  EmptyState,
  EmptyStateBody,
  FormSelect,
  FormSelectOption,
  SearchInput,
  Tab,
  Tabs,
  TabTitleText,
  Title,
  ToggleGroup,
  ToggleGroupItem,
  Toolbar,
  ToolbarContent,
  ToolbarItem,
} from '@patternfly/react-core'
import { CubesIcon, KeyIcon } from '@patternfly/react-icons'
import type { Row } from './api'
import { fetchHubInfo, fetchIssuedToken, type HubInfo, type IssuedToken } from './api'
import { clientsFor, TOKEN_ENV, type McpClient } from './mcpClients'
import { AddClusterButton, CopyableCommand } from './AddCluster'

// The Docs pages. Each one is about one way of using this hub, and every
// example is filled in with this hub's own address and namespace, so nothing
// has to be edited before it is pasted.

export type DocsPage = 'clusters' | 'components' | 'mcp' | 'api'

export const DOCS_PAGES: { id: DocsPage; title: string; description: string }[] = [
  { id: 'clusters', title: 'Clusters', description: 'Join a cluster, remove one, and purge what it left behind' },
  { id: 'components', title: 'Components', description: 'The component keys discovered across your clusters' },
  { id: 'mcp', title: 'MCP', description: 'Let an AI assistant read the fleet' },
  { id: 'api', title: 'API', description: 'The REST endpoints, for scripts and integrations' },
]

// The hub address as the browser sees it. Behind the route this is the public
// host, which is what a script or an MCP client on another machine uses too.
const hubURL = () => window.location.origin

function useHubInfo(): HubInfo | null {
  const [info, setInfo] = useState<HubInfo | null>(null)
  useEffect(() => {
    void fetchHubInfo().then(setInfo)
  }, [])
  return info
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="cc-doc-section">
      <Title headingLevel="h2" className="cc-doc-title">
        {title}
      </Title>
      {children}
    </section>
  )
}

// --- Clusters ---

export function DocsClusters({ onAddCluster }: { onAddCluster: () => void }) {
  const ns = useHubInfo()?.namespace ?? 'periscope'
  return (
    <>
      <Section title="Add a cluster">
        <Content component="p" className="cc-prose">
          A cluster joins by giving this hub a read-only token. The manifests that create it are
          served by the hub itself, so it takes two commands and no chart. The wizard walks them,
          filled in with this hub's address, namespace and label.
        </Content>
        <AddClusterButton onClick={onAddCluster} />
      </Section>

      <Divider className="cc-doc-divider" />

      <Section title="Remove a cluster">
        <Content component="p" className="cc-prose">
          A cluster leaves the fleet when its credential Secret goes. Nothing scrapes it after that,
          and its history stays, so a cluster taken out for a short time comes back with its past.
        </Content>
        <CopyableCommand label="remove" command={`oc -n ${ns} delete secret <CLUSTER_NAME>`} />
      </Section>

      <Divider className="cc-doc-divider" />

      <Section title="Purge what it left behind">
        <Content component="p" className="cc-prose">
          A removed cluster keeps a greyed-out column. <strong>Actions, Purge stale data</strong>{' '}
          deletes its history. It is not undoable, and it refuses any cluster that still has its
          Secret. Purging needs <code>create</code> on services in namespace <code>{ns}</code>, so
          the action shows only to administrators of that namespace.
        </Content>
        <CopyableCommand
          label="grant-admin"
          command={`oc -n ${ns} policy add-role-to-user admin <USER>`}
        />
      </Section>
    </>
  )
}

// --- Components ---

// Distinct components (unique by key), sorted by display name.
function distinctByKey(rows: Row[]): Row[] {
  const byKey = new Map<string, Row>()
  for (const r of rows) if (!byKey.has(r.key)) byKey.set(r.key, r)
  return [...byKey.values()].sort((a, b) =>
    a.name.toLowerCase().localeCompare(b.name.toLowerCase()),
  )
}

export function DocsComponents({ rows }: { rows: Row[] }) {
  const components = useMemo(() => distinctByKey(rows), [rows])
  const groups = useMemo(() => [...new Set(components.map((r) => r.group))].sort(), [components])
  const [query, setQuery] = useState('')
  const [group, setGroup] = useState('')

  const q = query.trim().toLowerCase()
  const shown = components.filter(
    (r) =>
      (group === '' || r.group === group) &&
      (q === '' || r.key.toLowerCase().includes(q) || r.name.toLowerCase().includes(q)),
  )

  return (
    <Section title="Component keys">
      <Content component="p" className="cc-prose">
        Every fact Periscope collects is a component with a stable <strong>key</strong>. Use the
        key in the <code>periscope-groups</code> ConfigMap (<code>groups.yaml</code>) to build
        custom matrix groups, in <code>/api/timeline?key=</code>, and in the MCP tools.
      </Content>

      {components.length === 0 ? (
        <EmptyState titleText="No components yet" headingLevel="h3" icon={CubesIcon}>
          <EmptyStateBody>Once clusters are scraped, their components appear here.</EmptyStateBody>
        </EmptyState>
      ) : (
        <>
          <Toolbar>
            <ToolbarContent>
              <ToolbarItem>
                <SearchInput
                  aria-label="Find a component"
                  placeholder="Find by name or key"
                  value={query}
                  onChange={(_e, v) => setQuery(v)}
                  onClear={() => setQuery('')}
                />
              </ToolbarItem>
              <ToolbarItem>
                <FormSelect
                  aria-label="Group"
                  value={group}
                  onChange={(_e, v) => setGroup(v)}
                >
                  <FormSelectOption value="" label="All groups" />
                  {groups.map((g) => (
                    <FormSelectOption key={g} value={g} label={g} />
                  ))}
                </FormSelect>
              </ToolbarItem>
              <ToolbarItem variant="pagination">
                {shown.length} of {components.length}
              </ToolbarItem>
            </ToolbarContent>
          </Toolbar>
          <Table aria-label="Component keys" variant="compact" borders>
            <Thead>
              <Tr>
                <Th>Display name</Th>
                <Th>Key</Th>
                <Th>Kind</Th>
                <Th>Group</Th>
              </Tr>
            </Thead>
            <Tbody>
              {shown.map((r) => (
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
        </>
      )}
    </Section>
  )
}

// --- MCP ---

const MCP_TOOLS: [tool: string, answers: string, example: string][] = [
  ['list_clusters', 'The clusters, their last scrape, and whether it worked', 'Which clusters failed to scrape?'],
  ['list_components', 'The component keys, filtered by text or group', 'What do you know about storage?'],
  ['get_component', 'One component on every cluster, with drift state and details', 'Which OpenShift version runs where?'],
  ['get_cluster', 'Everything about one cluster, or one group of it', 'Summarize the capacity of prod-emea.'],
  ['fleet_totals', 'Sum, minimum, maximum and average across the fleet', 'How many vCPUs do all clusters have, and how many are free?'],
  ['find_drift', 'Versions behind, differing settings, certificates close to expiry', 'What is out of line in the fleet?'],
  ['get_history', 'The history of up to 10 components, over 1 to 30 days', 'How did used memory change this week?'],
  ['list_changes', 'The change feed, newest first', 'What changed on prod-us since Monday?'],
  ['refresh', 'Start a scrape now', 'Scrape the fleet again and tell me what changed.'],
]

// The token and how to put it in the environment: step one of connecting.
function TokenStep({ hub, issued }: { hub: HubInfo | null; issued: IssuedToken | null }) {
  if (hub?.auth === 'none') {
    return (
      <Content component="p" className="cc-prose">
        This hub runs with the login off (<code>--auth=none</code>), so a client needs no token.
      </Content>
    )
  }
  const exportLine = `export ${TOKEN_ENV}='${issued?.token ?? '<TOKEN>'}'`
  return (
    <>
      {issued ? (
        <Alert
          isInline
          variant="success"
          title={`Token issued to ${issued.user}, valid until ${new Date(issued.expires).toLocaleString()}`}
          className="cc-doc-alert"
        >
          <Content component="p">
            Copy it now. Periscope does not keep it, and this page does not show it again after a
            reload. The examples below already carry it.
          </Content>
          <ClipboardCopy isReadOnly hoverTip="Copy" clickTip="Copied">
            {issued.token}
          </ClipboardCopy>
        </Alert>
      ) : hub?.mcpTokens ? (
        <>
          <Content component="p" className="cc-prose">
            A token of your own user, valid for 30 days unless this hub sets another lifetime. It
            reads Periscope with your rights and can do nothing else on the cluster.
          </Content>
          <Button component="a" href="/mcp/token" icon={<KeyIcon />}>
            Get an MCP token
          </Button>
        </>
      ) : (
        <Content component="p" className="cc-prose">
          This hub issues no MCP tokens: its chart was installed without a route host, so it has
          no OAuth client for them. Your own OpenShift token works too, until your login expires
          (often after 24 hours): use <code>$(oc whoami -t)</code> as the token below.
        </Content>
      )}
      <Content component="p" className="cc-prose cc-doc-gap">
        Put the token in the environment. Most clients below read it from there, so their
        configuration holds no secret. Add the line to your shell profile to keep it.
      </Content>
      <CopyableCommand label="token-env" command={exportLine} />
    </>
  )
}

function ClientSnippets({ clients }: { clients: McpClient[] }) {
  const [active, setActive] = useState(clients[0].id)
  const [mode, setMode] = useState<'command' | 'config'>('command')
  const client = clients.find((c) => c.id === active) ?? clients[0]
  const snippet = mode === 'command' ? client.command : client.config

  return (
    <>
      <Tabs
        activeKey={active}
        onSelect={(_e, key) => setActive(String(key))}
        aria-label="MCP clients"
        isBox
        className="cc-doc-tabs"
      >
        {clients.map((c) => (
          <Tab key={c.id} eventKey={c.id} title={<TabTitleText>{c.name}</TabTitleText>} />
        ))}
      </Tabs>
      <ToggleGroup aria-label="Command or config file" className="cc-doc-toggle">
        <ToggleGroupItem text="Command" isSelected={mode === 'command'} onChange={() => setMode('command')} />
        <ToggleGroupItem
          text="Config file"
          isSelected={mode === 'config'}
          onChange={() => setMode('config')}
        />
      </ToggleGroup>
      <Content component="p" className="cc-prose cc-doc-gap">
        {mode === 'command' ? (
          <>Run this in a shell where <code>{TOKEN_ENV}</code> is set.</>
        ) : (
          <>
            Add this to <code>{client.config.file}</code>.
          </>
        )}
      </Content>
      <CopyableCommand label={`${client.id}-${mode}`} command={snippet.text} />
      {snippet.note && (
        <Content component="p" className="cc-step-note">
          {snippet.note}
        </Content>
      )}
    </>
  )
}

export function DocsMCP() {
  const hub = useHubInfo()
  const [issued, setIssued] = useState<IssuedToken | null>(null)
  useEffect(() => {
    void fetchIssuedToken().then(setIssued)
  }, [])

  const url = `${hubURL()}/mcp`
  const clients = clientsFor(url, hub?.auth !== 'none', issued?.token ?? '<TOKEN>')

  return (
    <>
      <Section title="Connect an assistant">
        <Content component="p" className="cc-prose">
          This hub is an MCP server at <code>{url}</code>. An assistant connected to it reads the
          same data as these pages, with your rights.
        </Content>
        <Title headingLevel="h3" className="cc-doc-subtitle">
          1. Get a token
        </Title>
        <TokenStep hub={hub} issued={issued} />
        <Title headingLevel="h3" className="cc-doc-subtitle">
          2. Add Periscope to your client
        </Title>
        <ClientSnippets clients={clients} />
      </Section>

      <Divider className="cc-doc-divider" />

      <Section title="Tools">
        <Content component="p" className="cc-prose">
          Every read tool takes <code>at</code> (an RFC3339 time) to answer about the past.{' '}
          <code>fleet_totals</code> does the arithmetic on the hub, so a fleet total comes back
          exact.
        </Content>
        <Table aria-label="MCP tools" variant="compact" borders>
          <Thead>
            <Tr>
              <Th>Tool</Th>
              <Th>Answers</Th>
              <Th>Ask for example</Th>
            </Tr>
          </Thead>
          <Tbody>
            {MCP_TOOLS.map(([tool, answers, example]) => (
              <Tr key={tool}>
                <Td dataLabel="Tool">
                  <code>{tool}</code>
                </Td>
                <Td dataLabel="Answers">{answers}</Td>
                <Td dataLabel="Ask for example">{example}</Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      </Section>

      <Divider className="cc-doc-divider" />

      <Section title="Revoke a token">
        <Content component="p" className="cc-prose">
          A token is an OpenShift OAuth token of your user. To end it before it expires:
        </Content>
        <CopyableCommand
          label="revoke"
          command={`oc delete useroauthaccesstoken ${issued?.name ?? '<TOKEN_NAME>'}`}
        />
        <Content component="p" className="cc-prose cc-doc-gap">
          <code>oc get useroauthaccesstokens</code> lists yours, with the client that issued each:
          Periscope tokens show <code>periscope-mcp</code>. A token also stops working within a
          minute when you lose read access to Periscope.
        </Content>
      </Section>
    </>
  )
}

// --- API ---

const ENDPOINTS: [path: string, purpose: string][] = [
  ['GET /api/matrix', 'The full comparison matrix as JSON. Accepts at.'],
  ['GET /api/changes', 'The change feed, newest first. Accepts from, to, cluster and limit.'],
  ['GET /api/timeline?key=<key>&days=7', 'One series per cluster. days is 1, 2, 5, 7, 14 or 30.'],
  ['GET /api/export.csv, /api/export.json', 'The matrix as a file. Accepts at, page and cluster.'],
  ['POST /api/refresh', 'Start a scrape now.'],
  ['GET /api/version', 'The version of this hub.'],
  ['GET /metrics', 'Prometheus metrics: drift severity and stale clusters.'],
  ['POST /mcp', 'The MCP server, see the MCP page.'],
]

export function DocsAPI() {
  const ns = useHubInfo()?.namespace ?? 'periscope'
  const hub = hubURL()
  const examples = `H="Authorization: Bearer $(oc whoami -t)"

# the Statistics page as CSV
curl -sS -H "$H" "${hub}/api/export.csv?page=statistics" -o statistics.csv

# seven days of pod counts per cluster
curl -sS -H "$H" "${hub}/api/timeline?key=pod-total&days=7"

# scrape now
curl -sS -X POST -H "$H" "${hub}/api/refresh"`
  return (
    <>
      <Section title="Access">
        <Content component="p" className="cc-prose">
          Every endpoint needs an OpenShift token as <code>Authorization: Bearer</code>, and that
          user needs <code>get</code> on services in namespace <code>{ns}</code>. Without a valid
          token the answer is <code>401</code>, without the right it is <code>403</code>.{' '}
          <code>oc whoami -t</code>, a ServiceAccount token and an MCP token all work.
        </Content>
        <CopyableCommand label="grant-view" command={`oc -n ${ns} policy add-role-to-user view <USER>`} />
      </Section>

      <Divider className="cc-doc-divider" />

      <Section title="Endpoints">
        <Table aria-label="API endpoints" variant="compact" borders>
          <Thead>
            <Tr>
              <Th>Endpoint</Th>
              <Th>Purpose</Th>
            </Tr>
          </Thead>
          <Tbody>
            {ENDPOINTS.map(([path, purpose]) => (
              <Tr key={path}>
                <Td dataLabel="Endpoint">
                  <code>{path}</code>
                </Td>
                <Td dataLabel="Purpose">{purpose}</Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      </Section>

      <Divider className="cc-doc-divider" />

      <Section title="Examples">
        <CopyableCommand label="api-examples" command={examples} />
      </Section>
    </>
  )
}
