# Periscope

[![CI](https://github.com/croz-ltd/periscope/actions/workflows/ci.yml/badge.svg)](https://github.com/croz-ltd/periscope/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/croz-ltd/periscope.svg)](https://pkg.go.dev/github.com/croz-ltd/periscope)
[![Docker Hub](https://img.shields.io/docker/v/crozltd/periscope?label=docker&sort=semver)](https://hub.docker.com/r/crozltd/periscope)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Periscope shows version and configuration drift across a fleet of OpenShift
clusters in one view.

Once you run more than a handful of clusters, "which cluster is behind on what?"
becomes hard to answer. The console shows one cluster at a time, `oc` loops do not
scale, and some of the versions that matter most are not in any single view: the CSI
driver version buried inside a Portworx or Dell custom resource, the kubelet on a
straggler node, the API server certificate that expires in three weeks.

Periscope is a single Go binary that scrapes every cluster on an interval, keeps the
full history in an embedded SQLite database, and serves a comparison matrix with a
REST API, CSV/JSON export and Prometheus metrics.

![The Compare page: OpenShift release, update channel, kubelet, MachineConfigPool
and certificate rows across eight clusters, with the clusters that are behind shaded
red and one cluster flagged stale](docs/screenshot.png)

*Every screenshot on this page is the real UI in its glass [theme](#themes), running
against the synthetic fleet described in
[Working on the UI without a cluster](#working-on-the-ui-without-a-cluster).*

## Contents

- [What it shows](#what-it-shows): the matrix, drift, upgrade readiness, history
- [How it works](#how-it-works): the pull model in one diagram
- [Getting started](#getting-started): deploy the hub, sign-in, MCP for AI assistants, migrating from 1.x, join clusters, remove them
- [Configuration](#configuration): grouping the matrix, covering new components
- [CLI](#cli): commands, flags, environment variables
- [API and metrics](#api-and-metrics): REST endpoints, exports, Prometheus
- [Logs](#logs): what each level says
- [Development](#development): build, run the UI on mock data
- [Contributing](#contributing) and [License](#license)

## What it shows

Per cluster, in one matrix:

| | |
|---|---|
| Platform | OpenShift release and channel, node kubelet versions, MachineConfigPool state |
| Capacity | CPU cores and memory summed over all nodes: total (also split into masters, workers and other), used (pod requests) and free. With OpenShift Virtualization also VM vCPUs and VM memory, from the allocation ratio and memory overcommit |
| Workloads | pod counts across all namespaces: total, running and crash-looping |
| Operators | every OLM-installed operator, keyed by stable package name (auto-discovered, no configuration) |
| Storage | default StorageClass, PV/PVC counts per storage class, Portworx and Dell CSI operator and managed driver versions |
| Virtualization | OpenShift Virtualization version, VM counts by state, VM snapshots per storage class, VM templates |
| Certificates | API server and ingress certificate expiry, colour-coded by urgency |
| Managed workloads | the Grafana version `grafana-operator` actually runs, which its own operator version does not tell you |

There are two views over the same data: **Compare**, for version and configuration
drift, and **Statistics**, for fleet counts and capacity.

![The Statistics page: node counts, PV and PVC counts per storage class and virtual
machine counts per cluster](docs/screenshot-statistics.png)

Statistics also reads as bar charts, one card per row, which answers a different
question: the table says what the number is on a given cluster, and the chart says
which clusters carry the fleet. Rows that hold something other than a count, such as
the release a cluster is offered, stay in the table and are named as skipped.

![The Statistics page as charts: horizontal bar charts of node counts, PVC and PV
counts per storage class, and virtual machine counts, one card per row](docs/screenshot-charts.png)

A third view reads the same rows as history. Pick a window of 1, 2, 5, 7, 14 or 30
days and each card becomes a line per cluster, so a count that moved last Tuesday
says so. The values come from the stored snapshots, carried forward between them: a
scrape that found no change still means the number held. A cluster that joined inside
the window starts its line where it joined rather than at zero.

![The Statistics page as a timeline: step lines per cluster over seven days, for node
counts and volume counts, with a timeframe picker](docs/screenshot-timeline.png)

Both pages have a component search and a **Manage view** dialog for taking columns out
of a matrix too wide to read. Both are client-side, and the cluster selection is
remembered in the browser's `localStorage`, so it is per reader rather than per fleet:
hidden clusters still count toward the reference version, and the exports, metrics and
`report` output still cover everything joined.

![The Compare page filtered to "graf": the Grafana Operator row sits at the fleet's
leading version on almost every cluster, while the Grafana row it manages spans three
versions, two majors apart](docs/screenshot-search.png)

Those two rows are the reason the managed-workload extractors exist. The operator is at
the leading version nearly everywhere, and the Grafana it runs still ranges over two
major versions. They are different facts, and reading the first tells you nothing about
the second.

### Themes

The masthead's settings menu carries PatternFly's three theming axes, remembered per
browser in `localStorage` like the cluster selection: colour scheme (follow the system,
light, dark), theme (default or Project Felt), and contrast (default, high contrast, or
the translucent glass used for the screenshots here). Nothing about it is per fleet, so
readers of the same hub can each have their own.

### How drift is decided

The baseline for each component is the highest version seen across your fleet. That
cluster renders green and the ones behind it shade red, darker with a bigger gap. This
is intra-fleet drift, so Periscope tells you whether your clusters agree with each
other, not whether they match the newest upstream release. A fleet that is uniformly
outdated shows all green, which is the intended answer.

A component that is not installed on a cluster renders as "not installed" and is left
out of the baseline, so a partial rollout stays visible without being scored as drift.

Version parsing is tolerant: a leading `v` is stripped and build metadata ignored. If
a value still does not parse, the cell renders neutral grey with the raw string shown,
because a lexical comparison of something that is not a version is worse than no
comparison at all.

When a scrape fails or times out, the last good snapshot is kept and the cluster gets
a `stale since HH:MM` badge with the error reason, so you can tell stale data from
current data.

### What can actually be upgraded

Being behind the fleet only matters if there is somewhere to go, so Periscope also
reports what each cluster can do about it:

- **Update available**: the newest release the cluster is offered. The cluster asks the
  update service on its own channel, so Periscope reads this from its ClusterVersion and
  needs no egress from the hub.
- **Upgrade blocked**: an `Upgradeable=False` condition, with the reason and message.
  This is the difference between a cluster that is behind and one that is stuck.
- **Update in progress**: set while an upgrade runs. It explains a version that
  disagrees with the rest of the fleet for the next hour.
- **Operator updates pending** and **InstallPlans awaiting approval**: updates OLM
  resolved but did not apply. On manual approval these sit unnoticed for months, and
  they are the usual reason a cluster falls behind.

### Changes and time travel

Periscope keeps every scrape, so the **Changes** page reads that history back
as an audit log: what appeared, what was upgraded, what was uninstalled, and when a
cluster stopped answering. A scrape that found the fleet exactly as it was records
nothing, so the feed holds events rather than heartbeats.

The calendar beside it marks the days something happened, darker as more changed.
Pick a day to read that day's events, then **View the matrix as it was** to load the
whole matrix as of that moment, exports included. A banner says you are looking at
history until you leave it.

![The Changes page: a month calendar with the busy days shaded, beside a feed of
operator upgrades, an installed operator, and a cluster that stopped answering and
recovered](docs/screenshot-changes.png)

Two silences are deliberate, because a feed nobody trusts is a feed nobody reads.
An unreachable cluster reports nothing, so its components are not filed as removed and
re-added around every outage. Periscope measures changes against the last *successful*
scrape, so an upgrade during an outage is still reported when the cluster comes back. And a scrape with a partial error cannot tell "uninstalled" from
"could not read", so removals wait for a clean scrape.

Counters that move on nearly every scrape (VM totals, snapshot volumes) are recorded
but hidden behind the **Include counters** switch, and the calendar counts them the
same way the feed does. A day's colour is how much really changed. A day whose only
news was counters moving gets a dot instead, so it reads as neither empty nor busy. Turning counters on folds them into the colour, because then
they are what you came to look at.

The feed is derived from the snapshot history rather than being source data, so a
correction to how changes are detected also has to reach the events already
recorded. Each release stamps the logic it used. When that logic changes, the next
start re-derives the last 90 days from stored history. The rebuild runs in the
background (tens of seconds on a fleet with months of history) and swaps in when it
commits, so the pod serves throughout and nothing is half-replaced.

### Columns named by the clusters themselves

If a cluster carries a `ConsoleNotification` named `cluster-name`, its text and
colours head that column, so the matrix labels clusters the way their own operators
already label them in the OpenShift console. The joined name stays in the tooltip and
in exports, metrics and the API. Clusters without one keep their joined name.

Each column head is also a link. Periscope reads the cluster's web console address
from its `Console` config object (the same address `oc whoami --show-console`
reports), so clicking the header opens that cluster's console in a new tab: see the
drift, then open the cluster that has it. Reading the address from the cluster rather
than deriving it from the API URL keeps custom console routes and non-default `apps`
domains working. A cluster installed without the console capability reports no
address, and its header stays plain text.

## How it works

```
                    ┌─────────── hub cluster ───────────┐
                    │  periscope pod                    │
labeled Secrets ───►│   scheduler ──► SQLite (PVC)      │──► matrix UI + REST/CSV/metrics
(one per cluster)   │      │           full history     │
                    └──────┼────────────────────────────┘
                           │ read-only SA token per cluster
              ┌────────────┼────────────┬────────────┐
           cluster A    cluster B    cluster C    cluster D ...
```

The hub pulls from every cluster, so there are no per-cluster agents. Clusters are
discovered from labeled Secrets in the hub namespace. The Secret's name is the
cluster's display name and its data is `apiURL` plus `token`, with optional
`caBundle` or `insecureTLS`. There is no implicit "local" cluster: Periscope starts
with an empty matrix, and the hub's own cluster is joined like any other if you want
it compared.

Page loads read the cache and never fan out live. [DESIGN.md](DESIGN.md) covers the
reasoning behind these choices and what was left for later.

## Getting started

### Deploy the hub

```bash
helm install periscope charts/periscope --set version=latest
```

That installs the app, the RBAC it needs, a PVC, a Route and an `OAuthClient` for MCP
tokens. `OAuthClient` is cluster-scoped, so the install needs cluster-admin. The matrix
starts empty, because a hub has no clusters until you join one.

### Sign-in and access

Every request needs an OpenShift identity, and that identity needs `get` on services in
the hub namespace. Grant it with `oc -n periscope policy add-role-to-user view <user>`.

- **In a browser** the hub sends you to the OpenShift login and keeps an encrypted
  session cookie for up to 24 hours. The hub ServiceAccount is the OAuth client of this
  login, so it needs no cluster-scoped object.
- **From a script, Prometheus or an MCP client** send any token the API server accepts
  as `Authorization: Bearer <token>`: `oc whoami -t`, a ServiceAccount token, or an MCP
  token from `/mcp/token`.

The hub checks each token with a `TokenReview` and the right with a
`SubjectAccessReview` (it binds `system:auth-delegator` for both). It reuses each answer
for one minute, so a revoked token or a removed role stops working within a minute.

### Use Periscope from an AI assistant (MCP)

`/mcp` is an MCP server (Streamable HTTP) with these tools:

| Tool | Answers |
|---|---|
| `list_clusters` | the clusters, their last scrape, and whether it worked or is stale |
| `list_components` | the component keys, filtered by text or group |
| `get_component` | one component on every cluster, with its drift state and tooltip details |
| `get_cluster` | everything about one cluster, or one group of it |
| `fleet_totals` | sum, minimum, maximum and average of numeric rows across the fleet |
| `find_drift` | versions behind, configuration that differs, certificates close to expiry, failed scrapes |
| `get_history` | the history of up to 10 components, over 1 to 30 days |
| `list_changes` | the change feed |
| `refresh` | start a scrape now |

Every read tool takes `at` (RFC3339) to answer about the past. `fleet_totals` does the
arithmetic on the hub, so "how many vCPUs does the fleet have" comes back exact.

To connect a client, open **Docs, MCP** in the UI and click **Get an MCP token** (or
**user menu, MCP access token**). It issues an OpenShift OAuth token of your own user
and brings you back to the same page, which shows the token once and fills it into the
examples. Put it in the environment, because most clients read it from there and their
configuration then holds no secret:

```bash
export PERISCOPE_TOKEN='<token>'
```

The page has a tab per client, each with a command and a config file. In short:

| Client | Command | Config file |
|---|---|---|
| Claude Code | `claude mcp add --transport http --scope user periscope https://<hub-route>/mcp --header "Authorization: Bearer $PERISCOPE_TOKEN"` | `.mcp.json` or `~/.claude.json`: `"headers": {"Authorization": "Bearer ${PERISCOPE_TOKEN}"}` |
| Codex CLI | `codex mcp add periscope --url https://<hub-route>/mcp --bearer-token-env-var PERISCOPE_TOKEN` | `~/.codex/config.toml`: `[mcp_servers.periscope]` with `url` and `bearer_token_env_var = "PERISCOPE_TOKEN"` |
| Copilot CLI | `copilot mcp add --transport http periscope https://<hub-route>/mcp --header "Authorization: Bearer $PERISCOPE_TOKEN"` | `~/.copilot/mcp-config.json`: `"type": "http"`, `url`, `headers` with the token itself |
| Gemini CLI | `gemini mcp add --scope user --transport http --header "Authorization: Bearer $PERISCOPE_TOKEN" periscope https://<hub-route>/mcp` | `~/.gemini/settings.json`: `httpUrl` and `"headers": {"Authorization": "Bearer $PERISCOPE_TOKEN"}` |
| VS Code | `code --add-mcp "{\"name\":\"periscope\",\"type\":\"http\",\"url\":\"https://<hub-route>/mcp\",\"headers\":{\"Authorization\":\"Bearer $PERISCOPE_TOKEN\"}}"` | `.vscode/mcp.json`: `servers.periscope` with `"type": "http"` and an `inputs` password prompt for the token |

The token is valid for `mcpTokenDays` (30 days by default). Its scopes are `user:info`
and `user:check-access`, so it is of no use against the API server. Periscope checks
your own rights on every call, and the token stops working when you lose them. The page
also shows how to revoke it: `oc delete useroauthaccesstoken <name>`. The token reaches
the page in an encrypted cookie that only `/api/mcp/issued` reads, once, never in a URL.

The page needs the `periscope-mcp` OAuthClient, which needs the route host for its
redirect. The chart reads the cluster apps domain at install. When it cannot (`helm
template`, Argo CD), set `--set host=<hub-route-host>`. Without it, the page explains how
to use `$(oc whoami -t)` as the token instead, which works but expires with your login.

### Migrating from 1.x to 2.x

2.0.0 removes the `oauth-proxy` sidecar. The hub signs readers in itself, with the same
OpenShift login and the same access rule (`get` on services in the hub namespace). The
data, the joined clusters, the reader roles and the route address do not change.

**What changes**

| Area | 1.x | 2.x |
|---|---|---|
| Pod | `periscope` and `oauth-proxy` containers | `periscope` only |
| TLS on 8443 | served by `oauth-proxy` | served by the hub, same service-ca certificate, route stays `reencrypt` |
| Cluster-scoped objects | `ClusterRoleBinding` to `system:auth-delegator` | the same, plus the `OAuthClient` `periscope-mcp` |
| Chart values | `version`, `logLevel`, `logFormat`, `publicJoinYAML`, `allowClusterImport` | the same, plus `host` and `mcpTokenDays` |
| Browser session | `oauth-proxy` cookie | a new cookie, so every reader signs in once again |
| Logs | `oc logs deploy/periscope -c periscope` | `oc logs deploy/periscope` |
| Binary outside a cluster | no login | needs `--auth=none` (the default is `--auth=openshift`) |

**Steps with Helm**

1. Find the route host. The MCP OAuth client needs it for its redirect:

   ```bash
   oc -n periscope get route periscope -o jsonpath='{.spec.host}{"\n"}'
   ```

2. Upgrade as cluster-admin, because the chart now creates a cluster-scoped
   `OAuthClient`:

   ```bash
   helm upgrade periscope charts/periscope --reuse-values \
     --set version=2.0.1 --set host=<route host from step 1>
   ```

   `host` is optional with Helm, which reads the cluster apps domain itself. Setting it
   to the current host is the safe choice: the route keeps the address it has.

3. Check the result (see **Verify** below).

**Steps with Argo CD (or any `helm template` render)**

Argo CD renders the chart without access to the cluster, so the chart cannot look up
the apps domain or the Secrets it generated before.

1. Set the values in the Application: `version: 2.0.1` and `host: <route host>`.
   Without `host` the chart creates no `OAuthClient`, and `/mcp/token` only explains
   `oc whoami -t`.
2. Allow the new cluster-scoped kind. The AppProject needs it in
   `clusterResourceWhitelist`, and the Argo CD controller needs RBAC to create it:

   ```yaml
   clusterResourceWhitelist:
     - group: oauth.openshift.io
       kind: OAuthClient
   ```

3. Keep the generated secrets stable. Each render makes new random values for the
   session secret and the MCP client secret. Tell Argo CD to leave them as they were
   created, or every sync rotates them:

   ```yaml
   spec:
     ignoreDifferences:
       - kind: Secret
         name: periscope-oauth-session
         jsonPointers: [/data]
       - kind: Secret
         name: periscope-mcp-client
         jsonPointers: [/data]
       - group: oauth.openshift.io
         kind: OAuthClient
         name: periscope-mcp
         jsonPointers: [/secret]
     syncPolicy:
       syncOptions:
         - RespectIgnoreDifferences=true
   ```

   The `OAuthClient` secret and the `periscope-mcp-client` Secret are rendered in one
   template, so they match when they are first created.
4. Sync. The Deployment rolls to one container.

**Verify**

```bash
oc -n periscope get pods                       # READY 1/1, not 2/2
oc -n periscope logs deploy/periscope | grep 'authentication on'
                                               # mcpClient=periscope-mcp
oc get oauthclient periscope-mcp               # exists
curl -sS -o /dev/null -w '%{http_code}\n' https://<route host>/api/matrix
                                               # 401 without a token
curl -sS -H "Authorization: Bearer $(oc whoami -t)" -o /dev/null \
  -w '%{http_code}\n' https://<route host>/api/matrix
                                               # 200
```

Then open the route in a browser, sign in, and open **Docs, MCP, Get an MCP token**.

**What needs no change**

- Reader and admin access: still `get` (read) and `create` (purge) on services in the
  hub namespace.
- Scripts, exports and Prometheus scrapes that send a bearer token, on the same route
  or service port 8443.
- The joined clusters and their Secrets, the database, the grouping ConfigMap.
- Network: the hub calls the `oauth-openshift` route for the token exchange, as the
  proxy did before.

**If the login fails** after the upgrade, the hub log names the cause. A TLS error on
the token exchange means the hub does not trust the certificate of the
`oauth-openshift` route. Mount the CA and pass it with `--oauth-ca-file`. To go back,
roll back to 1.10.1 (`helm rollback periscope`, or the previous revision in Argo CD) and
delete the client with `oc delete oauthclient periscope-mcp`.

### Join a cluster from the UI

Open the UI and go to **Docs, Clusters**, **Add a cluster**. The same wizard is offered in the
empty state of a hub with no clusters yet, which is where you land on a fresh install.

![The Add a cluster wizard: a cluster name, an import mode select set to "Enter the API
URL and token for the existing cluster", and the four steps of that
path](docs/screenshot-add-cluster.png)

The **Import mode** select decides who does the work:

- **Enter the API URL and token for the existing cluster.** The hub does the whole join:
  it creates the namespace, the read-only ServiceAccount and the `cluster-reader`
  binding on that cluster, reads the token the cluster mints, stores it, and starts a
  scrape. It reports each step, and every one of them is idempotent, so re-importing a
  cluster whose token was rotated replaces the credentials.
- **Run the commands myself.** The wizard prints the two commands instead, built from
  this hub's own address, namespace and label, so nothing in them has to be edited by
  hand. It keeps the two clusters straight, because one command runs on the cluster
  being joined and the other on the hub.

The token you paste for the first mode needs to create a namespace, a ServiceAccount and
a ClusterRoleBinding, so in practice a cluster-admin token. It is used for those calls
and dropped: never written to the store, never logged, never returned. What the hub
keeps is the read-only token, which is the one every scrape uses. The provisioning runs
with that pasted token and never with the hub's own credentials, so the app holds no
privilege you do not.

Storing the credentials is the one thing Periscope writes, and it needs `create` and
`update` on secrets in its own namespace. `allowClusterImport` grants that and defaults
to on. Turn it off to keep the hub read-only, and the wizard offers the manual mode
alone, because the UI asks the hub what it can do rather than finding out halfway
through:

```bash
helm upgrade periscope charts/periscope --set allowClusterImport=false
```

### Join a cluster from the command line

The hub serves the same four resources the wizard applies, so one command onboards a
cluster with no chart and no UI:

```bash
# on the cluster you want to compare
oc apply -f <(curl -sH "Authorization: Bearer $(oc whoami -t)" \
  "https://<hub-route>/yaml/new-cluster?name=prod-emea")
```

The document opens with the two hub-side commands it leaves for you, filled in with the
name you passed and with the namespace and label this hub actually watches. It creates a
read-only ServiceAccount, binds it to `cluster-reader`, and asks for a long-lived token.
It carries no credential: the token is minted by the cluster after the apply, and it
reaches the hub only when you copy it across.

`name` is optional and must be a DNS-1123 label. Without it the instructions show
`<CLUSTER_NAME>` where the name goes, and the resources carry no cluster label.

For a GitOps install, `charts/periscope-join` creates the same four resources, and its
`NOTES` print the same two commands:

```bash
helm install periscope-join charts/periscope-join

# then, on the hub, with the API URL and token that chart's NOTES report
oc -n periscope create secret generic prod-emea \
  --from-literal=apiURL="$API_URL" --from-literal=token="$TOKEN"
oc -n periscope label secret prod-emea periscope.io/cluster=true

# optional: column order in the matrix (lower sorts further left)
oc -n periscope label secret prod-emea periscope.io/order=10
```

Whichever way you join a cluster, the hub picks it up on the next scrape.

The `curl` is there because every route of the hub needs a login. To let
`oc apply -f https://<hub-route>/yaml/new-cluster?name=prod-emea` work on its own,
install the hub with `--set publicJoinYAML=true`, which serves `/yaml/` without one.
Weigh that against what the document reveals: no credentials, but the namespace and
label this hub reads.

### Remove a cluster, and purge what it left behind

A cluster leaves the fleet when its credential Secret goes:

```bash
oc -n periscope delete secret prod-emea
```

Nothing scrapes it after that, but the history stays. That is deliberate, because a
cluster taken out for an afternoon should come back with its past intact, and the
Changes page and the timeline still answer questions about it. The cost is a column in
the matrix that is greyed out as stale and never recovers.

**Actions -> Purge stale data** removes it. The dialog lists every cluster the database
holds history for that no longer has a Secret, with how much would go, and takes the
ones you tick. It is not undoable: rejoining the cluster starts its history again from
the next scrape.

Two things gate the action. The reader must be able to `create` services in the hub
namespace, which is a right an administrator of that namespace holds and an ordinary
reader does not, and it is checked on every request rather than once at login. And a
cluster that still has its Secret cannot be purged at all, by the UI or by the API: the
configuration decides which clusters exist, and purging only clears up after it.

Readers without that right never see the action, because
[`GET /api/admin/clusters`](#api-and-metrics) answers them `403` and the UI reads the
status code.

**If the action is missing for you, that endpoint is what to check:**

```bash
curl -sS -H "Authorization: Bearer $(oc whoami -t)" "https://<hub-route>/api/admin/clusters"
```

Read the body, not just the code. The message names the cause, and the same line is
logged at `warn`, so
`oc -n periscope logs deploy/periscope | grep 'refused an admin request'` answers it
for a request made from the browser.

- `200` and the action is in the menu. It opens a dialog that says so when there is
  nothing to purge, so an empty result is not the same as a missing action.
- `401` means the token is expired, revoked or missing. `oc whoami -t` gives a fresh one.
- `403` that names `create on services` means you do not hold that right in the hub
  namespace. Check with `oc auth can-i create services -n periscope`, and grant it with
  `oc -n periscope policy add-role-to-user admin <user>` (the built-in `admin` and
  `edit` roles both include it).
- `403` that says the access review failed means the hub did not get an answer from
  the API server. The log line carries the error.
- `503` and the hub cannot reach a cluster registry, so it cannot tell an administrator
  from a reader and refuses to guess.

### Run the container

```bash
docker run --rm -p 8080:8080 \
  -v "$PWD/data:/data" \
  -v "$HOME/.kube:/root/.kube:ro" \
  crozltd/periscope:latest serve --auth=none --namespace periscope
```

The UI is then on http://localhost:8080. Without joined clusters the matrix is
empty, which is the expected starting state. `--auth=none` turns the login off, which
is right on your own machine and wrong anywhere else: anyone who reaches the port reads
the fleet.

Images live at [`crozltd/periscope`](https://hub.docker.com/r/crozltd/periscope):
`:latest` and `:<tag>` for releases, `:edge` for the tip of `master`. They are built
for linux/amd64 only, matching the clusters they run on, so on an arm64 machine
(Apple silicon) add `--platform linux/amd64` to the command above and expect
emulation. Building locally with `make image` gives you a native image instead.

### Verify before you run against a real fleet

Verify the Portworx and Dell CR field paths. The defaults in
`internal/extract/crfield.go` (`status.version`, `spec.driver.configVersion`) are
best-effort. Confirm them against your clusters with
`oc get storagecluster,containerstoragemodule -o yaml`, then override them with
`--config` rather than patching the code.

Watch extractor errors rather than empty cells. The reader SA binds OpenShift's
`cluster-reader`, so missing RBAC is mostly a non-issue, but if you narrow that role a
missing rule returns `403`, which the scheduler records in the snapshot error. An empty
cell on its own does not prove that a component is absent.

## Configuration

Periscope runs with none of this: operators are discovered through OLM, and the
built-in extractors need no configuration. These are the knobs for when the defaults
are not what your fleet looks like.

### Grouping the matrix

By default each page groups rows by subsystem in a fixed order. A `periscope-groups`
ConfigMap in the hub namespace replaces that with your own sections, per page, and can
hide rows entirely:

```bash
oc apply -f examples/periscope-groups.configmap.yaml -n periscope
```

The [example](examples/periscope-groups.configmap.yaml) documents the schema:
`compare` and `statistics` take ordered groups of component keys, `hidden` removes keys
from both pages. A page you configure becomes authoritative for that page, with
anything left over collected into "Ungrouped"; a page you leave out keeps the built-in
grouping. Component keys are listed on the UI's **Docs** page, and changes are picked
up on the next matrix load.

### Covering a component Periscope does not know

Adding coverage is usually a small change. If the version you need is a nested field
in a custom resource, no code is required, just declare it in the extractor config and
pass it with `--config`:

```yaml
crExtractors:
  - key: my-csi
    display: My CSI
    kind: csi
    group: storage.example.com
    version: v1
    resource: mydrivers
    versionPath: [spec, driver, configVersion]
```

The same file overrides the built-in Portworx and Dell field paths, see
[`config/extractors.yaml`](config/extractors.yaml). Every hand-written extractor stays
enabled alongside it.

Anything a nested field cannot express means implementing the `Extractor` interface and
registering it, which is a dozen lines. See
[CONTRIBUTING.md](CONTRIBUTING.md#adding-an-extractor).

## CLI

```
periscope serve    scrape on an interval and serve the UI + API   (default)
periscope report   print the latest matrix as a text table
periscope version  print the build version
```

Flags common to `serve` and `report`:

| Flag | Default | Meaning |
|---|---|---|
| `--namespace` | pod namespace, else `periscope` | hub namespace holding the joined-cluster Secrets |
| `--label-key` / `--label-value` | `periscope.io/cluster` / `true` | label marking those Secrets |
| `--db` | `/data/periscope.db` (`DB_PATH`) | SQLite database path |
| `--stale-after` | `30m` | mark a cluster stale when its last scrape is older than this |
| `--log-level` | `info` (`LOG_LEVEL`) | `debug` \| `info` \| `warn` \| `error` |
| `--log-format` | `text` (`LOG_FORMAT`) | `text` to read by eye, `json` for a log collector |

`serve` only:

| Flag | Default | Meaning |
|---|---|---|
| `--addr` | `:8080` (`LISTEN_ADDR`) | listen address |
| `--auth` | `openshift` (`AUTH`) | `openshift`: OpenShift login and the read check. `none`: no login, local development only |
| `--tls-cert` / `--tls-key` | none | serve HTTPS with this certificate, read again when it changes |
| `--session-secret-file` | `/etc/periscope/session/session_secret` | secret that encrypts the session cookie |
| `--oauth-client-id` | `system:serviceaccount:<namespace>:periscope` | OAuth client of the UI login, its secret is the ServiceAccount token |
| `--mcp-client-id` | none | OAuthClient that issues MCP tokens on `/mcp/token` |
| `--mcp-client-secret-file` | `/etc/periscope/mcp-client/secret` | secret of `--mcp-client-id` |
| `--oauth-ca-file` | none | extra CA bundle to trust for the OAuth server, comma-separated |
| `--public-join-yaml` | `false` | serve `/yaml/new-cluster` without a login |
| `--interval` | `10m` | how often the whole fleet is scraped |
| `--timeout` | `30s` | per-cluster scrape deadline |
| `--concurrency` | `4` | clusters scraped in parallel |
| `--config` | none (`CONFIG_PATH`) | extractor config, see [Configuration](#configuration) |

## API and metrics

| Endpoint | |
|---|---|
| `GET /` | embedded PatternFly UI |
| `GET /api/matrix` | the full comparison matrix as JSON (what the UI renders) |
| `GET /api/matrix?at=<RFC3339>` | the matrix as it stood at that moment |
| `GET /api/changes` | the change feed, newest first (`from`, `to`, `cluster`, `limit`) |
| `GET /api/changes/calendar` | per-day change counts, for marking a calendar |
| `GET /api/timeline?key=<key>&days=<1,2,5,7,14,30>` | one series per cluster for those components, `at` honoured |
| `GET /api/export.csv`, `GET /api/export.json` | current matrix export, `at` honoured; `page=compare\|statistics` keeps one page's rows in its order, and `cluster=<name>` (repeatable) keeps those columns |
| `POST /api/refresh` | trigger a scrape now |
| `GET /api/version` | the version stamped into this binary |
| `POST /api/clusters` | join a cluster from `{name, apiURL, token, caBundle, insecureTLS}` |
| `GET /api/admin/clusters` | what the database holds per cluster, and whether the fleet still includes it |
| `POST /api/admin/purge` | remove the stored history of `{clusters: [...]}`, refusing any that are still joined |
| `GET /yaml/new-cluster?name=<cluster>` | the join manifests for one cluster, ready for `oc apply -f` |
| `GET /metrics` | Prometheus text exposition |
| `POST /mcp` | the MCP server, see [MCP](#use-periscope-from-an-ai-assistant-mcp) |
| `GET /mcp/token` | in a browser: issue an MCP token for the signed-in user, then open **Docs, MCP** |
| `GET /api/mcp/issued` | the token from the last `/mcp/token`, once, for the UI |
| `GET /oauth/start`, `/oauth/callback`, `/oauth/sign_out` | the browser login and sign-out |
| `GET /healthz` | liveness, the only path with no login |

Every endpoint needs a login or a bearer token and `get` on services in the hub
namespace: `401` without a valid token, `403` without the right. Everything under
`/api/admin` needs `create` on services in the hub namespace and answers `403` without it. See
[Remove a cluster](#remove-a-cluster-and-purge-what-it-left-behind).

Two gauges are exported, both suitable for alerting:

```
periscope_component_drift_severity{cluster,component,state}  # 0 on the fleet leader
periscope_cluster_stale{cluster}                             # 1 when the snapshot is stale
```

## Logs

Everything logs through `log/slog`, tagged with the subsystem that produced it
(`component=scrape`, `cluster`, `store`, `api`, `extract`), so a fleet of thirty
clusters can be read by filtering rather than by grepping prose. The chart sets
`LOG_FORMAT=json` so OpenShift's log collector indexes the fields. For a terminal, use
`text`.

**info** is what a healthy hub says, and is meant to stay readable: one line per
scrape cycle with how many clusters were degraded or unreachable, one per cluster
with its component count and duration, plus discovery results and startup
configuration. Anything that silently degrades the result is a **warn**: an
extractor that failed (with how much of the per-cluster deadline was left when it
gave up, which separates a slow cluster from a broken extractor), a labeled Secret
missing its credentials, a cluster scraped without a CA bundle, an invalid grouping
ConfigMap.

**debug** explains one scrape in full: every extractor with its duration and
component count, every resource skipped because its CRD is not installed, every
snapshot saved with how many changes it recorded, and every HTTP request. It is
the level to raise while diagnosing, and to lower again afterwards.

```
level=WARN msg="extractor failed" component=scrape cluster=erls-p extractor=olm \
  duration=12.3s remaining=-1.2s error="context deadline exceeded"
level=WARN msg="cluster scraped" component=scrape cluster=erls-p components=42 \
  extractorsFailed=5 duration=30.0s
```

That pair is what a scrape timing out mid-list looks like, which is worth
recognising: the components those extractors did not read are missing from that
snapshot, and the matrix shows them as not installed until the next good scrape.

## Development

Requires Go 1.27+ and Node 22+.

```bash
make web      # build the PatternFly UI into web/dist (embedded by the Go build)
make build    # build bin/periscope
make test     # go test ./...
make image    # docker build

# serve using your current kubeconfig context
bin/periscope serve --namespace periscope --db ./periscope.db

# one-off matrix printed to stdout (no web UI), useful in CI
bin/periscope report --db ./periscope.db
```

### Working on the UI without a cluster

The UI needs a fleet to show anything, which is a poor way to start work on it, so it
ships with a synthetic one:

```bash
make web-mock          # or: cd web/app && npm run dev:mock
```

That runs Vite with a dev-server middleware answering `/api/*` from a mock fleet of
eight clusters: two production regions, staging, dev and two edge sites, one of them
stale and one reporting an extractor error, with real drift, expiring certificates,
counts and a month of change history. No cluster, no Go server, no database.

The fixture lives in [`web/app/mock/`](web/app/mock/) and is typed against the same
interfaces the app uses for real responses, so it cannot drift from the API's shapes.
It is a Vite plugin rather than a branch in `src/`, enabled only by `--mode mock`, so a
production build cannot pick it up. Every screenshot in this README was taken from it,
which is also how they can be retaken without a fleet.

### Version strings

Every build reports the release it came from, plus metadata saying where it was built,
so a version is always traceable to both a release and a commit:

```
1.0.0             a release build of the v1.0.0 tag
1.0.0+ci-1a2b3c4  the :edge image, or a CI binary, at that commit
1.0.0+tekton-...  built on a cluster by tekton/, tagged with the image tag
1.0.0+dev         a local `make build`
```

The running value is on the UI's About page, printed by `periscope version`, and served
from `/api/version`.

[CONTRIBUTING.md](CONTRIBUTING.md) has the repository layout, how to add an extractor,
the CI workflows and how a release is cut.

[`docs/okf/`](docs/okf/) holds the same documentation as an
[Open Knowledge Format](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md)
bundle: small linked markdown concepts with YAML frontmatter, for agents and
knowledge catalogs.

## Contributing

Issues and pull requests are welcome, see [CONTRIBUTING.md](CONTRIBUTING.md). Security
reports go through the process in [SECURITY.md](SECURITY.md) rather than public issues.

## License

[Apache License 2.0](LICENSE), Copyright CROZ d.o.o.
