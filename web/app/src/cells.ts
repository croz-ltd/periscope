import type { Cell } from './api'

// Severity from the backend: major gaps >= 10000, minor >= 100, patch small
// (>=2), prerelease == 1. Bucket into 1..4 shades (darker = further behind).
export function severityBucket(severity: number): 1 | 2 | 3 | 4 {
  if (severity >= 10000) return 4 // major
  if (severity >= 100) return 3 // minor
  if (severity >= 2) return 2 // patch
  return 1 // prerelease / tiny
}

export function cellClass(cell: Cell | undefined): string {
  if (!cell) return 'cc-cell cc-missing'
  switch (cell.state) {
    case 'leader':
      return 'cc-cell cc-leader'
    case 'unknown':
      return 'cc-cell cc-unknown'
    case 'not_installed':
      return 'cc-cell cc-missing'
    case 'behind':
      return `cc-cell cc-behind cc-behind-${severityBucket(cell.severity)}`
    default:
      return 'cc-cell'
  }
}

export function cellText(cell: Cell | undefined): string {
  if (!cell || cell.state === 'not_installed') return '—'
  const v = cell.version && cell.version.length > 0 ? cell.version : '(none)'
  return cell.state === 'unknown' ? `${v} ?` : v
}

// Human-readable tooltip lines for a cell, or null when there's nothing extra.
export function cellTooltip(cell: Cell | undefined): string | null {
  if (!cell) return null
  const lines: string[] = []
  switch (cell.state) {
    case 'leader':
      lines.push('At fleet-leading version')
      break
    case 'behind':
      lines.push(`Behind by ${cell.gapKind ?? 'version'}`)
      break
    case 'unknown':
      lines.push('Version could not be parsed as semver')
      break
    case 'not_installed':
      lines.push('Not installed on this cluster')
      break
  }
  if (cell.namespace) lines.push(`Namespace: ${cell.namespace}`)
  if (cell.extra) {
    for (const [k, val] of Object.entries(cell.extra)) {
      lines.push(`${k}: ${val}`)
    }
  }
  return lines.length ? lines.join('\n') : null
}
