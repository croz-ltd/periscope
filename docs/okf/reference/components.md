---
type: Reference
title: Component keys
description: Every matrix row Periscope 1.10.1 produces, with its key, group, page and source on the cluster.
tags: [components, keys, extractors, matrix]
status: stable
app_version: "1.10.1"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: extract
    resource: https://github.com/croz-ltd/periscope/tree/master/internal/extract
    title: Extractor code
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md#what-it-shows
    title: "README: What it shows"
---

# Concepts

* A **component key** is the stable identity of a row. Use it in the
  [groups ConfigMap](../configuration/groups-configmap.md), in
  `GET /api/timeline?key=`, and in the `component` label of the metrics.
* A **version row** shows on Compare and is coloured by drift.
* An **info row** is a count or a setting. It shows on Statistics, or on Compare
  with no drift colour.
* A key that ends in `-<name>` has one row per object, for example one row per
  storage class.
* The **Docs** page of the UI lists the keys of your own fleet.

# Schema

| Key | Name in the UI | Group | Source on the cluster |
|---|---|---|---|
| `openshift` | OpenShift | OpenShift | ClusterVersion: the current release |
| `openshift-channel` | Update channel | OpenShift | ClusterVersion: the update channel |
| `openshift-update-available` | Update available | OpenShift | ClusterVersion: the newest release offered |
| `openshift-upgradeable` | Upgrade blocked | OpenShift | ClusterVersion: an `Upgradeable=False` condition, with its reason |
| `openshift-updating` | Update in progress | OpenShift | ClusterVersion: an update that runs now |
| `node-count` | Total nodes | Node | Nodes |
| `node-kubelet` | Kubelet | Node | Nodes: kubelet versions |
| `cpu-capacity` | CPU cores: total | Capacity | Nodes: sum of `status.capacity.cpu`. The tooltip shows allocatable, the system reservation and the sum per role. |
| `cpu-capacity-masters` | CPU cores: total masters | Capacity | Total of the nodes with a master or control-plane role, compact-cluster nodes included |
| `cpu-capacity-workers` | CPU cores: total workers | Capacity | Total of the nodes with only the worker role |
| `cpu-capacity-other` | CPU cores: total other | Capacity | Total of all other nodes: infra, storage, custom roles, no role. The tooltip shows the roles. |
| `cpu-requested` | CPU cores: used (requested) | Capacity | CPU requests of the unfinished pods placed on a node |
| `cpu-free` | CPU cores: free | Capacity | Sum of `status.allocatable.cpu` minus the requests |
| `mem-capacity` | Memory GiB: total | Capacity | Nodes: sum of `status.capacity.memory`. The tooltip is the same as for CPU. |
| `mem-capacity-masters` | Memory GiB: total masters | Capacity | Same split as for CPU |
| `mem-capacity-workers` | Memory GiB: total workers | Capacity | Same split as for CPU |
| `mem-capacity-other` | Memory GiB: total other | Capacity | Same split as for CPU |
| `mem-requested` | Memory GiB: used (requested) | Capacity | Memory requests of the unfinished pods placed on a node |
| `mem-free` | Memory GiB: free | Capacity | Sum of `status.allocatable.memory` minus the requests |
| `vm-vcpu-total` | VM vCPUs: total | Capacity | Allocatable cores of the VM nodes times `vmiCPUAllocationRatio` (default 10) |
| `vm-vcpu-used` | VM vCPUs: used (requested) | Capacity | Requested cores on the VM nodes times the ratio |
| `vm-vcpu-free` | VM vCPUs: free | Capacity | Total minus used |
| `vm-memory-total` | VM memory GiB: total | Capacity | Allocatable memory of the VM nodes times `memoryOvercommitPercentage` / 100 (default 100) |
| `vm-memory-used` | VM memory GiB: used (requested) | Capacity | Requested memory on the VM nodes times the percentage |
| `vm-memory-free` | VM memory GiB: free | Capacity | Total minus used |
| `pod-total` | Pods total | Workloads | Pods in all namespaces. The tooltip shows the count per phase. |
| `pod-running` | Pods running | Workloads | Pods in phase Running |
| `pod-crashloop` | Pods crash-looping | Workloads | Pods with a container (init containers too) in CrashLoopBackOff |
| `mcp-<pool>` | MCP: `<pool>` | MachineConfigPools | The state of each pool |
| `default-storageclass` | StorageClass - Default | Storage | The StorageClass marked default |
| `storage-total` | Storage (all classes) | Storage | PVC and PV counts, all classes |
| `storage-<class>` | Storage: `<class>` | Storage | PVC and PV counts of one class |
| `portworx-csi` | Portworx (CSI) | Storage | StorageCluster CR. See [CR extractors](../configuration/cr-extractors.md). |
| `dell-csi` | Dell CSM (CSI) | Storage | ContainerStorageModule CR |
| `<package>` | The operator name | Operators | One row per OLM operator, keyed by package name |
| `olm-updates-pending` | Operator updates pending | Operators | Subscriptions with a newer CSV than the installed one |
| `olm-installplans-waiting` | InstallPlans awaiting approval | Operators | InstallPlans that wait for manual approval |
| `grafana` | Grafana | Operators | The Grafana version that `grafana-operator` runs |
| `cert-api` | API | Certificate | Expiry of the API server certificate |
| `cert-ingress` | Ingress | Certificate | Expiry of the default ingress certificate |
| `hco-vm-state-sc` | VM state storage class | OpenShift Virtualization | HyperConverged setting |
| `hco-scratch-sc` | Scratch space storage class | OpenShift Virtualization | HyperConverged setting |
| `hco-mem-overcommit` | Memory overcommit % | OpenShift Virtualization | HyperConverged setting |
| `hco-cpu-ratio` | vCPU allocation ratio | OpenShift Virtualization | HyperConverged setting |
| `vm-total` | Virtual machines | OpenShift Virtualization | VirtualMachines |
| `vm-running` | VMs running | OpenShift Virtualization | VirtualMachines in state Running |
| `vm-stopped` | VMs stopped | OpenShift Virtualization | VirtualMachines in state Stopped |
| `vm-other` | VMs other | OpenShift Virtualization | VirtualMachines in any other state |
| `vmsnapshot-total` | VM snapshots | OpenShift Virtualization | VirtualMachineSnapshots |
| `vmsnapshot-volumes` | Snapshot volumes | OpenShift Virtualization | Volumes in all VM snapshots |
| `vmsnapshot-vol-<class>` | Snapshot volumes: `<class>` | OpenShift Virtualization | Snapshot volumes of one storage class |
| `vm-templates` | VM templates | OpenShift Virtualization | VM templates |
| `cluster-banner` | (column title) | | The `cluster-name` ConsoleNotification. Not a row. |
| `cluster-console` | (column link) | | The web console URL. Not a row. |

# Notes

* The OpenShift Virtualization version itself is an operator row, from OLM.
* The capacity rows count requests, not live usage. The scheduler places pods and VMs by
  requests, so free is what a new workload can still get. The values are whole cores and
  whole GiB, and the tooltip carries the exact values.
* Each node is in one class (masters, workers or other), so the three class totals add up
  to the total. An infra node that keeps the worker label counts as other.
* A VM node has the label `kubevirt.io/schedulable=true` and is not cordoned. The `vm-*`
  capacity rows show only when the HyperConverged CR exists.
* Free capacity is a cluster sum. A VM fits only when one node has enough free CPU and
  memory, and live migration and node failure need spare room too.
* Rows for a component that a cluster does not run show "not installed" on
  that cluster.
* Info rows (pod, VM and volume counts, settings) count as counters. The Changes page hides them unless
  you turn on **Include counters**.
