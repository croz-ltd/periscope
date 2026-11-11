---
type: Metric
title: Prometheus metrics
description: Two gauges on /metrics for alerts, one for the drift of each component and one for stale clusters.
tags: [metrics, prometheus, alerting]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md#api-and-metrics
    title: "README: API and metrics"
---

# Schema

| Metric | Labels | Value |
|---|---|---|
| `periscope_component_drift_severity` | `cluster`, `component`, `state` | 0 on the fleet leader. Higher means further behind. |
| `periscope_cluster_stale` | `cluster` | 1 when the last snapshot of the cluster is stale, else 0 |

`component` is the component key. See [Component keys](components.md). The
drift value follows [How drift is decided](../architecture/drift-model.md).

# Examples

Alert rules to start from:

```yaml
groups:
  - name: periscope
    rules:
      - alert: PeriscopeClusterStale
        expr: periscope_cluster_stale == 1
        for: 30m
        annotations:
          summary: "Periscope cannot read cluster {{ $labels.cluster }}"
      - alert: PeriscopeOpenShiftDrift
        expr: periscope_component_drift_severity{component="openshift"} > 0
        for: 7d
        annotations:
          summary: "{{ $labels.cluster }} is behind the fleet on OpenShift"
```

# Notes

* The metrics read the stored data, like the UI. They cover every joined
  cluster, also the clusters a reader hides with **Manage view**.
* `/metrics` sits behind the proxy. A Prometheus that scrapes it needs a token
  with `get` on services in the hub namespace.
