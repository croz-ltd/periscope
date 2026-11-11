---
type: Reference
title: CLI flags
description: The commands of the periscope binary, and every flag with its default and environment variable.
tags: [cli, flags, configuration, environment]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md#cli
    title: "README: CLI"
---

# Commands

| Command | Purpose |
|---|---|
| `periscope serve` | Scrape on an interval, and serve the UI and the API. The default command. |
| `periscope report` | Print the latest matrix as a text table. No web UI. Useful in CI. |
| `periscope version` | Print the build version. |

# Schema

Flags for `serve` and `report`:

| Flag | Default | Environment | Meaning |
|---|---|---|---|
| `--namespace` | The pod namespace, else `periscope` | | Hub namespace that holds the cluster Secrets |
| `--label-key` | `periscope.io/cluster` | | Label key that marks a cluster Secret |
| `--label-value` | `true` | | Label value that marks a cluster Secret |
| `--db` | `/data/periscope.db` | `DB_PATH` | SQLite database path |
| `--stale-after` | `30m` | | Mark a cluster stale when its last scrape is older than this |
| `--log-level` | `info` | `LOG_LEVEL` | `debug`, `info`, `warn` or `error` |
| `--log-format` | `text` | `LOG_FORMAT` | `text` to read by eye, `json` for a log collector |

Flags for `serve` only:

| Flag | Default | Environment | Meaning |
|---|---|---|---|
| `--addr` | `:8080` | `LISTEN_ADDR` | HTTP listen address |
| `--interval` | `10m` | | How often the whole fleet is scraped |
| `--timeout` | `30s` | | Deadline for the scrape of one cluster |
| `--concurrency` | `4` | | Clusters scraped at the same time |
| `--config` | none | `CONFIG_PATH` | Extractor config file. See [CR extractors](cr-extractors.md). |

# Tuning

* Keep `--stale-after` larger than `--interval`. With the defaults, a cluster
  turns stale after about three missed scrapes.
* A large fleet or slow clusters: raise `--timeout` first. Raise
  `--concurrency` only if the hub pod has CPU and memory to spare.
* The hub chart sets the log level and format from its values. See
  [Helm values](helm-values.md).

# Log levels

| Level | What it writes |
|---|---|
| `info` | One line per scrape cycle, one per cluster, discovery results, startup settings |
| `warn` | Anything that makes the result less complete: a failed extractor, a Secret with no credentials, a cluster with no CA bundle, an invalid grouping ConfigMap |
| `debug` | Each extractor with its duration, each skipped resource, each saved snapshot, each HTTP request |

Each line carries a `component` field (`scrape`, `cluster`, `store`, `api`,
`extract`). Filter on it instead of searching the text.

# Examples

```bash
# serve with the current kubeconfig context and a local database
bin/periscope serve --namespace periscope --db ./periscope.db

# print the matrix once
bin/periscope report --db ./periscope.db
```
