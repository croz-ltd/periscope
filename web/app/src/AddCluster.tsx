import { useEffect, useState } from "react";
import {
  Alert,
  AlertActionLink,
  Button,
  ClipboardCopyButton,
  CodeBlock,
  CodeBlockAction,
  CodeBlockCode,
  Content,
  FormGroup,
  HelperText,
  HelperTextItem,
  Checkbox,
  FormSelect,
  FormSelectOption,
  List,
  ListItem,
  Modal,
  Spinner,
  TextArea,
  TextInput,
  Wizard,
  WizardHeader,
  WizardStep,
} from "@patternfly/react-core";
import { ExternalLinkAltIcon, PlusCircleIcon } from "@patternfly/react-icons";
import type { HubInfo, JoinResult } from "./api";
import { fetchHubInfo, joinCluster } from "./api";

// The two ways in, the same pair Advanced Cluster Management offers. Manual prints
// the commands. Credentials hands the hub an API URL and a token, and the hub
// prepares the cluster itself.
type ImportMode = "manual" | "credentials";

// Joining a cluster, as the four steps it actually is.
//
// The commands are built from this hub's own address, namespace and label,
// because a command a reader has to edit is a command a reader gets wrong. The
// steps run on two different clusters, and the wizard exists mostly to keep that
// straight: step two runs on the cluster being joined, step three on the hub.

// The server accepts a DNS-1123 label and rejects anything else with a 400. The
// same rule runs here, so the answer arrives while the name is being typed.
const NAME_PATTERN = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
const NAME_MAX = 63;

const isValidName = (name: string) =>
  name.length <= NAME_MAX && NAME_PATTERN.test(name);

// The namespace the served manifests create on the cluster being joined. The
// endpoint fixes it, so it is fixed here too.
const JOINED_NAMESPACE = "periscope";

const PLACEHOLDER = "<CLUSTER_NAME>";

// CopyableCommand shows a command with the copy button in its corner. A
// multi-line shell command retyped by hand is a command mistyped by hand.
function CopyableCommand({
  command,
  label,
}: {
  command: string;
  label: string;
}) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 2500);
    return () => clearTimeout(timer);
  }, [copied]);

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
              void navigator.clipboard?.writeText(command);
              setCopied(true);
            }}
            exitDelay={copied ? 1200 : 600}
            variant="plain"
          >
            {copied ? "Copied" : "Copy"}
          </ClipboardCopyButton>
        </CodeBlockAction>
      }
    >
      <CodeBlockCode id={`command-${label}`}>{command}</CodeBlockCode>
    </CodeBlock>
  );
}

// commandsFor builds both commands for one cluster name. A placeholder is meant
// to be read, so it is not percent-encoded the way a real name is.
function commandsFor(name: string, hub: HubInfo | null) {
  const cluster = name !== "" && isValidName(name) ? name : PLACEHOLDER;
  const hubNamespace = hub?.namespace ?? JOINED_NAMESPACE;
  const label = hub?.clusterLabel ?? "periscope.io/cluster=true";
  const url = `${window.location.origin}/yaml/new-cluster?name=${
    cluster === PLACEHOLDER ? cluster : encodeURIComponent(cluster)
  }`;

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
  };
}

// CredentialsForm is the second way in: where the cluster is, and a token that can
// create a ServiceAccount and a ClusterRoleBinding on it.
function CredentialsForm({
  apiURL,
  token,
  caBundle,
  insecure,
  onChange,
}: {
  apiURL: string;
  token: string;
  caBundle: string;
  insecure: boolean;
  onChange: (patch: Partial<CredentialsState>) => void;
}) {
  return (
    <>
      <Content component="p" className="cc-prose">
        The hub prepares the cluster with this token, then throws it away. What
        it keeps is the read-only token the cluster mints, which is the one
        every scrape uses.
      </Content>
      <FormGroup
        label="API URL"
        fieldId="cc-join-url"
        isRequired
        className="cc-join-field"
      >
        <TextInput
          id="cc-join-url"
          value={apiURL}
          type="url"
          placeholder="https://api.prod.example.com:6443"
          onChange={(_e, v) => onChange({ apiURL: v })}
          aria-label="API URL of the cluster to join"
        />
        <HelperText>
          <HelperTextItem>
            The external URL, which is what <code>oc whoami --show-server</code>{" "}
            reports.
          </HelperTextItem>
        </HelperText>
      </FormGroup>
      <FormGroup
        label="API token"
        fieldId="cc-join-token"
        isRequired
        className="cc-join-field"
      >
        <TextInput
          id="cc-join-token"
          value={token}
          type="password"
          onChange={(_e, v) => onChange({ token: v })}
          aria-label="Token for the cluster to join"
        />
        <HelperText>
          <HelperTextItem>
            It needs to create a namespace, a ServiceAccount and a
            ClusterRoleBinding, so a cluster-admin token. Used once, never
            stored.
          </HelperTextItem>
        </HelperText>
      </FormGroup>
      <FormGroup
        label="CA bundle"
        fieldId="cc-join-ca"
        className="cc-join-field"
      >
        <TextArea
          id="cc-join-ca"
          value={caBundle}
          rows={2}
          placeholder="-----BEGIN CERTIFICATE-----"
          onChange={(_e, v) => onChange({ caBundle: v })}
          aria-label="CA bundle in PEM format"
        />
        <HelperText>
          <HelperTextItem>
            PEM for the API server certificate. Without it the connection cannot
            be verified.
          </HelperTextItem>
        </HelperText>
      </FormGroup>
      <Checkbox
        id="cc-join-insecure"
        label="Continue without verifying the certificate"
        description="Every scrape of this cluster is unverified too."
        isChecked={insecure}
        isDisabled={caBundle.trim() !== ""}
        onChange={(_e, checked) => onChange({ insecure: checked })}
      />
    </>
  );
}

interface CredentialsState {
  apiURL: string;
  token: string;
  caBundle: string;
  insecure: boolean;
}

// AddClusterWizard walks the four steps. onRefresh starts the same scrape the
// Actions menu triggers, offered on the last step because that is when it is
// wanted.
export function AddClusterWizard({
  isOpen,
  onClose,
  onRefresh,
}: {
  isOpen: boolean;
  onClose: () => void;
  onRefresh?: () => void;
}) {
  const [name, setName] = useState("");
  const [hub, setHub] = useState<HubInfo | null>(null);
  const [mode, setMode] = useState<ImportMode>("manual");
  const [creds, setCreds] = useState<CredentialsState>({
    apiURL: "",
    token: "",
    caBundle: "",
    insecure: false,
  });
  const [joining, setJoining] = useState(false);
  const [joined, setJoined] = useState<JoinResult | null>(null);
  const [joinError, setJoinError] = useState<string | null>(null);

  useEffect(() => {
    if (isOpen) void fetchHubInfo().then(setHub);
  }, [isOpen]);

  const valid = name === "" || isValidName(name);
  const cmd = commandsFor(name, hub);
  // A hub that cannot write its own Secrets still prints the commands, so the
  // second mode is offered only when the server says it works.
  const canImport = hub?.canJoinClusters === true;
  const credentialsReady =
    isValidName(name) &&
    creds.apiURL.startsWith("https://") &&
    creds.token.trim() !== "" &&
    (creds.caBundle.trim() !== "" || creds.insecure);

  const submit = async () => {
    setJoining(true);
    setJoinError(null);
    try {
      setJoined(
        await joinCluster({
          name,
          apiURL: creds.apiURL.trim(),
          token: creds.token.trim(),
          caBundle: creds.caBundle.trim() || undefined,
          insecureTLS:
            creds.caBundle.trim() === "" ? creds.insecure : undefined,
        }),
      );
    } catch (e) {
      setJoinError(e instanceof Error ? e.message : String(e));
    } finally {
      setJoining(false);
    }
  };

  if (!isOpen) return null;

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
        height={620}

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
            The name heads the cluster's column in the matrix, and it names the
            Secret this hub reads. A cluster that publishes a console banner
            shows that banner instead, and keeps this name in exports, metrics
            and the API.
          </Content>
          <FormGroup
            label="Cluster name"
            fieldId="cc-new-cluster-name"
            className="cc-name-field"
          >
            <TextInput
              id="cc-new-cluster-name"
              value={name}
              type="text"
              placeholder="prod-emea"
              validated={valid ? "default" : "error"}
              onChange={(_e, v) => setName(v)}
              aria-label="Name for the new cluster"
            />
            <HelperText>
              <HelperTextItem variant={valid ? "default" : "error"}>
                {valid
                  ? "Lower-case letters, digits and dashes."
                  : "Lower-case letters, digits and dashes, up to 63 characters."}
              </HelperTextItem>
            </HelperText>
          </FormGroup>
          {name === "" && mode === "manual" && (
            <Content component="p" className="cc-step-note">
              Leave it empty to read the commands with {PLACEHOLDER} where the
              name goes.
            </Content>
          )}

          <FormGroup
            label="Import mode"
            fieldId="cc-import-mode"
            isRequired
            className="cc-join-field"
          >
            <FormSelect
              id="cc-import-mode"
              value={mode}
              onChange={(_e, v) => setMode(v as ImportMode)}
              aria-label="How to join this cluster"
            >
              <FormSelectOption
                value="manual"
                label="Run the commands myself"
              />
              <FormSelectOption
                value="credentials"
                label="Enter the API URL and token for the existing cluster"
                isDisabled={!canImport}
              />
            </FormSelect>
            <HelperText>
              <HelperTextItem>
                {canImport
                  ? "The second way lets this hub prepare the cluster for you."
                  : "This hub cannot write cluster Secrets, so it only prints the commands."}
              </HelperTextItem>
            </HelperText>
          </FormGroup>
        </WizardStep>

        {mode === "credentials" ? (
          <WizardStep
            name="Credentials"
            id="cc-wiz-creds"
            footer={{ isNextDisabled: !credentialsReady }}
          >
            <CredentialsForm
              apiURL={creds.apiURL}
              token={creds.token}
              caBundle={creds.caBundle}
              insecure={creds.insecure}
              onChange={(patch) => setCreds((prev) => ({ ...prev, ...patch }))}
            />
          </WizardStep>
        ) : (
          <WizardStep name="Apply the manifests" id="cc-wiz-apply">
            <Content component="p">
              Run this <strong>on the cluster you want to compare</strong>, not
              on the hub:
            </Content>
            <CopyableCommand command={cmd.apply} label="apply" />
            <Content component="p" className="cc-step-note">
              It creates a read-only ServiceAccount, binds it to{" "}
              <code>cluster-reader</code>, and asks for a long-lived token. It
              writes nothing else and carries no credential.{" "}
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
        )}

        {mode === "credentials" ? (
          <WizardStep
            name="Join"
            id="cc-wiz-join"
            footer={{
              nextButtonText: joined ? "Next" : "Join the cluster",
              // Once the cluster is joined, the way on must not depend on the form
              // still being valid: the work is done.
              isNextDisabled: joining || (!joined && !credentialsReady),
              onNext: joined ? undefined : submit,
            }}
          >
            {joining && (
              <Content component="p">
                <Spinner size="md" aria-label="Joining" /> Preparing {name} and
                storing its credentials.
              </Content>
            )}
            {joinError && (
              <Alert
                variant="danger"
                isInline
                title="The cluster was not joined"
                actionLinks={
                  <AlertActionLink onClick={() => setMode("manual")}>
                    Run the commands myself instead
                  </AlertActionLink>
                }
              >
                {joinError}
              </Alert>
            )}
            {joined && (
              <>
                <Alert
                  variant="success"
                  isInline
                  isPlain
                  title={
                    joined.created
                      ? `${joined.name} joined`
                      : `${joined.name} credentials replaced`
                  }
                />
                <List className="cc-join-actions">
                  {joined.actions.map((action) => (
                    <ListItem key={action}>{action}</ListItem>
                  ))}
                </List>
                {joined.warnings?.map((warning) => (
                  <Alert
                    key={warning}
                    variant="warning"
                    isInline
                    isPlain
                    title={warning}
                  />
                ))}
              </>
            )}
            {!joining && !joined && !joinError && (
              <Content component="p" className="cc-prose">
                The hub will create the namespace, the read-only ServiceAccount
                and its binding on <code>{creds.apiURL || "the cluster"}</code>,
                read the token it mints, and store that token here. Nothing is
                written until you press Join the cluster.
              </Content>
            )}
          </WizardStep>
        ) : (
          <WizardStep name="Register on the hub" id="cc-wiz-register">
            <Content component="p">
              Read the token on that same cluster, then create the labeled
              Secret here on the hub:
            </Content>
            <CopyableCommand command={cmd.register} label="register" />
            <Content component="p" className="cc-step-note">
              The first two lines run on the cluster being joined, the last two
              on the hub. Use the external API URL, which is what{" "}
              <code>oc whoami --show-server</code> reports, so the certificate
              rows report the endpoint clients really use.
            </Content>
          </WizardStep>
        )}

        <WizardStep
          name="Finish"
          id="cc-wiz-done"
          // Nothing is left to cancel on the last step, and two buttons that both
          // shut the wizard is one too many.
          footer={{
            nextButtonText: "Close",
            onNext: onClose,
            isCancelHidden: true,
          }}
        >
          <Alert
            variant="success"
            isInline
            isPlain
            title={
              joined
                ? `${joined.name} is joined and a scrape is running`
                : `${cmd.cluster} appears in the matrix after the next scrape`
            }
          />
          <Content component="p" className="cc-prose">
            {joined
              ? "Give the scrape a moment, then look at Compare."
              : "The hub polls on its own interval, ten minutes by default. To see the cluster now, start a scrape."}
          </Content>
          {onRefresh && (
            <Button
              variant="secondary"
              onClick={() => {
                onRefresh();
                onClose();
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
  );
}

// AddClusterButton is the launcher. It sits on the Docs page and in the empty
// state of a hub with no clusters yet, which is the other moment it is wanted.
export function AddClusterButton({
  onClick,
  variant = "primary",
}: {
  onClick: () => void;
  variant?: "primary" | "secondary" | "link";
}) {
  return (
    <Button variant={variant} icon={<PlusCircleIcon />} onClick={onClick}>
      Add a cluster
    </Button>
  );
}
