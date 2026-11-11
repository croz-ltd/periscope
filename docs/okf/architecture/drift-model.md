---
type: Definition
title: How drift is decided
description: Each row compares the clusters against the highest version in the fleet, and a cluster behind that leader shades red.
tags: [drift, compare, versions, baseline]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md#how-drift-is-decided
    title: "README: How drift is decided"
  - id: design
    resource: https://github.com/croz-ltd/periscope/blob/master/DESIGN.md#version-model-resolved
    title: "DESIGN.md: Version model"
---

# Definition

The baseline of each row is the **highest version seen across the fleet**. The
cluster with that version is the leader and shows green. A cluster behind it
shades red, and the colour is darker when the gap is bigger.[^readme]

This is **intra-fleet drift**. It answers "do my clusters agree with each
other?" and not "are my clusters on the newest upstream release?". A fleet that
is uniformly behind shows all green. That is the intended answer.[^design]

# Cell states

| State | Meaning | Counts toward the baseline |
|---|---|---|
| Green | The cluster is on the fleet leader version | Yes |
| Red, light to dark | The cluster is behind the leader. Darker means a bigger gap. | Yes |
| Not installed | The component is absent on this cluster | No |
| Grey, raw text | The value does not parse as a version | No |
| Info | A count or a setting, not a version (pod counts, PVC counts) | No drift colour |

# Rules

* **Tolerant parsing.** A leading `v` is removed and build metadata is
  ignored. A value that still does not parse is shown as text, in grey. Periscope
  never compares two non-versions as strings.
* **Absent is not behind.** A component that is not installed stays out of the
  baseline, so a partial rollout is visible but does not count as drift.
* **Hidden columns still count.** A cluster hidden with **Manage view** still
  takes part in the baseline, the exports and the metrics. The hide is per
  browser only.
* **Certificates** use their own colours: by how soon the certificate expires.

# Alerting

The metric `periscope_component_drift_severity` carries the same result. It is
0 on the leader. See [Prometheus metrics](../reference/metrics.md).

[^readme]: README: How drift is decided
[^design]: DESIGN.md: Version model
