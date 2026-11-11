---
type: API Reference
title: REST API
description: The HTTP endpoints of the Periscope hub, for the UI, for scripts and for integrations.
tags: [api, rest, export, integration]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md#api-and-metrics
    title: "README: API and metrics"
---

# Access

The API sits behind the `oauth-proxy` sidecar, like the UI. From a script,
send an OpenShift token:

```bash
curl -sS -H "Authorization: Bearer $(oc whoami -t)" "https://<hub-route>/api/matrix"
```

The reader needs `get` on services in the hub namespace. `/api/admin` needs
`create` on services. See [Access and security](../architecture/security.md).

# Schema

| Method and path | Purpose |
|---|---|
| `GET /` | The embedded PatternFly UI |
| `GET /api/matrix` | The full comparison matrix as JSON, the data the UI shows |
| `GET /api/matrix?at=<RFC3339>` | The matrix as it was at that moment |
| `GET /api/changes` | The change feed, newest first. Parameters: `from`, `to`, `cluster`, `limit`. |
| `GET /api/changes/calendar` | Number of changes per day, for the calendar |
| `GET /api/timeline?key=<key>&days=<1,2,5,7,14,30>` | One series per cluster for that component. Accepts `at`. |
| `GET /api/export.csv` | The matrix as CSV. Accepts `at`, `page=compare\|statistics` and `cluster=<name>` (repeatable). |
| `GET /api/export.json` | The matrix as JSON, with the same parameters as CSV |
| `POST /api/refresh` | Start a scrape of the fleet now |
| `GET /api/version` | The version of this build |
| `POST /api/clusters` | Join a cluster. Body: `{name, apiURL, token, caBundle, insecureTLS}`. |
| `GET /api/admin/clusters` | The data held per cluster, and if the cluster is still joined |
| `POST /api/admin/purge` | Delete the history of `{clusters: [...]}`. Refuses clusters that are still joined. |
| `GET /yaml/new-cluster?name=<cluster>` | The join manifests for one cluster, for `oc apply -f` |
| `GET /metrics` | Prometheus metrics. See [Prometheus metrics](metrics.md). |
| `GET /healthz` | Liveness |

# Rules for `POST /api/clusters`

* `apiURL` must start with `https://`.
* Send `caBundle`, or set `insecureTLS: true`. The call fails with neither.
* `token` needs rights to create a namespace, a ServiceAccount and a
  ClusterRoleBinding on the new cluster. It is not stored.
* The hub needs `allowClusterImport=true`. Otherwise the call fails and names
  the manual path.

# Examples

```bash
H="Authorization: Bearer $(oc whoami -t)"
HUB=https://<hub-route>

# the Statistics page of two clusters, as CSV
curl -sS -H "$H" "$HUB/api/export.csv?page=statistics&cluster=prod-emea&cluster=prod-us" -o stats.csv

# the matrix as it was on the first of the month
curl -sS -H "$H" "$HUB/api/matrix?at=2026-11-01T00:00:00Z"

# seven days of pod counts per cluster
curl -sS -H "$H" "$HUB/api/timeline?key=pod-total&days=7"

# scrape now
curl -sS -X POST -H "$H" "$HUB/api/refresh"
```
