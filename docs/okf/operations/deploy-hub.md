---
type: Playbook
title: Deploy the hub
description: Install Periscope on a hub cluster with the charts/periscope Helm chart, then check the empty matrix.
resource: https://github.com/croz-ltd/periscope/tree/master/charts/periscope
tags: [install, helm, hub, operations]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
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
* Helm, and rights to create a namespace, cluster-scoped RBAC and a Route.
* A clone of `github.com/croz-ltd/periscope`, for the chart.

# Steps

1. Install the chart:

   ```bash
   helm install periscope charts/periscope --set version=latest
   ```

   Set `version` to a release tag (for example `1.9.0`) to pin the image.
   The other values are in [Helm values](../configuration/helm-values.md).

2. Open the Route in the `periscope` namespace and sign in with your OpenShift
   account.

3. Expect an empty matrix with an **Add a cluster** button. A new hub has no
   clusters until you join one. Continue with
   [Join a cluster](join-cluster.md).

# What the chart creates

| Object | Purpose |
|---|---|
| Namespace `periscope` | The hub namespace. The cluster Secrets live here. |
| Deployment | The `periscope` container and the `oauth-proxy` sidecar |
| ServiceAccount, Role, RoleBinding | Read the labeled Secrets. Write them when `allowClusterImport` is on. |
| ClusterRoleBinding to `system:auth-delegator` | Lets the proxy check tokens and access |
| PVC (1Gi) | The SQLite database at `/data/periscope.db` |
| Service and Route | The UI and the API, through the proxy |
| Secret for the proxy session | The cookie secret of `oauth-proxy` |
| ConfigMap `periscope-groups` | An example row grouping. See [Grouping the matrix](../configuration/groups-configmap.md). |

# Run without a cluster install

To try Periscope against your current kubeconfig context, run the container:

```bash
docker run --rm -p 8080:8080 \
  -v "$PWD/data:/data" \
  -v "$HOME/.kube:/root/.kube:ro" \
  crozltd/periscope:latest serve --namespace periscope
```

The UI is on `http://localhost:8080`. The images are built for linux/amd64 only.
On Apple silicon, add `--platform linux/amd64`.

# Check before a real fleet

* Confirm the Portworx and Dell field paths against your clusters
  (`oc get storagecluster,containerstoragemodule -A -o yaml`). Override them with
  `--config` if they differ. See [CR extractors](../configuration/cr-extractors.md).
* Watch for extractor errors, not only empty cells. An empty cell alone does
  not prove that a component is absent.
