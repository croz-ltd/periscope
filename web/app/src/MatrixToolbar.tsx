import {
  Button,
  Label,
  SearchInput,
  ToggleGroup,
  ToggleGroupItem,
  Toolbar,
  ToolbarContent,
  ToolbarGroup,
  ToolbarItem,
  Tooltip,
} from '@patternfly/react-core'
import { ChartBarIcon, ColumnsIcon, EyeSlashIcon, TableIcon } from '@patternfly/react-icons'

// Statistics can be read as a table or as bar charts. Compare has no chart view:
// its cells are versions and dates, and there is nothing to plot.
export type StatsView = 'table' | 'charts'

// The toolbar above the matrix: find a component in a page that has grown past
// one screen, and take cluster columns out of one that has grown past one width.
export function MatrixToolbar({
  query,
  onQueryChange,
  shown,
  total,
  hiddenClusters,
  onManageView,
  onShowAllClusters,
  view,
  onViewChange,
}: {
  query: string
  onQueryChange: (v: string) => void
  shown: number
  total: number
  hiddenClusters: number
  onManageView: () => void
  onShowAllClusters: () => void
  view?: StatsView // omitted on pages that have only one view
  onViewChange?: (v: StatsView) => void
}) {
  return (
    <Toolbar
      id="cc-matrix-toolbar"
      className="cc-matrix-toolbar"
      inset={{ default: 'insetNone' }}
    >
      <ToolbarContent>
        <ToolbarItem>
          <SearchInput
            aria-label="Search components"
            placeholder="Search components..."
            value={query}
            onChange={(_e, value) => onQueryChange(value)}
            onClear={() => onQueryChange('')}
            resultsCount={query.trim() ? `${shown} of ${total}` : undefined}
          />
        </ToolbarItem>
        {/* Icon only, the way the console puts its column manager next to a
            table's search: the name belongs on the dialog, not on a button
            competing with the matrix for attention. */}
        <ToolbarItem>
          <Tooltip content="Manage view">
            <Button
              variant="plain"
              aria-label="Manage view"
              icon={<ColumnsIcon />}
              onClick={onManageView}
            />
          </Tooltip>
        </ToolbarItem>
        {view && onViewChange && (
          <ToolbarItem>
            <ToggleGroup aria-label="Statistics view">
              <ToggleGroupItem
                icon={<TableIcon />}
                text="Table"
                buttonId="cc-view-table"
                isSelected={view === 'table'}
                onChange={() => onViewChange('table')}
              />
              <ToggleGroupItem
                icon={<ChartBarIcon />}
                text="Charts"
                buttonId="cc-view-charts"
                isSelected={view === 'charts'}
                onChange={() => onViewChange('charts')}
              />
            </ToggleGroup>
          </ToolbarItem>
        )}
        {/* Columns missing from a comparison must never be a silent state, so the
            count stays on screen with the way back next to it. */}
        {hiddenClusters > 0 && (
          <ToolbarGroup gap={{ default: 'gapSm' }} align={{ default: 'alignStart' }}>
            <ToolbarItem>
              <Label color="blue" isCompact icon={<EyeSlashIcon />}>
                {hiddenClusters} {hiddenClusters === 1 ? 'cluster' : 'clusters'} hidden
              </Label>
            </ToolbarItem>
            <ToolbarItem>
              <Button variant="link" isInline onClick={onShowAllClusters}>
                Show all
              </Button>
            </ToolbarItem>
          </ToolbarGroup>
        )}
      </ToolbarContent>
    </Toolbar>
  )
}
