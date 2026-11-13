---
type: API Reference
title: MCP server
description: The MCP tools an AI assistant uses to read the Periscope fleet, how a user gets a token, and how to connect a client.
tags: [mcp, ai, api, tokens]
status: stable
app_version: "2.0.2"
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
2. Open **Docs, MCP** and click **Get an MCP token**, or open the user menu,
   **MCP access token** (`/mcp/token`).
3. The browser comes back to **Docs, MCP**. The page shows the token once and
   fills it into every example. Copy it, because a reload does not show it
   again.

The token reaches the page in an encrypted cookie that only `/api/mcp/issued`
reads, once. It is never in a URL.

The token is an OpenShift OAuth token from the `periscope-mcp` OAuthClient:

* It is valid for `mcpTokenDays`, 30 days by default.
* Its scopes are `user:info` and `user:check-access`. It cannot act on the API
  server.
* Periscope checks your own rights on each call. If you lose `get` on services
  in the hub namespace, the token stops working within a minute.
* Revoke it with `oc delete useroauthaccesstoken <name>`. The page shows the
  name.

Without the OAuthClient (the chart did not know the route host), the page
explains how to use `$(oc whoami -t)` instead. That works, but it expires with
the OpenShift login, often after 24 hours.

# Connect a client

Put the token in the environment first. Most clients read it from there, so
their configuration holds no secret:

```bash
export PERISCOPE_TOKEN='<token>'
```

**Docs, MCP** has a tab per client, each with a command and a config file,
filled in with the URL of the hub:

| Client | Command | Config file |
|---|---|---|
| Claude Code | `claude mcp add --transport http --scope user periscope https://<hub-route>/mcp --header "Authorization: Bearer $PERISCOPE_TOKEN"` | `.mcp.json` or `~/.claude.json`: `"headers": {"Authorization": "Bearer ${PERISCOPE_TOKEN}"}` |
| Codex CLI | `codex mcp add periscope --url https://<hub-route>/mcp --bearer-token-env-var PERISCOPE_TOKEN` | `~/.codex/config.toml`: `[mcp_servers.periscope]` with `url` and `bearer_token_env_var = "PERISCOPE_TOKEN"` |
| Copilot CLI | `copilot mcp add --transport http periscope https://<hub-route>/mcp --header "Authorization: Bearer $PERISCOPE_TOKEN"` | `~/.copilot/mcp-config.json`: `"type": "http"`, `url`, `headers` with the token itself |
| Gemini CLI | `gemini mcp add --scope user --transport http --header "Authorization: Bearer $PERISCOPE_TOKEN" periscope https://<hub-route>/mcp` | `~/.gemini/settings.json`: `httpUrl` and `"headers": {"Authorization": "Bearer $PERISCOPE_TOKEN"}` |
| VS Code | `code --add-mcp "{\"name\":\"periscope\",\"type\":\"http\",\"url\":\"https://<hub-route>/mcp\",\"headers\":{\"Authorization\":\"Bearer $PERISCOPE_TOKEN\"}}"` | `.vscode/mcp.json`: `servers.periscope` with `"type": "http"` and an `inputs` password prompt for the token |

* Claude Code, Codex CLI and Gemini CLI read `PERISCOPE_TOKEN` when they start.
  Their config files can be committed.
* VS Code asks for the token once and keeps it in its secret storage.
* The Copilot CLI config file holds the token itself. Keep it private.

# Examples

* "How many vCPUs do all clusters have, and how many are free?" uses
  `fleet_totals` with `vm-vcpu-total` and `vm-vcpu-free`.
* "Which clusters are behind on OpenShift?" uses `find_drift` with the group
  `OpenShift`.
* "What changed on prod-emea since Monday?" uses `list_changes` with `cluster`
  and `from`.

[^readme]: README: Use Periscope from an AI assistant
