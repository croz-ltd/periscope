---
type: Kubernetes Secret
title: The cluster Secret
description: The labeled Secret in the hub namespace that registers one cluster, with its API URL, its read-only token and an optional CA bundle.
tags: [secret, registry, join, configuration]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: registry
    resource: https://github.com/croz-ltd/periscope/blob/master/internal/cluster/registry.go
    title: Cluster registry code
  - id: design
    resource: https://github.com/croz-ltd/periscope/blob/master/DESIGN.md#architecture-resolved
    title: "DESIGN.md: Cluster registry"
---

# Purpose

The Secrets are the cluster registry. One Secret in the hub namespace is one
cluster in the matrix. There is no other list of clusters.[^design]

# Schema

| Part | Key | Required | Meaning |
|---|---|---|---|
| `metadata.name` | | Yes | The cluster name in the matrix, the exports, the metrics and the API |
| label | `periscope.io/cluster: "true"` | Yes | Marks the Secret as a cluster. Key and value follow `--label-key` and `--label-value`. |
| label | `periscope.io/order` | No | An integer. Lower sorts further left. Clusters without it sort after, by name. |
| data | `apiURL` | Yes | The external API URL, for example `https://api.prod-emea.example.com:6443` |
| data | `token` | Yes | The read-only ServiceAccount token from the joined cluster |
| data | `caBundle` | No | PEM CA bundle for the API server certificate |

Rules from the code:[^registry]

* A labeled Secret with no `apiURL` or no `token` is skipped, with a `warn` in
  the log.
* With no `caBundle`, the hub does not verify the TLS certificate of that
  cluster, and logs a `warn`. Add the CA bundle for production clusters.
* A `periscope.io/order` value that is not an integer is ignored, with a `warn`.

# Examples

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: prod-emea
  namespace: periscope
  labels:
    periscope.io/cluster: "true"
    periscope.io/order: "10"
type: Opaque
stringData:
  apiURL: https://api.prod-emea.example.com:6443
  token: <read-only token>
  caBundle: |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
```

Delete the Secret to remove the cluster. See
[Remove a cluster](../operations/remove-cluster.md).

[^registry]: Cluster registry code
[^design]: DESIGN.md: Cluster registry
