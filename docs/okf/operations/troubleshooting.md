---
type: Playbook
title: Troubleshooting
description: Symptoms, causes and fixes for stale clusters, empty cells, a missing Purge action and other common faults.
tags: [troubleshooting, oncall, logs, operations]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md
    title: Periscope README
---

# First look

```bash
oc -n periscope logs deploy/periscope -c periscope
```

At `info`, a healthy hub writes one line per scrape cycle and one line per
cluster. Every `warn` line is something that makes the result less complete.
For the full detail of one scrape, set `logLevel=debug` and lower it again
after. See [CLI flags](../configuration/cli-flags.md).

# A cluster column is stale

**Symptom:** a `stale since HH:MM` badge on the column header.

| Cause | Fix |
|---|---|
| The hub cannot reach the API server | Test from the hub namespace: `oc -n periscope run nettest --rm -it --restart=Never --image=registry.access.redhat.com/ubi10/ubi -- curl -sk https://<api>:6443/healthz`. The Periscope image has no shell or `curl`. Check firewalls and egress. |
| The token is invalid or revoked | Import the cluster again from the wizard, or replace `token` in the Secret. |
| The scrape passes `--timeout` | Raise `--timeout`, or lower `--concurrency` on a busy hub. |
| TLS fails | Check the `caBundle` in the Secret. It must sign the certificate of the API server. |

The badge shows the error. The matrix keeps the last good data meanwhile.

# A cell is empty or "not installed", but the component is there

* Look for `extractor failed` in the logs for that cluster. A failed
  extractor leaves its rows out of that snapshot.
* `remaining=-1.2s` or `context deadline exceeded` in that line means the
  deadline ran out mid-scrape. The rows come back on the next good scrape.
  Raise `--timeout` if it repeats.
* A `403` means the reader role does not cover the resource. This happens only
  when `cluster-reader` was replaced with a narrower role.
* For Portworx or Dell, the field path is wrong for your vendor version. See
  [CR extractors](../configuration/cr-extractors.md).

# A joined cluster does not show at all

* The Secret is missing the label `periscope.io/cluster=true`, or it is in a
  different namespace than `--namespace`.
* The Secret has no `apiURL` or no `token`. The logs show a `warn` for it.
* The next scrape did not run yet. Click **Refresh**.

# The Purge action is missing

Ask the API directly:

```bash
curl -sS -H "Authorization: Bearer $(oc whoami -t)" "https://<hub-route>/api/admin/clusters"
```

| Answer | Meaning | Fix |
|---|---|---|
| `200` | The action is in the menu | Nothing. An empty list is not a missing action. |
| `403` from the browser | No `create` on services in the hub namespace | `oc -n periscope policy add-role-to-user admin <user>` |
| `403` from `curl` | The proxy refuses bearer tokens | Apply `charts/periscope` again. Older charts lack `--openshift-delegate-urls`. |
| `503` | The hub cannot check access | Check the hub API server and the auth-delegator binding |

The body of a `403` says which of three causes it is: no token, a token that
could not be reviewed, or access denied. The logs have the same line:

```bash
oc -n periscope logs deploy/periscope -c periscope | grep 'refused an admin request'
```

# The Changes page is quiet during an outage

This is by design. An unreachable cluster records no removals, so an outage
does not show as everything uninstalled and installed again. An upgrade during
the outage shows when the cluster answers again.

# The grouping ConfigMap has no effect

The logs show a `warn` for an invalid `periscope-groups` ConfigMap. Check the
YAML under the `groups.yaml` key. See
[Grouping the matrix](../configuration/groups-configmap.md).
