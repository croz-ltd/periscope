import type { Cell } from './api'

// Severity from the backend (version compare): major gaps >= 10000, minor >= 100,
// patch small (>=2), prerelease == 1. Bucket into 1..4 shades (darker = further behind).
export function severityBucket(severity: number): 1 | 2 | 3 | 4 {
  if (severity >= 10000) return 4 // major
  if (severity >= 100) return 3 // minor
  if (severity >= 2) return 2 // patch
  return 1 // prerelease / tiny
}

// All cells share the cc-cell base (so theme fixes apply uniformly), plus a
// state-specific modifier.
export function cellClass(cell: Cell | undefined): string {
  if (!cell) return 'cc-cell cc-missing'
  switch (cell.state) {
    case 'leader':
      return 'cc-cell cc-leader'
    case 'behind':
      return `cc-cell cc-behind cc-behind-${severityBucket(cell.severity)}`
    case 'unknown':
      return 'cc-cell cc-unknown'
    case 'not_installed':
      return 'cc-cell cc-missing'
    case 'match':
      return 'cc-cell cc-match'
    case 'mismatch':
      return 'cc-cell cc-mismatch'
    case 'expiry_ok':
      return 'cc-cell cc-exp-ok'
    case 'expiry_warn':
      return 'cc-cell cc-exp-warn'
    case 'expiry_crit':
      return 'cc-cell cc-exp-crit'
    default:
      return 'cc-cell'
  }
}

const isExpiry = (s: Cell['state']) =>
  s === 'expiry_ok' || s === 'expiry_warn' || s === 'expiry_crit'

export function cellText(cell: Cell | undefined): string {
  if (!cell || cell.state === 'not_installed') return '-'
  const v = cell.version && cell.version.length > 0 ? cell.version : '(none)'
  if (cell.state === 'unknown') return `${v} ?`
  if (isExpiry(cell.state)) {
    return cell.severity < 0 ? `${v} · expired` : `${v} · ${cell.severity}d`
  }
  return v
}

// Human-readable tooltip lines for a cell, or null when there's nothing extra.
// `reference` is the row's reference value (row.leader), used by mismatch cells.
export function cellTooltip(cell: Cell | undefined, reference?: string): string | null {
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
      lines.push('Version did not parse as semver')
      break
    case 'not_installed':
      lines.push('Not installed on this cluster')
      break
    case 'match':
      lines.push('Consistent with the fleet value')
      break
    case 'mismatch':
      lines.push(`Differs from fleet value: ${reference || '(unknown)'}`)
      break
    case 'expiry_ok':
    case 'expiry_warn':
    case 'expiry_crit':
      lines.push(`Expires ${cell.version ?? '(unknown)'} (${cell.severity} days)`)
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
