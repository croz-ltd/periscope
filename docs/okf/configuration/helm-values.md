---
type: Reference
title: Helm values
description: The seven values of the charts/periscope hub chart, and the static periscope-join chart.
resource: https://github.com/croz-ltd/periscope/blob/master/charts/periscope/values.yaml
tags: [helm, chart, configuration]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-13T11:30:00Z }
sources:
  - id: values
    resource: https://github.com/croz-ltd/periscope/blob/master/charts/periscope/values.yaml
    title: Hub chart values.yaml
---

# Schema

The hub chart, `charts/periscope`, has seven values. Everything else is fixed in
the templates.[^values]

| Value | Default | Meaning |
|---|---|---|
| `version` | `latest` | The tag of the `crozltd/periscope` image. Pin a release tag in production. |
| `logLevel` | `info` | `debug`, `info`, `warn` or `error`. Raise to `debug` while you diagnose a scrape. |
| `logFormat` | `json` | `json` for the cluster log collector, `text` to read by eye |
| `host` | empty | The route host. Empty means the OpenShift default, `periscope-periscope.<apps domain>`, read from the cluster. Set it for `helm template` and Argo CD: without a host the chart makes no MCP OAuth client. |
| `mcpTokenDays` | `30` | How long an MCP token from `/mcp/token` is valid |
| `publicJoinYAML` | `false` | Serve `/yaml/new-cluster` with no sign-in. The document holds no credentials but shows the namespace and label of the hub. |
| `allowClusterImport` | `true` | Let the UI join a cluster from an API URL and a token. The hub then writes Secrets in its own namespace. Set `false` to keep the hub read-only. The wizard then offers the manual mode only. |

# The join chart

`charts/periscope-join` has no values. It always creates the namespace
`periscope`, the ServiceAccount `periscope-reader`, the ClusterRoleBinding
`periscope-reader` to `cluster-reader`, and the token Secret
`periscope-reader-token`. See [Join a cluster](../operations/join-cluster.md).

# Examples

```bash
# pin a release and keep the hub read-only
helm upgrade periscope charts/periscope --set version=2.0.0 --set allowClusterImport=false

# render for Argo CD, with the route host the chart cannot read
helm template periscope charts/periscope --set host=periscope-periscope.apps.example.com

# allow `oc apply -f https://<route>/yaml/new-cluster?name=<cluster>` with no token
helm upgrade periscope charts/periscope --set publicJoinYAML=true
```

[^values]: Hub chart values.yaml
