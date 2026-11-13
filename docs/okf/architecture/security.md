---
type: Architecture
title: Access and security
description: The rights Periscope holds on the hub and on each joined cluster, and how sign-in and the admin actions are checked.
tags: [security, rbac, oauth, tokens, mcp]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-13T11:30:00Z }
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
| Periscope pod | Hub cluster | `system:auth-delegator`, to check tokens (TokenReview) and access (SubjectAccessReview). |
| UI and API reader | Hub namespace | `get` on services, checked by the app on every request. |
| `periscope-mcp` OAuthClient | Hub cluster | Issues MCP tokens with the scopes `user:info` and `user:check-access` only. |
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

* Since 2.0.0 the hub signs readers in itself. Up to 1.x an `oauth-proxy`
  sidecar did it.
* A browser goes through the OpenShift login (authorization code with PKCE).
  The hub ServiceAccount is the OAuth client. The hub keeps the token in a
  session cookie encrypted with AES-GCM, for 24 hours at most.
* A script, Prometheus or an MCP client sends `Authorization: Bearer <token>`.
  Any token the API server accepts works: `oc whoami -t`, a ServiceAccount
  token, or an MCP token.
* Every request, cookie or token, passes the same two checks: a TokenReview
  (who is it) and a SubjectAccessReview (`get` on services in the hub
  namespace). Each answer is reused for one minute at most, so a revoked token
  or a removed role stops working within a minute.
* A state-changing request that a browser marks as cross-site is refused. A
  page on another site cannot use the session cookie.

# MCP tokens

* `/mcp/token` issues an OpenShift OAuth token of the signed-in user through
  the `periscope-mcp` OAuthClient. It is valid for `mcpTokenDays` (30 by
  default).
* Its scopes are `user:info` and `user:check-access`, so it cannot act on the
  API server. Periscope checks the rights of the user, not of the token, on
  every request.
* Each token belongs to one user, shows as that user in the audit log, and can
  be revoked alone: `oc delete useroauthaccesstoken <name>`. The page shows the
  name.

# The admin API

* Everything under `/api/admin` (the purge action) needs `create` on services in
  the hub namespace. The app checks this with a SubjectAccessReview for the
  signed-in user, on every request.[^design]
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
