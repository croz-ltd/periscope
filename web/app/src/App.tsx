import { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Bullseye,
  Button,
  Content,
  EmptyState,
  EmptyStateBody,
  Flex,
  FlexItem,
  Nav,
  NavItem,
  NavList,
  Page,
  PageSidebar,
  PageSidebarBody,
  PageSection,
  Spinner,
  Title,
  Toolbar,
  ToolbarContent,
  ToolbarItem,
} from '@patternfly/react-core'
import { CubesIcon, SyncAltIcon, DownloadIcon } from '@patternfly/react-icons'
import type { Matrix } from './api'
import { fetchMatrix, triggerRefresh } from './api'
import { MatrixTable } from './MatrixTable'
import { AppMasthead } from './AppMasthead'

const LEGEND: { cls: string; label: string }[] = [
  { cls: 'cc-leader', label: 'Leader (fleet max)' },
  { cls: 'cc-behind cc-behind-2', label: 'Behind — patch' },
  { cls: 'cc-behind cc-behind-3', label: 'Behind — minor' },
  { cls: 'cc-behind cc-behind-4', label: 'Behind — major' },
  { cls: 'cc-unknown', label: 'Unknown version' },
  { cls: 'cc-missing', label: 'Not installed' },
]

function Legend() {
  return (
    <Flex className="cc-legend" spaceItems={{ default: 'spaceItemsMd' }} alignItems={{ default: 'alignItemsCenter' }}>
      {LEGEND.map((l) => (
        <FlexItem key={l.label}>
          <span className={`cc-swatch ${l.cls}`} /> <span className="cc-legend-label">{l.label}</span>
        </FlexItem>
      ))}
    </Flex>
  )
}

type NavKey = 'matrix' | 'about'

export default function App() {
  const [matrix, setMatrix] = useState<Matrix | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [isSidebarOpen, setSidebarOpen] = useState(true)
  const [activeNav, setActiveNav] = useState<NavKey>('matrix')

  const load = useCallback(async () => {
    try {
      setError(null)
      const m = await fetchMatrix()
      setMatrix(m)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const onRefresh = useCallback(async () => {
    setRefreshing(true)
    try {
      await triggerRefresh()
      // give the scheduler a moment to scrape, then reload
      await new Promise((r) => setTimeout(r, 2500))
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setRefreshing(false)
    }
  }, [load])

  const hasData = !!matrix && matrix.clusters.length > 0

  const masthead = (
    <AppMasthead isSidebarOpen={isSidebarOpen} onSidebarToggle={() => setSidebarOpen((o) => !o)} />
  )

  const sidebar = (
    <PageSidebar isSidebarOpen={isSidebarOpen}>
      <PageSidebarBody>
        <Nav
          aria-label="Main navigation"
          onSelect={(_e, item) => setActiveNav(item.itemId as NavKey)}
        >
          <NavList>
            <NavItem itemId="matrix" isActive={activeNav === 'matrix'}>
              Matrix
            </NavItem>
            <NavItem itemId="about" isActive={activeNav === 'about'}>
              About
            </NavItem>
          </NavList>
        </Nav>
      </PageSidebarBody>
    </PageSidebar>
  )

  return (
    <Page masthead={masthead} sidebar={sidebar}>
      {activeNav === 'about' ? (
        <PageSection>
          <Title headingLevel="h1">About</Title>
          <Content component="p">
            Cluster Comparator shows version drift across OpenShift clusters — the OpenShift
            release, installed operators, and managed CSI driver versions. Each cluster is added
            as a labeled Secret; the drift baseline is the highest semver seen across the fleet.
          </Content>
        </PageSection>
      ) : (
        <PageSection>
          <Flex justifyContent={{ default: 'justifyContentSpaceBetween' }} alignItems={{ default: 'alignItemsCenter' }}>
            <FlexItem>
              <Title headingLevel="h1">Cluster Comparator</Title>
              <Content component="small">Version drift across OpenShift clusters</Content>
            </FlexItem>
          </Flex>

          <Toolbar>
            <ToolbarContent>
              <ToolbarItem>
                <Button
                  variant="primary"
                  icon={<SyncAltIcon />}
                  onClick={onRefresh}
                  isLoading={refreshing}
                  isDisabled={refreshing}
                >
                  Refresh
                </Button>
              </ToolbarItem>
              <ToolbarItem>
                <Button variant="secondary" icon={<DownloadIcon />} component="a" href="/api/export.csv">
                  Export CSV
                </Button>
              </ToolbarItem>
              <ToolbarItem>
                <Button variant="secondary" icon={<DownloadIcon />} component="a" href="/api/export.json">
                  Export JSON
                </Button>
              </ToolbarItem>
            </ToolbarContent>
          </Toolbar>

          {error && (
            <Alert variant="danger" title="Failed to load matrix" isInline style={{ marginBottom: '1rem' }}>
              {error}
            </Alert>
          )}

          {loading ? (
            <Bullseye style={{ minHeight: '40vh' }}>
              <Spinner aria-label="Loading matrix" />
            </Bullseye>
          ) : hasData ? (
            <>
              <Legend />
              <div className="cc-table-wrap">
                <MatrixTable matrix={matrix!} />
              </div>
            </>
          ) : (
            <EmptyState titleText="No clusters scraped yet" headingLevel="h2" icon={CubesIcon}>
              <EmptyStateBody>
                Join clusters with a labeled Secret, or wait for the first scrape to complete, then refresh.
              </EmptyStateBody>
            </EmptyState>
          )}
        </PageSection>
      )}
    </Page>
  )
}
