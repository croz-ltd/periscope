---
type: Architecture
title: Access and security
description: The rights Periscope holds on the hub and on each joined cluster, and how sign-in and the admin actions are checked.
tags: [security, rbac, oauth-proxy, tokens]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: design
    resource: https://github.com/croz-ltd/periscope/blob/master/DESIGN.md#access--security-resolved
    title: "DESIGN.md: Access and security"
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md#getting-started
    title: "README: Getting started"
---

# Rights at a glance

| Who | Where | Rights |
|---|---|---|
| Reader ServiceAccount | Each joined cluster | Bound to the built-in `cluster-reader` ClusterRole. Read-only, cluster-wide. |
| Periscope pod | Hub namespace | List and watch the labeled Secrets. With `allowClusterImport`, also create and update Secrets in that namespace. |
| oauth-proxy | Hub cluster | `system:auth-delegator`, to check tokens and access. |
| UI reader | Hub namespace | `get` on services, checked by the proxy. |
| Admin (purge) | Hub namespace | `create` on services, checked by the app on every request. |

# Joined clusters

* Each cluster has its own read-only ServiceAccount token. It is stored only
  in the hub Secret for that cluster and can be revoked per cluster.[^design]
* `cluster-reader` lets every extractor read new operator CRDs with no RBAC
  change. The trade-off is broad read access on every joined cluster.
* A narrower ClusterRole is possible: edit
  `charts/periscope-join/templates/clusterrolebinding.yaml`. Then a new
  extractor can need a new rule, and a missing rule shows as a `403` in the
  scrape error.

# Sign-in

* The `oauth-proxy` sidecar signs readers in through OpenShift SSO. Periscope
  has no sign-in code of its own.
* A browser session and a bearer token pass the same read check (`get` on
  services in the hub namespace).

# The admin API

* Everything under `/api/admin` (the purge action) needs `create` on services in
  the hub namespace. The app checks this with a SubjectAccessReview for the
  token of the reader, on every request.[^design]
* A request with no token gets the rights of the pod, and those do not include
  `create` on services. A path around the proxy is not a path around the check.
* Purge refuses any cluster that still has its Secret. The Secrets decide which
  clusters exist. Purge only clears up after a removal.

# The import token

The **Add a cluster** wizard can take an API URL and a token with admin rights on
the new cluster. Periscope uses that token only to create the read-only
ServiceAccount and its binding. It never stores it, never logs it and never
returns it. The hub keeps only the read-only token.[^readme]

# Public join YAML

`publicJoinYAML=true` lets anyone fetch `/yaml/new-cluster` with no sign-in. The
document holds no credentials, but it shows the namespace and the label that the
hub reads. It is off by default.

[^design]: DESIGN.md: Access and security
[^readme]: README: Getting started
