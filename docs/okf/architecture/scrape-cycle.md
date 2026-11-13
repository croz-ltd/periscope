---
type: Process
title: The scrape cycle
description: How Periscope reads the fleet on an interval, stores a snapshot per cluster, and marks a cluster stale when a scrape fails.
tags: [architecture, scrape, scheduler, extractors, history]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md
    title: Periscope README
  - id: registry
    resource: https://github.com/croz-ltd/periscope/blob/master/internal/extract/registry.go
    title: Extractor registry
---

# Steps

1. **Discover.** The scheduler lists the Secrets in the hub namespace that carry
   the cluster label (default `periscope.io/cluster=true`). Each Secret is one
   cluster. A Secret added or deleted between cycles takes effect on the next
   cycle.
2. **Connect.** For each cluster, it builds a client from `apiURL`, `token` and
   the optional `caBundle`. With no `caBundle`, the hub does not verify
   the TLS certificate of that cluster and logs a `warn`.
3. **Extract.** It runs every extractor against the cluster, with a deadline of
   `--timeout` (default 30s) for the whole cluster. Up to `--concurrency`
   (default 4) clusters run at the same time.
4. **Store.** It saves one snapshot per cluster in SQLite. It also records the
   changes against the last successful snapshot of that cluster.
5. **Wait.** The next cycle starts after `--interval` (default 10m).
   **Refresh** in the UI, or `POST /api/refresh`, starts a cycle now.

# Extractors

An extractor reads one part of a cluster and returns matrix rows. Version 1.10.0
runs 18 of them:[^registry]

| Extractor | Reads |
|---|---|
| OpenShift | ClusterVersion: release, channel, update available, upgradeable, update in progress |
| Console banner | The `cluster-name` ConsoleNotification, used as the column title |
| Console URL | The web console address, used as the column link |
| Default StorageClass | The default storage class |
| Storage volumes | PV and PVC counts per storage class |
| Certificates | API server and ingress certificate expiry |
| Virtualization | HyperConverged settings of OpenShift Virtualization |
| Virtual machines | VM counts by state |
| VM snapshots | VM snapshots and snapshot volumes per storage class |
| VM templates | The number of VM templates |
| Nodes | Node count and kubelet versions |
| Pods | Pod counts in all namespaces: total, running, crash-looping |
| Capacity | CPU and memory of all nodes, and the VM vCPU and VM memory view: total, used (requests), free |
| MachineConfigPools | The state of each pool |
| OLM | Every installed operator, plus pending updates and InstallPlans that wait for approval |
| Portworx | The Portworx operator and CSI driver version from the StorageCluster CR |
| Dell CSM | The Dell CSI driver version from the ContainerStorageModule CR |
| Grafana | The Grafana version that `grafana-operator` runs |

The Portworx and Dell extractors read a field path from a custom resource. A
config file can change those paths and add new ones. See
[CR extractors](../configuration/cr-extractors.md).

An extractor for a resource that the cluster does not serve (a vendor CRD that
is not installed) skips quietly. It is not an error.

# When a scrape fails

* **The cluster does not answer, or the deadline passes.** Periscope keeps the
  last good snapshot and shows a `stale since HH:MM` badge with the reason. A
  cluster is stale when its last scrape is older than `--stale-after`
  (default 30m).[^readme]
* **One extractor fails.** The other rows of that cluster still update. The
  rows of the failed extractor are missing from that snapshot and show as "not
  installed" until the next good scrape. The log has a `warn` line
  `extractor failed` with the error.
* **No false change events.** An unreachable cluster records no removals.
  A scrape with a partial error does not record removals either, because it
  cannot tell "uninstalled" from "could not read".

[^readme]: Periscope README
[^registry]: Extractor registry
