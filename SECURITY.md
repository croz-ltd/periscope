# Security Policy

## Supported versions

Periscope is pre-1.0. Fixes land on `master` and in the next tagged release, and only
the latest release is supported.

## Reporting a vulnerability

Never open a public issue for security problems.

Report privately through GitHub's
[private vulnerability reporting](https://github.com/croz-ltd/periscope/security/advisories/new)
("Report a vulnerability" under the Security tab), and include:

- a description of the issue and its impact,
- steps to reproduce, or a proof of concept,
- the affected version or commit.

We aim to acknowledge within 5 working days and to agree a disclosure timeline with you
before publishing.

## Security model, in brief

Knowing what Periscope holds helps scope a report.

Periscope stores no credentials of its own. Joined clusters are described by Secrets in
the hub namespace, each containing one read-only ServiceAccount token. Revoking a
cluster's token, or deleting the Secret, removes Periscope's access to that cluster.

Every extractor performs read operations only. Periscope never writes to a joined
cluster.

The per-cluster reader ServiceAccount binds OpenShift's built-in `cluster-reader`
ClusterRole, which is broad, cluster-wide and read-only. That keeps newly installed
operator CRDs from returning `forbidden`. The trade-off is deliberate and documented in
[DESIGN.md](DESIGN.md). Substituting a curated least-privilege ClusterRole means
editing `charts/periscope-join/templates/clusterrolebinding.yaml`.

UI authentication is delegated to an `oauth-proxy` sidecar doing OpenShift SSO plus an
RBAC access review, so Periscope contains no authentication code of its own. The API
behind the proxy is read-only apart from `POST /api/refresh`, which triggers a scrape.

The SQLite database on the PVC holds cluster names, component keys and version strings.
It is inventory metadata, not workload data.

Vulnerabilities in dependencies (Go modules, npm packages, base images) are handled as
ordinary upgrades through Dependabot. Open a normal issue or pull request for those,
unless the vulnerability is exploitable through Periscope in a way that is not obvious.
