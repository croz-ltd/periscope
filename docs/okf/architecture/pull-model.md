---
type: Architecture
title: The pull model
description: One Periscope pod on a hub cluster reads every joined cluster with a read-only token, and no agent runs on the joined clusters.
tags: [architecture, hub, topology]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md#how-it-works
    title: "README: How it works"
  - id: design
    resource: https://github.com/croz-ltd/periscope/blob/master/DESIGN.md#architecture-resolved
    title: "DESIGN.md: Architecture"
---

# Diagram

```
                    ┌─────────── hub cluster ───────────┐
                    │  periscope pod                    │
labeled Secrets ───►│   scheduler ──► SQLite (PVC)      │──► matrix UI + REST/CSV/metrics
(one per cluster)   │      │           full history     │
                    └──────┼────────────────────────────┘
                           │ read-only SA token per cluster
              ┌────────────┼────────────┬────────────┐
           cluster A    cluster B    cluster C    cluster D ...
```

# Parts

| Part | Role |
|---|---|
| Hub cluster | Runs the Periscope pod. Any OpenShift cluster that reaches the API servers of the fleet. |
| Periscope pod | The `periscope` container plus an `oauth-proxy` sidecar for sign-in. |
| Labeled Secrets | One Secret per joined cluster, in the hub namespace. They are the cluster registry. See [The cluster Secret](../configuration/cluster-secret.md). |
| Scheduler | Reads all clusters on an interval, in parallel, with a deadline per cluster. See [The scrape cycle](scrape-cycle.md). |
| SQLite on a PVC | Holds every snapshot. It survives pod restarts and needs no external database. |
| Read surfaces | The web UI, the REST API, CSV and JSON export, Prometheus metrics and the `report` command. |

# Decisions

* **Central pull, no agents.** The hub calls each remote API server directly,
  so there is nothing to install or upgrade on the fleet. The hub must reach
  the API endpoint of every cluster. Confirm this before adoption.[^design]
* **Scrape, then cache, then read.** A page load or an API call reads the
  stored data and never calls the clusters live.[^readme]
* **Every cluster is a Secret.** There is no implicit "local" cluster. A new
  hub starts with an empty matrix. To compare the hub cluster too, join it like
  any other cluster.[^design]
* **Full history.** Every scrape is kept, so the matrix can show any past
  moment and the Changes page can list what moved.
* **Design size.** The target is a fleet of about ten clusters reachable from
  one hub.[^design]

[^readme]: README: How it works
[^design]: DESIGN.md: Architecture
