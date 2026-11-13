---
type: Playbook
title: Deploy the hub
description: Install Periscope on a hub cluster with the charts/periscope Helm chart, then check the empty matrix.
resource: https://github.com/croz-ltd/periscope/tree/master/charts/periscope
tags: [install, helm, hub, operations]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-13T11:30:00Z }
sources:
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md#deploy-the-hub
    title: "README: Deploy the hub"
  - id: chart
    resource: https://github.com/croz-ltd/periscope/tree/master/charts/periscope/templates
    title: Hub chart templates
---

# Before you start

* An OpenShift cluster to act as the hub. It needs network access to the API
  endpoint (port 6443) of every cluster you plan to join.
* Helm, and cluster-admin: the chart creates a namespace, cluster-scoped RBAC,
  a Route and a cluster-scoped `OAuthClient`.
* A clone of `github.com/croz-ltd/periscope`, for the chart.

# Steps

1. Install the chart:

   ```bash
   helm install periscope charts/periscope --set version=latest
   ```

   Set `version` to a release tag (for example `2.0.0`) to pin the image.
   With `helm template` or Argo CD, also set `host` to the route host. The
   chart cannot read the cluster domain then, and needs the host for the MCP
   OAuth client. The other values are in [Helm values](../configuration/helm-values.md).

2. Open the Route in the `periscope` namespace and sign in with your OpenShift
   account. You need `get` on services in the `periscope` namespace:
   `oc -n periscope policy add-role-to-user view <user>`.

3. Expect an empty matrix with an **Add a cluster** button. A new hub has no
   clusters until you join one. Continue with
   [Join a cluster](join-cluster.md).

# What the chart creates

| Object | Purpose |
|---|---|
| Namespace `periscope` | The hub namespace. The cluster Secrets live here. |
| Deployment | The `periscope` container. It serves TLS and the OpenShift login itself. |
| ServiceAccount, Role, RoleBinding | Read the labeled Secrets. Write them when `allowClusterImport` is on. |
| ClusterRoleBinding to `system:auth-delegator` | Lets the hub check tokens and access |
| PVC (1Gi) | The SQLite database at `/data/periscope.db` |
| Service and Route | The UI, the API and MCP, on port 8443, reencrypt |
| Secret `periscope-oauth-session` | Encrypts the session cookie. Kept across upgrades. |
| OAuthClient `periscope-mcp` and Secret `periscope-mcp-client` | Issue MCP tokens on `/mcp/token`. Only when the host is known. |
| ConfigMap `periscope-groups` | An example row grouping. See [Grouping the matrix](../configuration/groups-configmap.md). |

# Run without a cluster install

To try Periscope against your current kubeconfig context, run the container:

```bash
docker run --rm -p 8080:8080 \
  -v "$PWD/data:/data" \
  -v "$HOME/.kube:/root/.kube:ro" \
  crozltd/periscope:latest serve --auth=none --namespace periscope
```

The UI is on `http://localhost:8080`. `--auth=none` turns the login off, so
use it on your own machine only. The images are built for linux/amd64 only.
On Apple silicon, add `--platform linux/amd64`.

# Upgrade from 1.x

2.0.0 removes the `oauth-proxy` sidecar. Apply the chart again. The data and the
joined clusters do not change. Readers sign in again once. The pod now has one
container, so `oc logs deploy/periscope` needs no `-c periscope`.

# Check before a real fleet

* Confirm the Portworx and Dell field paths against your clusters
  (`oc get storagecluster,containerstoragemodule -A -o yaml`). Override them with
  `--config` if they differ. See [CR extractors](../configuration/cr-extractors.md).
* Watch for extractor errors, not only empty cells. An empty cell alone does
  not prove that a component is absent.
