import { useEffect, useState } from 'react'
import {
  Alert,
  Button,
  ClipboardCopyButton,
  CodeBlock,
  CodeBlockAction,
  CodeBlockCode,
  Content,
  FormGroup,
  HelperText,
  HelperTextItem,
  Modal,
  TextInput,
  Wizard,
  WizardHeader,
  WizardStep,
} from '@patternfly/react-core'
import { ExternalLinkAltIcon, PlusCircleIcon } from '@patternfly/react-icons'
import type { HubInfo } from './api'
import { fetchHubInfo } from './api'

// Joining a cluster, as the four steps it actually is.
//
// The commands are built from this hub's own address, namespace and label,
// because a command a reader has to edit is a command a reader gets wrong. The
// steps run on two different clusters, and the wizard exists mostly to keep that
// straight: step two runs on the cluster being joined, step three on the hub.

// The server accepts a DNS-1123 label and rejects anything else with a 400. The
// same rule runs here, so the answer arrives while the name is being typed.
const NAME_PATTERN = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/
const NAME_MAX = 63

const isValidName = (name: string) => name.length <= NAME_MAX && NAME_PATTERN.test(name)

// The namespace the served manifests create on the cluster being joined. The
// endpoint fixes it, so it is fixed here too.
const JOINED_NAMESPACE = 'periscope'

const PLACEHOLDER = '<CLUSTER_NAME>'

// CopyableCommand shows a command with the copy button in its corner. A
// multi-line shell command retyped by hand is a command mistyped by hand.
function CopyableCommand({ command, label }: { command: string; label: string }) {
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), 2500)
    return () => clearTimeout(timer)
  }, [copied])

  return (
    <CodeBlock
      className="cc-command"
      actions={
        <CodeBlockAction>
          <ClipboardCopyButton
            id={`copy-${label}`}
            textId={`command-${label}`}
            aria-label={`Copy the ${label} command`}
            onClick={() => {
              void navigator.clipboard?.writeText(command)
              setCopied(true)
            }}
            exitDelay={copied ? 1200 : 600}
            variant="plain"
          >
            {copied ? 'Copied' : 'Copy'}
          </ClipboardCopyButton>
        </CodeBlockAction>
      }
    >
      <CodeBlockCode id={`command-${label}`}>{command}</CodeBlockCode>
    </CodeBlock>
  )
}

// commandsFor builds both commands for one cluster name. A placeholder is meant
// to be read, so it is not percent-encoded the way a real name is.
function commandsFor(name: string, hub: HubInfo | null) {
  const cluster = name !== '' && isValidName(name) ? name : PLACEHOLDER
  const hubNamespace = hub?.namespace ?? JOINED_NAMESPACE
  const label = hub?.clusterLabel ?? 'periscope.io/cluster=true'
  const url = `${window.location.origin}/yaml/new-cluster?name=${
    cluster === PLACEHOLDER ? cluster : encodeURIComponent(cluster)
  }`

  return {
    cluster,
    url,
    apply: `oc apply -f <(curl -sH "Authorization: Bearer $(oc whoami -t)" \\
  "${url}")`,
    // Two namespaces, and mixing them up sends the reader to the wrong cluster.
    // The token is read where the manifests put it, on the joined cluster. The
    // Secret is created where this hub reads its cluster Secrets.
    register: `API_URL=$(oc whoami --show-server)
TOKEN=$(oc -n ${JOINED_NAMESPACE} get secret periscope-reader-token \\
  -o jsonpath='{.data.token}' | base64 -d)

oc -n ${hubNamespace} create secret generic ${cluster} \\
  --from-literal=apiURL="$API_URL" --from-literal=token="$TOKEN"
oc -n ${hubNamespace} label secret ${cluster} ${label}`,
    order: `oc -n ${hubNamespace} label secret ${cluster} periscope.io/order=10`,
  }
}

// AddClusterWizard walks the four steps. onRefresh starts the same scrape the
// Actions menu triggers, offered on the last step because that is when it is
// wanted.
export function AddClusterWizard({
  isOpen,
  onClose,
  onRefresh,
}: {
  isOpen: boolean
  onClose: () => void
  onRefresh?: () => void
}) {
  const [name, setName] = useState('')
  const [hub, setHub] = useState<HubInfo | null>(null)

  useEffect(() => {
    if (isOpen) void fetchHubInfo().then(setHub)
  }, [isOpen])

  const valid = name === '' || isValidName(name)
  const cmd = commandsFor(name, hub)

  if (!isOpen) return null

  return (
    <Modal
      variant="large"
      isOpen={isOpen}
      // Not onClose: the modal answers that with a close button of its own, next
      // to the one the wizard header already draws. Escape still works.
      onEscapePress={onClose}
      aria-label="Add a cluster"
      className="cc-wizard-modal"
    >
      <Wizard
        height={560}
        title="Add a cluster"
        header={
          <WizardHeader
            title="Add a cluster"
            description="A cluster joins by giving this hub a read-only token"
            onClose={onClose}
          />
        }
        onClose={onClose}
      >
        <WizardStep name="Name it" id="cc-wiz-name">
          <Content component="p" className="cc-prose">
            The name heads the cluster's column in the matrix, and it names the Secret this hub
            reads. A cluster that publishes a console banner shows that banner instead, and keeps
            this name in exports, metrics and the API.
          </Content>
          <FormGroup label="Cluster name" fieldId="cc-new-cluster-name" className="cc-name-field">
            <TextInput
              id="cc-new-cluster-name"
              value={name}
              type="text"
              placeholder="prod-emea"
              validated={valid ? 'default' : 'error'}
              onChange={(_e, v) => setName(v)}
              aria-label="Name for the new cluster"
            />
            <HelperText>
              <HelperTextItem variant={valid ? 'default' : 'error'}>
                {valid
                  ? 'Lower-case letters, digits and dashes.'
                  : 'Lower-case letters, digits and dashes, up to 63 characters.'}
              </HelperTextItem>
            </HelperText>
          </FormGroup>
          {name === '' && (
            <Content component="p" className="cc-step-note">
              Leave it empty to read the commands with {PLACEHOLDER} where the name goes.
            </Content>
          )}
        </WizardStep>

        <WizardStep name="Apply the manifests" id="cc-wiz-apply">
          <Content component="p">
            Run this <strong>on the cluster you want to compare</strong>, not on the hub:
          </Content>
          <CopyableCommand command={cmd.apply} label="apply" />
          <Content component="p" className="cc-step-note">
            It creates a read-only ServiceAccount, binds it to <code>cluster-reader</code>, and asks
            for a long-lived token. It writes nothing else and carries no credential.{' '}
            <Button
              variant="link"
              isInline
              component="a"
              href={cmd.url}
              target="_blank"
              rel="noreferrer"
              icon={<ExternalLinkAltIcon />}
              iconPosition="end"
            >
              Read the document first
            </Button>
          </Content>
        </WizardStep>

        <WizardStep name="Register on the hub" id="cc-wiz-register">
          <Content component="p">
            Read the token on that same cluster, then create the labeled Secret here on the hub:
          </Content>
          <CopyableCommand command={cmd.register} label="register" />
          <Content component="p" className="cc-step-note">
            The first two lines run on the cluster being joined, the last two on the hub. Use the
            external API URL, which is what <code>oc whoami --show-server</code> reports, so the
            certificate rows report the endpoint clients really use.
          </Content>
        </WizardStep>

        <WizardStep
          name="Finish"
          id="cc-wiz-done"
          // Nothing is left to cancel on the last step, and two buttons that both
          // shut the wizard is one too many.
          footer={{ nextButtonText: 'Close', onNext: onClose, isCancelHidden: true }}
        >
          <Alert
            variant="success"
            isInline
            isPlain
            title={`${cmd.cluster} appears in the matrix after the next scrape`}
          />
          <Content component="p" className="cc-prose">
            The hub polls on its own interval, ten minutes by default. To see the cluster now, start
            a scrape.
          </Content>
          {onRefresh && (
            <Button
              variant="secondary"
              onClick={() => {
                onRefresh()
                onClose()
              }}
            >
              Scrape now
            </Button>
          )}
          <Content component="p" className="cc-step-note">
            Column order is optional, and lower sorts further left:
          </Content>
          <CopyableCommand command={cmd.order} label="order" />
        </WizardStep>
      </Wizard>
    </Modal>
  )
}

// AddClusterButton is the launcher. It sits on the Docs page and in the empty
// state of a hub with no clusters yet, which is the other moment it is wanted.
export function AddClusterButton({
  onClick,
  variant = 'primary',
}: {
  onClick: () => void
  variant?: 'primary' | 'secondary' | 'link'
}) {
  return (
    <Button variant={variant} icon={<PlusCircleIcon />} onClick={onClick}>
      Add a cluster
    </Button>
  )
}
