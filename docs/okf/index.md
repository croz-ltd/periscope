---
okf_version: "0.2"
---

# Periscope knowledge bundle

This directory is the Periscope documentation as an
[Open Knowledge Format](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md)
(OKF v0.2) bundle. Every file is markdown with YAML frontmatter, so people read
it as plain text and agents parse it without special tools. The
[README](../../README.md) and [DESIGN.md](../../DESIGN.md) stay the primary
sources. This bundle restates them as small concepts that link to each other.

# Start here

* [Periscope](overview.md) - What Periscope is, what it shows, and who it is for.

# Sections

* [Architecture](architecture/) - How the hub pulls from the fleet, stores history and decides drift.
* [Operations](operations/) - Deploy the hub, join and remove clusters, fix common faults, keep it current.
* [Configuration](configuration/) - Every knob: CLI flags, Helm values, the cluster Secret, grouping, CR extractors.
* [Reference](reference/) - Component keys, the REST API, the MCP server, Prometheus metrics and the UI pages.

# History

* [Update log](log.md) - Changes to this bundle, newest first.
