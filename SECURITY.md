# Security Policy

## Supported versions

Periscope is pre-1.0. Fixes land on `master` and in the next tagged release; only
the latest release is supported.

## Reporting a vulnerability

Please **do not** open a public issue for security problems.

Report privately via GitHub's
[private vulnerability reporting](https://github.com/croz-ltd/periscope/security/advisories/new)
("Report a vulnerability" under the Security tab), including:

- a description of the issue and its impact,
- steps to reproduce (or a proof of concept),
- the affected version or commit.

We aim to acknowledge within 5 working days and to agree a disclosure timeline with
you before publishing.

## Security model, in brief

Understanding what Periscope holds helps scope a report:

- **Credentials.** Periscope stores no credentials of its own. Joined clusters are
  described by Secrets in the hub namespace containing a read-only ServiceAccount
  token per cluster. Revoking a cluster's token, or deleting the Secret, removes
  Periscope's access to it.
- **Read-only.** Every extractor performs read operations only. Periscope never
  writes to a joined cluster.
- **Privilege.** The per-cluster reader ServiceAccount binds OpenShift's built-in
  `cluster-reader` ClusterRole — broad, cluster-wide, read-only, so newly installed
  operator CRDs never return `forbidden`. That trade-off is deliberate and documented
  in [DESIGN.md](DESIGN.md); substituting a curated least-privilege ClusterRole means
  editing `charts/periscope-join/templates/clusterrolebinding.yaml`.
- **UI authentication** is delegated to an `oauth-proxy` sidecar (OpenShift SSO plus
  an RBAC access review). Periscope contains no authentication code of its own, and
  the API surface it exposes behind the proxy is read-only apart from
  `POST /api/refresh`, which triggers a scrape.
- **Stored data.** The SQLite database on the PVC holds cluster names, component
  keys, and version strings — inventory metadata, not workload data.

Issues in the *dependencies* (Go modules, npm packages, base images) are handled as
ordinary upgrades via Dependabot; open a normal issue or PR for those unless the
vulnerability is exploitable through Periscope in a non-obvious way.
