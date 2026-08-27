import { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Checkbox,
  Content,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Spinner,
} from '@patternfly/react-core'
import type { PurgeResult, StoredCluster } from './api'
import { fetchStoredClusters, purgeClusters } from './api'

// Purge stale cluster data: remove the history of clusters the fleet no longer
// includes.
//
// Removing a cluster means deleting its credential Secret on the hub. Nothing
// scrapes it after that, but its column stays in the matrix, greyed out as
// stale, and its rows stay in the Changes feed. This is how that goes away.
//
// A cluster still holding a Secret is not offered. The configuration decides
// which clusters exist and purging only clears up after it, so there is no way
// to aim this at a cluster somebody is still watching. The server enforces the
// same rule, and this dialog only saves the reader from asking.

// staleClusters returns the ones a purge can take, in the order they were last
// seen, oldest first. Longest gone is what a reader is most sure about.
function staleClusters(all: StoredCluster[]): StoredCluster[] {
  return all
    .filter((c) => !c.joined)
    .sort((a, b) => new Date(a.last).getTime() - new Date(b.last).getTime())
}

function lastSeen(iso: string): string {
  const then = new Date(iso)
  const days = Math.floor((Date.now() - then.getTime()) / 86_400_000)
  if (days >= 1) return `last seen ${days} day${days === 1 ? '' : 's'} ago`
  return `last seen ${then.toLocaleString()}`
}

// held describes what purging one cluster would remove, so the number is on
// screen before the button is pressed rather than in the result afterwards.
function held(c: StoredCluster): string {
  const snaps = `${c.snapshots} snapshot${c.snapshots === 1 ? '' : 's'}`
  const changes = `${c.changes} change${c.changes === 1 ? '' : 's'}`
  return `${snaps}, ${changes}, ${lastSeen(c.last)}`
}

export function PurgeClustersModal({
  isOpen,
  onClose,
  onPurged,
}: {
  isOpen: boolean
  onClose: () => void
  // Called once data actually went, so the matrix behind the dialog reloads
  // without the columns that are no longer there.
  onPurged: () => void
}) {
  const [clusters, setClusters] = useState<StoredCluster[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<PurgeResult | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    const stored = await fetchStoredClusters()
    setClusters(stored)
    // Nothing is ticked to begin with. A dialog that arrives with every stale
    // cluster selected turns one careless click into a deletion.
    setSelected(new Set())
    setLoading(false)
  }, [])

  // Reopening reads the fleet again: a Secret deleted since the last look
  // changes what is purgeable, and so does a purge already done from another
  // browser.
  useEffect(() => {
    if (!isOpen) return
    setError(null)
    setResult(null)
    void load()
  }, [isOpen, load])

  const stale = staleClusters(clusters ?? [])
  const toggle = (name: string, checked: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (checked) next.add(name)
      else next.delete(name)
      return next
    })

  const onPurge = async () => {
    setBusy(true)
    setError(null)
    try {
      const res = await purgeClusters([...selected])
      setResult(res)
      if (res.purged.length > 0) onPurged()
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal variant="small" isOpen={isOpen} onClose={onClose} aria-labelledby="cc-purge-title">
      <ModalHeader title="Purge stale cluster data" labelId="cc-purge-title" />
      <ModalBody>
        <Content component="p">
          These clusters have history on the hub but no longer have a credential Secret, so nothing
          scrapes them any more. Purging removes their snapshots and their entries in the Changes
          feed.
        </Content>

        {error && (
          <Alert variant="danger" title="Purge failed" isInline className="cc-manage-hint">
            {error}
          </Alert>
        )}

        {result && result.purged.length > 0 && (
          <Alert
            variant="success"
            isInline
            className="cc-manage-hint"
            title={`Purged ${result.purged.length} cluster${result.purged.length === 1 ? '' : 's'}`}
          >
            {result.purged
              .map((p) => `${p.cluster}: ${p.snapshots} snapshots, ${p.changes} changes`)
              .join('; ')}
          </Alert>
        )}

        {result?.refused?.length ? (
          <Alert variant="warning" isInline className="cc-manage-hint" title="Some clusters were kept">
            {result.refused.map((r) => `${r.cluster}: ${r.reason}`).join('; ')}
          </Alert>
        ) : null}

        {loading ? (
          <div className="cc-purge-loading">
            <Spinner size="lg" aria-label="Reading what the hub holds" />
          </div>
        ) : clusters === null ? (
          <Alert variant="warning" isInline className="cc-manage-hint" title="Cannot read the stored clusters">
            The hub refused the request. Purging needs create on services in the hub namespace.
          </Alert>
        ) : stale.length === 0 ? (
          <Alert variant="info" isInline isPlain className="cc-manage-hint" title="Nothing to purge">
            Every cluster in the database is still joined, so all of its data is current.
          </Alert>
        ) : (
          <>
            <Alert
              variant="warning"
              isInline
              isPlain
              className="cc-manage-hint"
              title="Purging cannot be undone"
            >
              The history goes for good. Rejoining the cluster starts it again from the next scrape.
            </Alert>
            <div className="cc-cluster-picker">
              {stale.map((c) => (
                <Checkbox
                  key={c.name}
                  id={`cc-purge-${c.name}`}
                  label={c.name}
                  description={held(c)}
                  isChecked={selected.has(c.name)}
                  onChange={(_e, checked) => toggle(c.name, checked)}
                />
              ))}
            </div>
          </>
        )}
      </ModalBody>
      <ModalFooter>
        <Button
          key="purge"
          variant="danger"
          isDisabled={selected.size === 0 || busy}
          isLoading={busy}
          onClick={onPurge}
        >
          {selected.size > 0 ? `Purge ${selected.size}` : 'Purge'}
        </Button>
        <Button key="close" variant="secondary" onClick={onClose}>
          Close
        </Button>
      </ModalFooter>
    </Modal>
  )
}
