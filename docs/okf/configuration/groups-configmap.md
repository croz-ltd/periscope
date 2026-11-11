---
type: Kubernetes ConfigMap
title: Grouping the matrix
description: The periscope-groups ConfigMap replaces the built-in row sections of the Compare and Statistics pages, and can hide rows.
resource: https://github.com/croz-ltd/periscope/blob/master/examples/periscope-groups.configmap.yaml
tags: [configmap, grouping, ui, configuration]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md#grouping-the-matrix
    title: "README: Grouping the matrix"
  - id: chart-cm
    resource: https://github.com/croz-ltd/periscope/blob/master/charts/periscope/templates/configmap-groups.yaml
    title: Hub chart groups ConfigMap
---

# Default behaviour

With no ConfigMap, each page groups rows by subsystem, in this order:
OpenShift, Node, Workloads, MachineConfigPools, Storage, Operators, Certificate,
OpenShift Virtualization.

# Schema

A ConfigMap named `periscope-groups` in the hub namespace, with one key,
`groups.yaml`:

| Field | Type | Meaning |
|---|---|---|
| `compare` | list of groups | The sections of the Compare page, in order |
| `statistics` | list of groups | The sections of the Statistics page, in order |
| `hidden` | list of keys | Component keys removed from both pages |
| group `title` | string | The section heading |
| group `keys` | list of keys | The rows of the section, in order |

Rules:[^readme]

* A page you configure follows your groups only. Rows that match no group go to
  an "Ungrouped" section at the end.
* A page you leave out keeps the built-in grouping.
* A key can be in more than one group.
* A key that matches no row is skipped.
* Changes take effect on the next load of the matrix. No restart.
* Delete the ConfigMap to return to the built-in grouping.

Find the component keys on the **Docs** page of the UI, or in
[Component keys](../reference/components.md).

# Examples

The hub chart installs this ConfigMap:[^chart-cm]

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: periscope-groups
  namespace: periscope
data:
  groups.yaml: |
    compare:
      - title: Platform
        keys: [openshift, node-kubelet]
      - title: Certificates
        keys: [cert-api, cert-ingress]
      - title: Storage drivers
        keys: [default-storageclass, portworx-csi, dell-csi]
    statistics:
      - title: Capacity
        keys: [node-count, storage-total]
      - title: Pods
        keys: [pod-total, pod-running, pod-crashloop]
      - title: Virtual machines
        keys: [vm-total, vm-running, vm-stopped]
    hidden: []
```

Apply your own version:

```bash
oc apply -f examples/periscope-groups.configmap.yaml -n periscope
```

[^readme]: README: Grouping the matrix
[^chart-cm]: Hub chart groups ConfigMap
