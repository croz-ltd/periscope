# Configuration

Periscope runs with no configuration. OLM discovery finds the operators, and the
built-in extractors need no setup. These are the settings to use when the
defaults do not fit your fleet.

* [CLI flags](cli-flags.md) - Flags and environment variables of `serve` and `report`.
* [Helm values](helm-values.md) - The five values of the hub chart. The join chart has none.
* [The cluster Secret](cluster-secret.md) - The Secret on the hub that registers one cluster.
* [Grouping the matrix](groups-configmap.md) - The `periscope-groups` ConfigMap: your own sections per page, and hidden rows.
* [CR extractors](cr-extractors.md) - Read a version from any custom resource field, with no code change.
