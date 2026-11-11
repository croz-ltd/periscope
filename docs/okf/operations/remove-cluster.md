---
type: Playbook
title: Remove a cluster and purge its history
description: Delete the hub Secret to stop scraping a cluster, then use Purge stale data to remove the history it left.
tags: [remove, purge, offboarding, operations]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md#remove-a-cluster-and-purge-what-it-left-behind
    title: "README: Remove a cluster"
---

# Step 1: stop scraping

Delete the Secret of the cluster on the hub:

```bash
oc -n periscope delete secret prod-emea
```

The next scrape skips the cluster. Its history stays. This is deliberate: a
cluster removed for an afternoon comes back with its past intact, and the
Changes page and the timeline still show it. Until you purge, the matrix keeps
a grey column for it, marked stale.[^readme]

# Step 2: clean the joined cluster (optional)

On the removed cluster, delete what the join created. If you joined with the
chart, `helm uninstall periscope-join` does it. Otherwise:

```bash
oc delete clusterrolebinding periscope-reader
oc -n periscope delete serviceaccount periscope-reader   # its token stops working
oc -n periscope delete secret periscope-reader-token
oc delete namespace periscope
```

Do not delete the `periscope` namespace when the removed cluster is also the
hub. On the hub, that namespace holds Periscope itself.

# Step 3: purge the history

1. Open **Actions**, then **Purge stale data**.
2. The dialog lists every cluster that has history in the database but no
   Secret, with how much data each holds.
3. Tick the clusters to purge and confirm.

Purge cannot be undone. If the cluster joins again, its history starts again
from the next scrape.

# Who can purge

* The reader needs `create` on services in the hub namespace. The built-in
  `admin` and `edit` roles include it:
  `oc -n periscope policy add-role-to-user admin <user>`.
* A cluster that still has its Secret cannot be purged, not from the UI and not
  from the API.

If the action is missing, see "The Purge action is missing" in
[Troubleshooting](troubleshooting.md).

[^readme]: README: Remove a cluster
