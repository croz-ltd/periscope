import {
  Button,
  Label,
  SearchInput,
  Toolbar,
  ToolbarContent,
  ToolbarGroup,
  ToolbarItem,
} from '@patternfly/react-core'
import { ColumnsIcon, EyeSlashIcon } from '@patternfly/react-icons'

// The toolbar above the matrix: find a component in a page that has grown past
// one screen, and take cluster columns out of one that has grown past one width.
export function MatrixToolbar({
  query,
  onQueryChange,
  shown,
  total,
  hiddenClusters,
  onManageClusters,
  onShowAllClusters,
}: {
  query: string
  onQueryChange: (v: string) => void
  shown: number
  total: number
  hiddenClusters: number
  onManageClusters: () => void
  onShowAllClusters: () => void
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
        <ToolbarItem>
          <Button variant="secondary" icon={<ColumnsIcon />} onClick={onManageClusters}>
            Manage clusters
          </Button>
        </ToolbarItem>
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
