---
type: API Reference
title: MCP server
description: The MCP tools an AI assistant uses to read the Periscope fleet, how a user gets a token, and how to connect a client.
tags: [mcp, ai, api, tokens]
status: stable
app_version: "2.0.1"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-13T11:30:00Z }
sources:
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md#use-periscope-from-an-ai-assistant-mcp
    title: "README: Use Periscope from an AI assistant"
  - id: code
    resource: https://github.com/croz-ltd/periscope/blob/master/internal/api/mcp.go
    title: MCP server code
---

# Concepts

* `/mcp` is an MCP server with the Streamable HTTP transport. It is stateless:
  each request stands alone, so a hub restart does not break a client.
* It reads the same stored data as the UI. It never calls the clusters live,
  except `refresh`, which starts a scrape.
* It sits behind the same login and the same access rule as the REST API: a
  bearer token of a user with `get` on services in the hub namespace.[^readme]

# Tools

| Tool | Input | Answers |
|---|---|---|
| `list_clusters` | `at` | The clusters, their last scrape, whether it worked, whether it is stale |
| `list_components` | `query`, `group`, `at` | The component keys, with group, comparison kind and how many clusters report each |
| `get_component` | `key`, `clusters`, `at` | One component on each cluster: value, drift state, tooltip details |
| `get_cluster` | `cluster`, `group`, `at` | Every component of one cluster, or of one group |
| `fleet_totals` | `keys`, `clusters`, `at` | Sum, minimum, maximum and average of numeric rows, and the value per cluster |
| `find_drift` | `group`, `clusters`, `at` | Versions behind, configuration that differs, certificates within 120 days, failed or stale scrapes |
| `get_history` | `keys` (1 to 10), `days` (1, 2, 5, 7, 14, 30), `at` | The value of each component per cluster over time |
| `list_changes` | `from`, `to`, `cluster`, `limit`, `includeCounters` | The change feed, newest first |
| `refresh` | none | Starts a scrape of every cluster now |

* `at` is an RFC3339 time. With it, a tool answers about that moment.
* `clusters` and `cluster` accept the joined name or the console banner label.
* `fleet_totals` sums only `info` rows (counts). A version row is listed under
  `notCounted`. A pair row such as `3 PVC / 4 PV` is summed per part.
* An unknown key fails with a list of similar keys, so the assistant can retry.

# Get a token

1. Open the hub in a browser and sign in.
2. Open the user menu, **MCP access token** (`/mcp/token`).
3. The page shows a token of your own user and the command to add it to a
   client. Copy it, because the page does not show it again.

The token is an OpenShift OAuth token from the `periscope-mcp` OAuthClient:

* It is valid for `mcpTokenDays`, 30 days by default.
* Its scopes are `user:info` and `user:check-access`. It cannot act on the API
  server.
* Periscope checks your own rights on each call. If you lose `get` on services
  in the hub namespace, the token stops working within a minute.
* Revoke it with `oc delete useroauthaccesstoken <name>`. The page shows the
  name.

Without the OAuthClient (the chart did not know the route host), the page
explains how to use `oc whoami -t` instead. That works, but it expires with the
OpenShift login, often after 24 hours.

# Connect a client

```bash
# Claude Code
claude mcp add --transport http periscope https://<hub-route>/mcp \
  --header "Authorization: Bearer <token>"
```

Other clients take the same URL and header in their config:

```json
{
  "mcpServers": {
    "periscope": {
      "type": "http",
      "url": "https://<hub-route>/mcp",
      "headers": { "Authorization": "Bearer <token>" }
    }
  }
}
```

# Examples

* "How many vCPUs do all clusters have, and how many are free?" uses
  `fleet_totals` with `vm-vcpu-total` and `vm-vcpu-free`.
* "Which clusters are behind on OpenShift?" uses `find_drift` with the group
  `OpenShift`.
* "What changed on prod-emea since Monday?" uses `list_changes` with `cluster`
  and `from`.

[^readme]: README: Use Periscope from an AI assistant
