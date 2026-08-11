import { useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Checkbox,
  Content,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
} from '@patternfly/react-core'
import type { ClusterInfo } from './api'

// Manage view: pick the cluster columns the matrix shows. The list is checked
// rather than crossed out, so it reads the way the OpenShift console's column
// manager does, and a fleet too wide to scan can be cut down to the clusters in
// question.
//
// Selection is confirmed on Save, not applied per click, so a reader taking half
// a dozen clusters out of a wide matrix is not made to watch it reflow six times.
export function ManageViewModal({
  isOpen,
  clusters,
  hidden,
  onClose,
  onSave,
}: {
  isOpen: boolean
  clusters: ClusterInfo[]
  hidden: string[]
  onClose: () => void
  onSave: (hidden: string[]) => void
}) {
  const [draft, setDraft] = useState<Set<string>>(() => new Set(hidden))

  // Reopening starts from what is actually applied, so edits abandoned with
  // Cancel do not come back the next time the modal is opened.
  useEffect(() => {
    if (isOpen) setDraft(new Set(hidden))
  }, [isOpen, hidden])

  const toggle = (name: string, shown: boolean) =>
    setDraft((prev) => {
      const next = new Set(prev)
      if (shown) next.delete(name)
      else next.add(name)
      return next
    })

  const shownCount = clusters.filter((c) => !draft.has(c.name)).length

  return (
    <Modal
      variant="small"
      isOpen={isOpen}
      onClose={onClose}
      aria-labelledby="cc-manage-view-title"
    >
      <ModalHeader title="Manage view" labelId="cc-manage-view-title" />
      <ModalBody>
        <Content component="p">Selected clusters will appear in the matrix.</Content>
        {/* Hiding the leader otherwise looks like the whole fleet fell behind,
            so the text states that the comparison is unchanged. */}
        <Alert
          variant="info"
          isInline
          isPlain
          title="Hiding a cluster only changes this browser"
          className="cc-manage-hint"
        >
          Hidden clusters still count toward the reference version, and exports, metrics and the
          report still cover the whole fleet.
        </Alert>
        <div className="cc-cluster-picker">
          {clusters.map((c) => (
            <Checkbox
              key={c.name}
              id={`cc-cluster-${c.name}`}
              label={c.name}
              description={c.label && c.label !== c.name ? c.label : undefined}
              isChecked={!draft.has(c.name)}
              onChange={(_e, checked) => toggle(c.name, checked)}
            />
          ))}
        </div>
        {shownCount === 0 && (
          <Content component="p" className="cc-manage-warn">
            Keep at least one cluster to compare.
          </Content>
        )}
      </ModalBody>
      <ModalFooter>
        <Button
          key="save"
          variant="primary"
          isDisabled={shownCount === 0}
          onClick={() => {
            onSave([...draft])
            onClose()
          }}
        >
          Save
        </Button>
        <Button key="cancel" variant="secondary" onClick={onClose}>
          Cancel
        </Button>
        <Button key="all" variant="link" onClick={() => setDraft(new Set())}>
          Select all clusters
        </Button>
      </ModalFooter>
    </Modal>
  )
}
