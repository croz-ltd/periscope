import { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Breadcrumb,
  BreadcrumbItem,
  Bullseye,
  Button,
  Content,
  Divider,
  Dropdown,
  DropdownItem,
  DropdownList,
  EmptyState,
  EmptyStateActions,
  EmptyStateBody,
  EmptyStateFooter,
  Flex,
  FlexItem,
  MenuToggle,
  Nav,
  NavExpandable,
  NavItem,
  NavList,
  Page,
  PageSidebar,
  PageSidebarBody,
  PageSection,
  Spinner,
  Title,
} from '@patternfly/react-core'
import { ClusterIcon, CubesIcon, SearchIcon, SyncAltIcon, DownloadIcon } from '@patternfly/react-icons'
import type { Matrix } from './api'
import { fetchMatrix, triggerRefresh } from './api'
import { MatrixTable } from './MatrixTable'
import type { StatsView } from './MatrixToolbar'
import { MatrixToolbar } from './MatrixToolbar'
import { StatisticsCharts } from './StatisticsCharts'
import { ManageViewModal } from './ManageView'
import { getHiddenClusters, saveHiddenClusters, visibleClusters } from './clusterPrefs'
import { countComponents, filterGroups, rowsByKey } from './matrixView'
import { AppMasthead } from './AppMasthead'
import { About } from './About'
import { Docs } from './Docs'
import { Changes } from './Changes'

const LEGEND_GROUPS: { title: string; items: { cls: string; label: string }[] }[] = [
  {
    title: 'Version',
    items: [
      { cls: 'cc-leader', label: 'Leader' },
      { cls: 'cc-behind cc-behind-3', label: 'Behind (darker = bigger gap)' },
      { cls: 'cc-unknown', label: 'Unknown' },
    ],
  },
  {
    title: 'Match',
    items: [
      { cls: 'cc-match', label: 'Consistent' },
      { cls: 'cc-mismatch', label: 'Differs' },
    ],
  },
  {
    title: 'Expiry',
    items: [
      { cls: 'cc-exp-ok', label: 'OK' },
      { cls: 'cc-exp-warn', label: 'Warning' },
      { cls: 'cc-exp-crit', label: 'Critical' },
    ],
  },
  {
    title: 'Other',
    items: [{ cls: 'cc-missing', label: 'Not installed' }],
  },
]

function Legend() {
  return (
    <Flex className="cc-legend" spaceItems={{ default: 'spaceItemsLg' }} alignItems={{ default: 'alignItemsCenter' }}>
      {LEGEND_GROUPS.map((g) => (
        <FlexItem key={g.title}>
          <span className="cc-legend-group-title">{g.title}:</span>{' '}
          {g.items.map((l) => (
            <span key={l.label} className="cc-legend-item">
              <span className={`cc-swatch ${l.cls}`} /> <span className="cc-legend-label">{l.label}</span>
            </span>
          ))}
        </FlexItem>
      ))}
    </Flex>
  )
}

// Page header in the OpenShift console shape: breadcrumb, then title and
// description on the left with the page actions pinned to the right, closed off
// by a rule that separates the header from the page body.
function PageHeader({
  breadcrumb,
  title,
  description,
  actions,
}: {
  breadcrumb?: React.ReactNode
  title: string
  description?: React.ReactNode
  actions?: React.ReactNode
}) {
  return (
    <div className="cc-page-header">
      {breadcrumb}
      <Flex
        justifyContent={{ default: 'justifyContentSpaceBetween' }}
        alignItems={{ default: 'alignItemsFlexStart' }}
        flexWrap={{ default: 'wrap' }}
      >
        <FlexItem>
          <Title headingLevel="h1" className="cc-page-title">
            {title}
          </Title>
          {description && (
            <Content component="p" className="cc-page-desc">
              {description}
            </Content>
          )}
        </FlexItem>
        {actions && <FlexItem>{actions}</FlexItem>}
      </Flex>
    </div>
  )
}

// The two matrix pages compare different things, so each states its own scope.
const PAGE_DESCRIPTIONS: Record<string, string> = {
  compare: 'Version and configuration drift across OpenShift clusters',
  statistics: 'Counts and inventory reported by each cluster',
}

type NavKey = 'compare' | 'statistics' | 'changes' | 'docs' | 'about'

export default function App() {
  const [matrix, setMatrix] = useState<Matrix | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [isSidebarOpen, setSidebarOpen] = useState(true)
  const [activeNav, setActiveNav] = useState<NavKey>('compare')
  const [isClustersExpanded, setClustersExpanded] = useState(true)
  const [actionsOpen, setActionsOpen] = useState(false)
  // Non-null means the matrix pages are showing history rather than the fleet
  // as it is now (RFC3339, set from the Changes calendar).
  const [at, setAt] = useState<string | null>(null)
  // Client-side component search, shared by both matrix pages: a large fleet
  // makes either page longer than a screen, and the answer is usually one row.
  const [query, setQuery] = useState('')
  // Cluster columns this browser leaves out, read once at startup and written
  // back on every change (localStorage, never sent to the server).
  const [hidden, setHidden] = useState<string[]>(getHiddenClusters)
  const [manageOpen, setManageOpen] = useState(false)
  // Statistics reads as a table or as bar charts. The table stays the default,
  // because it holds every row, and charts only the countable ones.
  const [statsView, setStatsView] = useState<StatsView>('table')

  const changeHidden = useCallback((names: string[]) => {
    saveHiddenClusters(names)
    setHidden(names)
  }, [])

  const load = useCallback(async () => {
    try {
      setError(null)
      const m = await fetchMatrix(at ?? undefined)
      setMatrix(m)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [at])

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

  const hasClusters = !!matrix && matrix.clusters.length > 0
  const pageId = activeNav === 'statistics' ? 'statistics' : 'compare'
  const page = matrix?.pages.find((p) => p.id === pageId)
  const pageTitle = page?.title ?? (pageId === 'statistics' ? 'Statistics' : 'Compare')

  // What this page actually renders: the fleet minus the hidden columns, and the
  // page's groups minus the components the search excludes.
  const allClusters = matrix?.clusters ?? []
  const shownClusters = visibleClusters(allClusters, hidden)
  const hiddenCount = allClusters.length - shownClusters.length
  const pageGroups = page?.groups ?? []
  const byKey = rowsByKey(matrix?.rows ?? [])
  const shownGroups = filterGroups(pageGroups, byKey, query)
  const totalComponents = countComponents(pageGroups)
  const shownComponents = countComponents(shownGroups)

  const masthead = (
    <AppMasthead
      isSidebarOpen={isSidebarOpen}
      onSidebarToggle={() => setSidebarOpen((o) => !o)}
      onAbout={() => setActiveNav('about')}
    />
  )

  // An export follows what is on screen, history included, so a CSV taken while
  // time travelling is the matrix you were looking at.
  const exportHref = (format: 'csv' | 'json') =>
    at ? `/api/export.${format}?at=${encodeURIComponent(at)}` : `/api/export.${format}`

  // One Actions menu per matrix page, the way the console groups page actions.
  // The toggle shows a spinner while a refresh is in flight.
  const actions = (
    <Dropdown
      isOpen={actionsOpen}
      onOpenChange={setActionsOpen}
      onSelect={() => setActionsOpen(false)}
      popperProps={{ position: 'right' }}
      toggle={(toggleRef) => (
        <MenuToggle
          ref={toggleRef}
          variant="secondary"
          isExpanded={actionsOpen}
          isDisabled={refreshing}
          icon={refreshing ? <Spinner size="sm" aria-label="Refreshing" /> : undefined}
          onClick={() => setActionsOpen((v) => !v)}
        >
          Actions
        </MenuToggle>
      )}
    >
      <DropdownList>
        {/* Scraping now does not change a view of the past, so time travel
            offers the way back instead. */}
        {at ? (
          <DropdownItem key="now" icon={<SyncAltIcon />} onClick={() => setAt(null)}>
            Back to now
          </DropdownItem>
        ) : (
          <DropdownItem key="refresh" icon={<SyncAltIcon />} onClick={onRefresh}>
            Refresh
          </DropdownItem>
        )}
        <Divider component="li" />
        <DropdownItem key="csv" icon={<DownloadIcon />} component="a" href={exportHref('csv')}>
          Export CSV
        </DropdownItem>
        <DropdownItem key="json" icon={<DownloadIcon />} component="a" href={exportHref('json')}>
          Export JSON
        </DropdownItem>
      </DropdownList>
    </Dropdown>
  )

  const sidebar = (
    <PageSidebar isSidebarOpen={isSidebarOpen}>
      <PageSidebarBody>
        <div className="cc-perspective">
          <ClusterIcon className="cc-perspective-icon" />
          <span className="cc-perspective-label">Cluster fleet</span>
        </div>
        <Nav
          aria-label="Main navigation"
          onSelect={(_e, item) => setActiveNav(item.itemId as NavKey)}
        >
          <NavList>
            <NavExpandable
              title="Clusters"
              groupId="clusters"
              isExpanded={isClustersExpanded}
              onExpand={(_e, expanded) => setClustersExpanded(expanded)}
              isActive={activeNav === 'compare' || activeNav === 'statistics'}
            >
              <NavItem itemId="compare" isActive={activeNav === 'compare'}>
                Compare
              </NavItem>
              <NavItem itemId="statistics" isActive={activeNav === 'statistics'}>
                Statistics
              </NavItem>
              <NavItem itemId="changes" isActive={activeNav === 'changes'}>
                Changes
              </NavItem>
            </NavExpandable>
            <NavItem itemId="docs" isActive={activeNav === 'docs'}>
              Docs
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
          <PageHeader title="About" description="What Periscope is and how it collects its data" />
          <About />
        </PageSection>
      ) : activeNav === 'changes' ? (
        <PageSection>
          <PageHeader
            breadcrumb={
              <Breadcrumb className="cc-breadcrumb">
                <BreadcrumbItem>Clusters</BreadcrumbItem>
                <BreadcrumbItem isActive>Changes</BreadcrumbItem>
              </Breadcrumb>
            }
            title="Changes"
            description="What changed across the fleet, and what the matrix looked like then"
          />
          <Changes
            onTimeTravel={(t) => {
              setAt(t)
              setActiveNav('compare')
            }}
          />
        </PageSection>
      ) : activeNav === 'docs' ? (
        <PageSection>
          <PageHeader
            title="Component reference"
            description="Component keys discovered across your clusters"
          />
          <Docs rows={matrix?.rows ?? []} />
        </PageSection>
      ) : (
        <PageSection>
          <PageHeader
            breadcrumb={
              <Breadcrumb className="cc-breadcrumb">
                <BreadcrumbItem>Clusters</BreadcrumbItem>
                <BreadcrumbItem isActive>{pageTitle}</BreadcrumbItem>
              </Breadcrumb>
            }
            title={pageTitle}
            description={PAGE_DESCRIPTIONS[pageId]}
            actions={actions}
          />

          {/* Time travel is easy to forget you are in, and every cell is
              quietly wrong for today. The banner stays until you leave it. */}
          {at && (
            <Alert
              variant="info"
              isInline
              title={`Showing the fleet as it was on ${new Date(at).toLocaleString()}`}
              style={{ marginBottom: '1rem' }}
              actionLinks={
                <Button variant="link" isInline onClick={() => setAt(null)}>
                  Back to now
                </Button>
              }
            >
              This is history: nothing here reflects the clusters as they are right now.
            </Alert>
          )}

          {error && (
            <Alert variant="danger" title="Failed to load matrix" isInline style={{ marginBottom: '1rem' }}>
              {error}
            </Alert>
          )}

          {matrix?.warning && (
            <Alert variant="warning" title="Custom grouping" isInline style={{ marginBottom: '1rem' }}>
              {matrix.warning}
            </Alert>
          )}

          {loading ? (
            <Bullseye style={{ minHeight: '40vh' }}>
              <Spinner aria-label="Loading matrix" />
            </Bullseye>
          ) : !hasClusters ? (
            <EmptyState titleText="No clusters scraped yet" headingLevel="h2" icon={CubesIcon}>
              <EmptyStateBody>
                Join clusters with a labeled Secret, or wait for the first scrape to complete, then refresh.
              </EmptyStateBody>
            </EmptyState>
          ) : page && page.groups.length > 0 ? (
            <>
              {/* Statistics cells are plain values, so the drift key explains nothing. */}
              {pageId === 'compare' && <Legend />}
              <MatrixToolbar
                query={query}
                onQueryChange={setQuery}
                shown={shownComponents}
                total={totalComponents}
                hiddenClusters={hiddenCount}
                onManageView={() => setManageOpen(true)}
                onShowAllClusters={() => changeHidden([])}
                view={pageId === 'statistics' ? statsView : undefined}
                onViewChange={pageId === 'statistics' ? setStatsView : undefined}
              />
              {/* A stored preference can outlive the fleet it was made for, so
                  hiding everything is recoverable rather than a blank table. */}
              {shownClusters.length === 0 ? (
                <EmptyState titleText="All clusters are hidden" headingLevel="h2" icon={CubesIcon}>
                  <EmptyStateBody>
                    Every joined cluster is hidden in this browser, so there is nothing to compare.
                  </EmptyStateBody>
                  <EmptyStateFooter>
                    <EmptyStateActions>
                      <Button variant="primary" onClick={() => changeHidden([])}>
                        Show all clusters
                      </Button>
                    </EmptyStateActions>
                  </EmptyStateFooter>
                </EmptyState>
              ) : shownGroups.length === 0 ? (
                <EmptyState titleText="No components match your search" headingLevel="h2" icon={SearchIcon}>
                  <EmptyStateBody>
                    Nothing on this page matches “{query.trim()}”. Component names, keys and kinds are
                    searched.
                  </EmptyStateBody>
                  <EmptyStateFooter>
                    <EmptyStateActions>
                      <Button variant="link" onClick={() => setQuery('')}>
                        Clear search
                      </Button>
                    </EmptyStateActions>
                  </EmptyStateFooter>
                </EmptyState>
              ) : pageId === 'statistics' && statsView === 'charts' ? (
                <StatisticsCharts groups={shownGroups} rows={byKey} clusters={shownClusters} />
              ) : (
                <div className="cc-table-wrap">
                  <MatrixTable matrix={matrix!} groups={shownGroups} clusters={shownClusters} />
                </div>
              )}
              <ManageViewModal
                isOpen={manageOpen}
                clusters={allClusters}
                hidden={hidden}
                onClose={() => setManageOpen(false)}
                onSave={changeHidden}
              />
            </>
          ) : (
            <EmptyState titleText="Nothing to show on this page" headingLevel="h2" icon={CubesIcon}>
              <EmptyStateBody>
                The “{pageTitle}” page has no groups configured.
              </EmptyStateBody>
            </EmptyState>
          )}
        </PageSection>
      )}
    </Page>
  )
}
