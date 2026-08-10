import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Bullseye,
  Button,
  Card,
  CardBody,
  CardTitle,
  EmptyState,
  EmptyStateBody,
  Flex,
  FlexItem,
  Label,
  Spinner,
  Switch,
  Tooltip,
} from '@patternfly/react-core'
import {
  AngleLeftIcon,
  AngleRightIcon,
  ArrowRightIcon,
  HistoryIcon,
  PlusCircleIcon,
  MinusCircleIcon,
  ArrowCircleUpIcon,
  DisconnectedIcon,
  CheckCircleIcon,
  PlugIcon,
} from '@patternfly/react-icons'
import type { Change, ChangeDay, ChangeKind } from './api'
import { fetchChangeCalendar, fetchChanges } from './api'

// Local calendar-day key. Days are the unit here, and a day is the reader's
// day: bucketing by UTC would file a 01:00 CET change under "yesterday".
function dayKey(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function startOfDay(key: string): Date {
  const [y, m, d] = key.split('-').map(Number)
  return new Date(y, m - 1, d, 0, 0, 0, 0)
}

function endOfDay(key: string): Date {
  const [y, m, d] = key.split('-').map(Number)
  return new Date(y, m - 1, d, 23, 59, 59, 999)
}

const WEEKDAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`

// What a calendar day says on hover. With counters hidden it has to be honest
// about a day whose only news was counters, or the day looks marked for nothing.
function label(d: ChangeDay, showCounters: boolean): string {
  const across = ` across ${plural(d.clusters, 'cluster')}`
  if (showCounters) return `${plural(d.count, 'change')}${across}`

  const real = d.count - d.counters
  if (real === 0) return `Only ${plural(d.counters, 'counter update')}${across}`
  if (d.counters === 0) return `${plural(real, 'change')}${across}`
  return `${plural(real, 'change')}${across}, plus ${plural(d.counters, 'counter update')}`
}

const KIND_META: Record<ChangeKind, { icon: React.ComponentType; label: string; cls: string }> = {
  updated: { icon: ArrowCircleUpIcon, label: 'Updated', cls: 'cc-chg-updated' },
  added: { icon: PlusCircleIcon, label: 'Installed', cls: 'cc-chg-added' },
  removed: { icon: MinusCircleIcon, label: 'Removed', cls: 'cc-chg-removed' },
  joined: { icon: PlugIcon, label: 'Joined', cls: 'cc-chg-joined' },
  unreachable: { icon: DisconnectedIcon, label: 'Unreachable', cls: 'cc-chg-unreachable' },
  recovered: { icon: CheckCircleIcon, label: 'Recovered', cls: 'cc-chg-recovered' },
}

// What happened, in one line, phrased for whoever is scrolling the feed.
function describe(c: Change): React.ReactNode {
  switch (c.kind) {
    case 'updated':
      return (
        <>
          <strong>{c.name}</strong> <span className="cc-chg-from">{c.from}</span>{' '}
          <ArrowRightIcon className="cc-chg-arrow" /> <span className="cc-chg-to">{c.to}</span>
        </>
      )
    case 'added':
      return (
        <>
          <strong>{c.name}</strong> appeared{c.to ? ` at ${c.to}` : ''}
        </>
      )
    case 'removed':
      return (
        <>
          <strong>{c.name}</strong> is gone{c.from ? ` (was ${c.from})` : ''}
        </>
      )
    case 'joined':
      return <>joined the fleet</>
    case 'unreachable':
      return <>stopped answering{c.to ? `: ${c.to}` : ''}</>
    case 'recovered':
      return <>is answering again</>
  }
}

// Month grid, Monday first.
//
// The calendar has to agree with the feed beside it, so it marks days the same
// way the feed counts them: the background is how much really changed, and it
// darkens with the amount, while a dot marks a day whose only news was counters
// moving. Weighing counters the same would colour in every day and promise
// changes the feed then hides. Turning counters on folds them into the
// background, because then they are what you came to look at.
function Calendar({
  month,
  days,
  selected,
  showCounters,
  onSelect,
  onMonth,
}: {
  month: Date
  days: Map<string, ChangeDay>
  selected: string | null
  showCounters: boolean
  onSelect: (key: string | null) => void
  onMonth: (delta: number) => void
}) {
  // What the day is worth on this calendar, given the counter switch.
  const weight = (d: ChangeDay) => (showCounters ? d.count : d.count - d.counters)
  const first = new Date(month.getFullYear(), month.getMonth(), 1)
  const daysInMonth = new Date(month.getFullYear(), month.getMonth() + 1, 0).getDate()
  const leading = (first.getDay() + 6) % 7 // Sunday=0 -> Monday-first
  const today = dayKey(new Date())

  const busiest = Math.max(1, ...[...days.values()].map(weight))
  const cells: React.ReactNode[] = []
  for (let i = 0; i < leading; i++) cells.push(<div key={`pad-${i}`} className="cc-cal-day cc-cal-pad" />)

  for (let day = 1; day <= daysInMonth; day++) {
    const key = dayKey(new Date(month.getFullYear(), month.getMonth(), day))
    const entry = days.get(key)
    const marked = entry ? weight(entry) : 0
    // Four steps: enough to tell a busy day from a quiet one, few enough that
    // each step is a visible difference.
    const heat = marked > 0 ? Math.min(4, Math.ceil((marked / busiest) * 4)) : 0
    // The dot exists for one case: a day that looks empty but is not, because
    // all it had was counters. On a day that is already coloured the background
    // has said it, and with counters folded in it would say it twice.
    const dot = !showCounters && !!entry && entry.counters > 0 && marked === 0
    const classes = [
      'cc-cal-day',
      heat > 0 ? `cc-cal-heat-${heat}` : 'cc-cal-quiet',
      dot ? 'cc-cal-counters' : '',
      key === selected ? 'cc-cal-selected' : '',
      key === today ? 'cc-cal-today' : '',
    ]
      .filter(Boolean)
      .join(' ')

    const cell = (
      <button
        key={key}
        type="button"
        className={classes}
        aria-label={entry ? `${key}, ${label(entry, showCounters)}` : key}
        aria-pressed={key === selected}
        disabled={!entry}
        onClick={() => onSelect(key === selected ? null : key)}
      >
        <span className="cc-cal-num">{day}</span>
        {dot && <span className="cc-cal-dot" />}
      </button>
    )
    cells.push(
      entry ? (
        <Tooltip key={key} content={label(entry, showCounters)}>
          {cell}
        </Tooltip>
      ) : (
        cell
      ),
    )
  }

  return (
    <Card isPlain className="cc-calendar">
      <CardTitle>
        <Flex justifyContent={{ default: 'justifyContentSpaceBetween' }} alignItems={{ default: 'alignItemsCenter' }}>
          <Button variant="plain" aria-label="Previous month" icon={<AngleLeftIcon />} onClick={() => onMonth(-1)} />
          <FlexItem>
            {month.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })}
          </FlexItem>
          <Button variant="plain" aria-label="Next month" icon={<AngleRightIcon />} onClick={() => onMonth(1)} />
        </Flex>
      </CardTitle>
      <CardBody>
        <div className="cc-cal-grid">
          {WEEKDAYS.map((w) => (
            <div key={w} className="cc-cal-weekday">
              {w}
            </div>
          ))}
          {cells}
        </div>
      </CardBody>
    </Card>
  )
}

export function Changes({ onTimeTravel }: { onTimeTravel: (at: string) => void }) {
  const [month, setMonth] = useState(() => new Date())
  const [days, setDays] = useState<Map<string, ChangeDay>>(new Map())
  const [selected, setSelected] = useState<string | null>(null)
  const [changes, setChanges] = useState<Change[]>([])
  const [hiddenCount, setHiddenCount] = useState(0)
  const [showCounters, setShowCounters] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // The calendar is loaded a month either side of the one on screen, so paging
  // back and forth does not refetch on every click.
  useEffect(() => {
    const from = new Date(month.getFullYear(), month.getMonth() - 1, 1)
    const to = new Date(month.getFullYear(), month.getMonth() + 2, 0, 23, 59, 59)
    fetchChangeCalendar(from, to)
      .then((cal) => setDays(new Map(cal.days.map((d) => [d.date, d]))))
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [month])

  // Counters (VM totals, snapshot volumes) move on nearly every scrape. They are
  // real changes, but they are not news, so they stay behind a switch, and the
  // server leaves them out so the limit is not spent on them.
  const load = useCallback(async () => {
    setLoading(true)
    try {
      setError(null)
      const feed = await fetchChanges({
        ...(selected ? { from: startOfDay(selected), to: endOfDay(selected) } : { limit: 200 }),
        includeCounters: showCounters,
      })
      setChanges(feed.changes)
      setHiddenCount(feed.hiddenCounters)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [selected, showCounters])

  useEffect(() => {
    void load()
  }, [load])

  // The feed reads as a diary: one heading per day, entries under it.
  const byDay = useMemo(() => {
    const groups = new Map<string, Change[]>()
    for (const c of changes) {
      const key = dayKey(new Date(c.time))
      const list = groups.get(key)
      if (list) list.push(c)
      else groups.set(key, [c])
    }
    return [...groups.entries()]
  }, [changes])

  return (
    <Flex
      className="cc-changes"
      alignItems={{ default: 'alignItemsFlexStart' }}
      flexWrap={{ default: 'wrap' }}
      gap={{ default: 'gapLg' }}
    >
      <FlexItem className="cc-changes-side">
        <Calendar
          month={month}
          days={days}
          selected={selected}
          showCounters={showCounters}
          onSelect={setSelected}
          onMonth={(d) => setMonth(new Date(month.getFullYear(), month.getMonth() + d, 1))}
        />
        <p className="cc-cal-legend">
          <span className="cc-cal-key cc-cal-heat-3" /> changes
          {!showCounters && (
            <>
              <span className="cc-cal-key cc-cal-quiet cc-cal-counters">
                <span className="cc-cal-dot" />
              </span>{' '}
              counter updates only
            </>
          )}
        </p>
        {selected && (
          <div className="cc-cal-actions">
            <Button
              variant="secondary"
              icon={<HistoryIcon />}
              isBlock
              onClick={() => onTimeTravel(endOfDay(selected).toISOString())}
            >
              View the matrix as it was
            </Button>
            <Button variant="link" isInline onClick={() => setSelected(null)}>
              Back to the latest changes
            </Button>
          </div>
        )}
      </FlexItem>

      <FlexItem flex={{ default: 'flex_1' }} className="cc-changes-feed">
        <Flex
          justifyContent={{ default: 'justifyContentSpaceBetween' }}
          alignItems={{ default: 'alignItemsCenter' }}
          className="cc-feed-toolbar"
        >
          <FlexItem>
            {selected
              ? `Changes on ${startOfDay(selected).toLocaleDateString(undefined, { dateStyle: 'full' })}`
              : 'Most recent changes'}
          </FlexItem>
          <FlexItem>
            <Switch
              id="cc-show-counters"
              label="Include counters"
              isChecked={showCounters}
              onChange={(_e, v) => setShowCounters(v)}
            />
          </FlexItem>
        </Flex>

        {error && (
          <Alert variant="danger" title="Failed to load changes" isInline>
            {error}
          </Alert>
        )}

        {loading ? (
          <Bullseye style={{ minHeight: '30vh' }}>
            <Spinner aria-label="Loading changes" />
          </Bullseye>
        ) : byDay.length === 0 ? (
          <EmptyState titleText="Nothing changed" headingLevel="h2" icon={HistoryIcon}>
            <EmptyStateBody>
              {selected
                ? 'No cluster reported anything new that day.'
                : 'Every scrape so far found the fleet exactly as it was.'}
              {hiddenCount > 0 && ` ${plural(hiddenCount, 'counter update')} hidden.`}
            </EmptyStateBody>
          </EmptyState>
        ) : (
          byDay.map(([day, entries]) => (
            <section key={day} className="cc-feed-day">
              <h3 className="cc-feed-date">
                {startOfDay(day).toLocaleDateString(undefined, { dateStyle: 'full' })}
                <span className="cc-feed-count">{entries.length}</span>
              </h3>
              <ul className="cc-feed-list">
                {entries.map((c, i) => {
                  const meta = KIND_META[c.kind]
                  const Icon = meta.icon
                  return (
                    <li key={`${c.time}-${c.cluster}-${c.key}-${i}`} className={`cc-feed-item ${meta.cls}`}>
                      <span className="cc-feed-time">
                        {new Date(c.time).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })}
                      </span>
                      <Tooltip content={meta.label}>
                        <span className="cc-feed-icon">
                          <Icon />
                        </span>
                      </Tooltip>
                      <Label isCompact className="cc-feed-cluster">
                        {c.cluster}
                      </Label>
                      <span className="cc-feed-text">{describe(c)}</span>
                    </li>
                  )
                })}
              </ul>
            </section>
          ))
        )}

        {!loading && hiddenCount > 0 && byDay.length > 0 && (
          <p className="cc-feed-hidden">
            {plural(hiddenCount, 'counter update')} hidden.
          </p>
        )}
      </FlexItem>
    </Flex>
  )
}
