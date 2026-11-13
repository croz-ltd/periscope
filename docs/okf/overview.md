---
type: Product
title: Periscope
description: A single Go binary that shows version and configuration drift across a fleet of OpenShift clusters in one view.
resource: https://github.com/croz-ltd/periscope
tags: [openshift, fleet, drift, inventory, overview]
status: stable
app_version: "2.0.0"
license: Apache-2.0
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md
    title: Periscope README
    author: team:croz
  - id: design
    resource: https://github.com/croz-ltd/periscope/blob/master/DESIGN.md
    title: "Periscope: Design"
    author: team:croz
---

# Problem

A fleet of more than a few OpenShift clusters is hard to keep in step. The
console shows one cluster at a time, and loops of `oc` commands do not scale.
Some versions that matter most are not in any single view: the CSI driver
version inside a Portworx or Dell custom resource, the kubelet on one node that
missed an upgrade, or the API server certificate that expires in three weeks.[^readme]

# What Periscope does

Periscope runs as one pod on a hub cluster. On an interval, it reads every
joined cluster with a read-only token, keeps the full history in an embedded
SQLite database, and serves the result as a comparison matrix.[^readme]

| Area | What it collects per cluster |
|---|---|
| Platform | OpenShift release and channel, node kubelet versions, MachineConfigPool state |
| Capacity | CPU cores and memory summed over all nodes: total, used (pod requests) and free. With OpenShift Virtualization also VM vCPUs and VM memory, from the allocation ratio and memory overcommit |
| Workloads | Pod counts across all namespaces: total, running and crash-looping |
| Operators | Every OLM-installed operator, keyed by package name, found with no configuration |
| Storage | Default StorageClass, PV and PVC counts per storage class, Portworx and Dell CSI versions |
| Virtualization | OpenShift Virtualization settings, VM counts by state, VM snapshots, VM templates |
| Certificates | API server and ingress certificate expiry |
| Managed workloads | The Grafana version that `grafana-operator` actually runs |

The full list of rows is in [Component keys](reference/components.md).

# What it answers

* **Do my clusters agree?** Each row compares the clusters against the highest
  version in the fleet. See [How drift is decided](architecture/drift-model.md).
* **What can each cluster do about it?** Update available, upgrade blocked,
  update in progress, operator updates pending.
* **What changed, and when?** Every scrape is stored, so the Changes page is an
  audit log and the matrix can show any past moment.
* **How big is the fleet?** The Statistics page shows counts as a table, as bar
  charts and as a timeline.

The pages are described in [UI views](reference/views.md).

The same data is open to scripts through the [REST API](reference/api.md) and to
AI assistants through the [MCP server](reference/mcp.md). Both use the OpenShift
login and the same access rule as the UI.

# What it does not do

* It does not compare clusters with the newest upstream release. A fleet that is
  uniformly behind shows all green, by design.[^design]
* It does not change anything on a joined cluster. Every scrape is read-only.
* It runs no agent on the joined clusters. The hub pulls from them.

# Where to go next

* [The pull model](architecture/pull-model.md) explains the architecture.
* [Deploy the hub](operations/deploy-hub.md) and [Join a cluster](operations/join-cluster.md) get a fleet running.
* [CLI flags](configuration/cli-flags.md) lists every setting.

[^readme]: Periscope README
[^design]: Periscope: Design
