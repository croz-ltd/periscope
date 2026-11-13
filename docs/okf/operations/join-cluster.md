---
type: Playbook
title: Join a cluster
description: Add a cluster to the fleet from the UI wizard, with one oc command, or with the periscope-join chart for GitOps.
tags: [join, onboarding, operations, secrets]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md#join-a-cluster-from-the-ui
    title: "README: Join a cluster"
  - id: notes
    resource: https://github.com/croz-ltd/periscope/blob/master/charts/periscope-join/templates/NOTES.txt
    title: periscope-join chart NOTES
---

# What a join creates

Every path ends with the same five things:

On the **joined cluster**:

1. Namespace `periscope`
2. ServiceAccount `periscope-reader`
3. ClusterRoleBinding to `cluster-reader` (read-only)
4. Secret `periscope-reader-token`, a long-lived token for that ServiceAccount

On the **hub**:

5. A Secret named after the cluster, with `apiURL` and `token`, and the label
   `periscope.io/cluster=true`. See [The cluster Secret](../configuration/cluster-secret.md).

The hub reads the new cluster on the next scrape. Click **Refresh** to read it now.

# Choose a path

| Path | Best for | Needs |
|---|---|---|
| A. UI wizard, import mode | One-off joins by an admin | A token with admin rights on the new cluster, and `allowClusterImport=true` on the hub |
| B. UI wizard, manual mode | Joins where the hub stays read-only | Access to both clusters with `oc` |
| C. One command | Scripts | `oc` on the new cluster, then two commands on the hub |
| D. `periscope-join` chart | GitOps | Argo CD or Helm on the new cluster |

# A. UI wizard, import mode

1. Open **Docs, Clusters**, then **Add a cluster**. A new hub shows the same wizard on its
   empty page.
2. Enter the cluster name. It becomes the Secret name and the column name.
   Use a DNS-1123 label, for example `prod-emea`.
3. Choose **Enter the API URL and token for the existing cluster**.
4. Enter the external API URL (`oc whoami --show-server`) and a token with
   admin rights (`oc whoami -t`). Add the CA bundle of the API server. The wizard
   refuses to continue without one unless you tick insecure TLS. Use that for
   labs only: it turns off TLS checks for the import and for every scrape.
5. Submit. The wizard shows each step: namespace, ServiceAccount, binding,
   token, hub Secret, first scrape.

The pasted token is used for these calls only. The hub never stores it. Every
step is idempotent, so a second import of the same cluster replaces its
credentials. Use this to rotate a token.[^readme]

# B. UI wizard, manual mode

Choose **Run the commands myself**. The wizard prints two commands, filled in
with the address, namespace and label of this hub. Run the first on the new
cluster and the second on the hub.

# C. One command

On the cluster to join:

```bash
oc apply -f <(curl -sH "Authorization: Bearer $(oc whoami -t)" \
  "https://<hub-route>/yaml/new-cluster?name=prod-emea")
```

The document starts with the two hub-side commands as comments. Run them on the
hub to create and label the Secret. With `publicJoinYAML=true` on the hub, the
`curl` is not necessary: `oc apply -f https://<hub-route>/yaml/new-cluster?name=prod-emea`.

# D. The periscope-join chart

```bash
helm install periscope-join charts/periscope-join
```

The chart has no values. Its NOTES print the commands for the hub:[^notes]

```bash
API_URL=$(oc whoami --show-server)
TOKEN=$(oc -n periscope get secret periscope-reader-token \
  -o jsonpath='{.data.token}' | base64 -d)

# on the hub
oc -n periscope create secret generic prod-emea \
  --from-literal=apiURL="$API_URL" --from-literal=token="$TOKEN"
oc -n periscope label secret prod-emea periscope.io/cluster=true
```

# Optional: column order

```bash
oc -n periscope label secret prod-emea periscope.io/order=10
```

Lower numbers sort further left. Clusters with no order label sort to the right,
then by name.

# Optional: a friendly column title

Create a `ConsoleNotification` named `cluster-name` on the joined cluster. Its
text and colours become the column title. The joined name stays in the tooltip,
the exports and the API.

# Check

* The new column shows in the matrix after the next scrape.
* A `stale` badge or an error icon on the column header gives the reason. See
  [Troubleshooting](troubleshooting.md).

[^readme]: README: Join a cluster
[^notes]: periscope-join chart NOTES
