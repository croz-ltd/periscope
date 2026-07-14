# Cluster Comparator — Design

Single pane of glass for version drift across 8 OpenShift clusters. A Go CLI that
serves an embedded PatternFly React UI backed by a small REST API, running as a pod
in a "hub" cluster, pulling inventory from itself and joined clusters.

## Architecture (resolved)

**Topology: central pull.** The hub pod reaches all 7 remote API servers directly
(confirmed reachable). No per-cluster agents.

**Data flow: periodic scrape → cache → read.** A background scheduler polls every
cluster on an interval (parallel, per-cluster timeout). Results land in an embedded
store. Page loads and API calls read the cache — never fan out live.

**Store: SQLite on a PVC, full snapshot history.** Survives pod restarts, no external
DB. Every scrape is retained so we can render drift-over-time timelines (which version
landed where, when), not just the latest snapshot.

**Cluster registry: labeled Secrets (no ConfigMap).** The hub discovers clusters by
listing Secrets in its namespace carrying a custom label. Convention:
- cluster **name** = Secret name
- Secret data = `apiURL` + `token` (read-only SA token from the cluster)
- optional `caBundle` / `insecureTLS`

**Every cluster is a Secret; there is no special "local" target.** The app starts
with zero clusters and an empty matrix — clusters appear only as labeled Secrets are
added. The hub's in-cluster credentials are used only to *read the Secrets*. The hub's
own cluster is optional and, if you want it compared, is joined exactly like any other
(apply the join chart, create a labeled Secret pointing at its API). Uniform naming
and RBAC across all clusters.

## Version model (resolved)

**Discovery:** auto-enumerate all OLM `ClusterServiceVersion`s for broad operator
coverage, PLUS a registry of hand-written deep extractors for special components.

**Deep extractors (v1):**
- `openshift` — `ClusterVersion` (clusterversion/version): release + channel/status
- `portworx-csi` — parse `StorageCluster` CR: operator version AND managed CSI driver version
- `dell-csi` — parse `ContainerStorageModule` / CSM CRs: operator + managed CSI driver version
- `nodes` — per-node kubelet/OS versions (straggler nodes mid-upgrade)

**Component key (matrix row identity):** stable OLM package name (Subscription
`spec.name` / `olm.package` label), falling back to CSV `metadata.name` with the
version suffix stripped. Deep-extractor components use explicit stable keys
(`openshift`, `portworx-csi`, `dell-csi`).

**Drift baseline:** per-component **max semver seen across the fleet** = the leader
(green). Others shade red, darker with larger gap. This is deliberately *intra-fleet*
drift only — we care whether the 8 clusters agree with each other, NOT whether they
lag the latest upstream release. A uniformly-outdated fleet showing all-green is the
correct, intended behavior. No declared-target or catalog-latest baseline in scope.

**Version parsing:** tolerant — strip leading `v`, ignore build metadata, coerce
common forms. Unparseable → neutral grey cell, raw string shown, **no** drift shading.
Never guess drift on garbage; never lexical-compare.

**Missing cells:** a component absent on a cluster renders as a distinct "not installed"
style (dash/hatch) and is **excluded from the baseline** — absence never counts as
"behind". Partial-rollout drift stays visible.

## Access & security (resolved)

- **UI auth:** oauth-proxy sidecar → OpenShift SSO + RBAC check. Read-only app, no
  custom auth code.
- **Remote creds:** per-cluster read-only ServiceAccount tokens, stored as labeled
  Secrets on the hub. Least-privilege, revocable per cluster.
- **Reader RBAC mode (default `clusterReader`):** the read SA on each cluster binds to
  OpenShift's built-in `cluster-reader` ClusterRole — read-only cluster-wide, so new
  operator CRDs never 403 and adding an extractor needs no RBAC change. Trade-off: broad
  read on every joined cluster. `explicit` mode is the hardened alternative: a curated
  least-privilege ClusterRole (ClusterVersion/ClusterOperators, CSVs/Subscriptions,
  nodes, Portworx `storageclusters`, Dell `containerstoragemodules`, + `additionalRules`).
- **Hub RBAC:** namespaced Role to list/watch labeled Secrets; the same reader mode for
  self-scraping the local cluster; `system:auth-delegator` binding for oauth-proxy.
- **Confirmed CRDs:** Portworx `core.libopenstorage.org/v1 storageclusters`;
  Dell CSM `storage.dell.com/v1 containerstoragemodules`.

## Failure behavior (resolved)

On scrape failure/timeout: keep last-good snapshot, show a `stale since HH:MM` badge
and a visible error indicator with reason. Report stays useful; freshness is honest.

## Delivery (resolved)

- **Frontend:** PatternFly React + Vite; build output embedded via `go:embed` →
  single binary. PatternFly matches the OpenShift console and gives table / empty-state
  / a11y / color tokens for free.
- **Data-out surfaces (v1):**
  - REST JSON API (the UI backend; also script-consumable)
  - CSV / JSON export of the current matrix
  - Prometheus `/metrics` (version info + drift gap gauges for alerting)
  - `report` CLI subcommand (prints the matrix to terminal/CI, no web UI)
- **Packaging: two Helm charts.**
  - *hub* chart: Deployment, oauth-proxy sidecar, SA + RBAC, PVC, Route, documents the
    labeled-Secret join convention.
  - *joinee* chart: creates the read-only SA + token on a cluster to be joined and
    emits the labeled Secret to install on the hub.

## Implementation notes (defaults, not separately grilled)

- **K8s access:** `client-go`; build a `rest.Config` per cluster from stored
  `apiURL`+`token`. Typed clientset for core/nodes; dynamic (unstructured) client for
  CSVs and arbitrary vendor CRs so extractors don't need generated types.
- **Extractor interface:** `Extract(ctx, dynClient) ([]Component, error)` registered by
  key; scheduler runs OLM enumeration + all registered extractors per cluster.
- **Scheduler:** configurable interval, bounded concurrency across clusters,
  per-cluster context timeout; each cluster's result committed independently.
- **Server:** Go stdlib `net/http` (or chi) serving `/api/*` + embedded static assets.

## Explicitly deferred to v2

- Declared/target and catalog-latest drift baselines (v1 is max-seen only)
- Runtime "join cluster" UI (v1 join = apply the joinee chart + Secret)
- Auto-discovery via ACM
- Non-OLM (raw Helm/manifest) operator versions beyond the deep extractors
