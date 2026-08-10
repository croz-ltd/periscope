# Periscope

[![CI](https://github.com/croz-ltd/periscope/actions/workflows/ci.yml/badge.svg)](https://github.com/croz-ltd/periscope/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/croz-ltd/periscope.svg)](https://pkg.go.dev/github.com/croz-ltd/periscope)
[![Docker Hub](https://img.shields.io/docker/v/crozltd/periscope?label=docker&sort=semver)](https://hub.docker.com/r/crozltd/periscope)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Periscope shows version and configuration drift across a fleet of OpenShift
clusters in one view.

Once you run more than a handful of clusters, "which cluster is behind on what?"
becomes hard to answer. The console shows one cluster at a time, `oc` loops don't
scale, and some of the versions that matter most are not in any single view: the CSI
driver version buried inside a Portworx or Dell custom resource, the kubelet on a
straggler node, the API server certificate that expires in three weeks.

Periscope is a single Go binary that scrapes every cluster on an interval, keeps the
full history in an embedded SQLite database, and serves a comparison matrix with a
REST API, CSV/JSON export and Prometheus metrics.

<!-- Add a screenshot here: docs/screenshot.png -->

## What it shows

Per cluster, in one matrix:

| | |
|---|---|
| Platform | OpenShift release and channel, node kubelet versions, MachineConfigPool state |
| Operators | every OLM-installed operator, keyed by stable package name (auto-discovered, no configuration) |
| Storage | default StorageClass, PV/PVC counts per storage class, Portworx and Dell CSI operator and managed driver versions |
| Virtualization | OpenShift Virtualization version, VM counts by state, VM snapshots per storage class, VM templates |
| Certificates | API server and ingress certificate expiry, colour-coded by urgency |

There are two views over the same data: Compare, for version and configuration drift,
and Statistics, for fleet counts and capacity. Both can be grouped through a
ConfigMap, see [`examples/periscope-groups.configmap.yaml`](examples/periscope-groups.configmap.yaml).

### How drift is decided

The baseline for each component is the highest version seen across your fleet. That
cluster renders green and the ones behind it shade red, darker with a bigger gap. This
is intra-fleet drift, so Periscope tells you whether your clusters agree with each
other, not whether they match the newest upstream release. A fleet that is uniformly
outdated shows all green, which is the intended answer.

A component that isn't installed on a cluster renders as "not installed" and is left
out of the baseline, so a partial rollout stays visible without being scored as drift.

Version parsing is tolerant: a leading `v` is stripped and build metadata ignored. If
a value still doesn't parse, the cell renders neutral grey with the raw string shown,
because a lexical comparison of something that isn't a version is worse than no
comparison at all.

When a scrape fails or times out, the last good snapshot is kept and the cluster gets
a `stale since HH:MM` badge with the error reason, so you can tell stale data from
current data.

## How it works

```
                    ┌─────────── hub cluster ───────────┐
                    │  periscope pod                    │
labeled Secrets ───►│   scheduler ──► SQLite (PVC)      │──► matrix UI + REST/CSV/metrics
(one per cluster)   │      │           full history     │
                    └──────┼────────────────────────────┘
                           │ read-only SA token per cluster
              ┌────────────┼────────────┬────────────┐
           cluster A    cluster B    cluster C    cluster D ...
```

The hub pulls from every cluster, so there are no per-cluster agents. Clusters are
discovered from labeled Secrets in the hub namespace. The Secret's name is the
cluster's display name and its data is `apiURL` plus `token`, with optional
`caBundle` or `insecureTLS`. There is no implicit "local" cluster: Periscope starts
with an empty matrix, and the hub's own cluster is joined like any other if you want
it compared.

Page loads read the cache and never fan out live. [DESIGN.md](DESIGN.md) covers the
reasoning behind these choices and what was left for later.

## Quick start

### Deploy on OpenShift

```bash
# 1. On the hub: app + oauth-proxy + RBAC + PVC + Route
helm install periscope charts/periscope --set version=latest

# 2. On every cluster you want to compare (including the hub, if you want it in the
#    matrix): create the read-only SA, cluster-scoped read RBAC and a long-lived token
helm install periscope-join charts/periscope-join
```

The join chart's `NOTES` then print the two commands that register the cluster on the
hub. Capture the API URL and token, then create the labeled Secret:

```bash
oc -n periscope create secret generic prod-emea \
  --from-literal=apiURL="$API_URL" --from-literal=token="$TOKEN"
oc -n periscope label secret prod-emea periscope.io/cluster=true

# optional: column order in the matrix (lower sorts further left)
oc -n periscope label secret prod-emea periscope.io/order=10
```

The hub picks the cluster up on the next scrape. UI authentication is handled by an
`oauth-proxy` sidecar against OpenShift SSO, so Periscope has no auth code of its own.

### Run the container

```bash
docker run --rm -p 8080:8080 \
  -v "$PWD/data:/data" \
  -v "$HOME/.kube:/root/.kube:ro" \
  crozltd/periscope:latest serve --namespace periscope
```

The UI is then on http://localhost:8080. Without joined clusters the matrix is
empty, which is the expected starting state.

Images live at [`crozltd/periscope`](https://hub.docker.com/r/crozltd/periscope):
`:latest` and `:<tag>` for releases, `:edge` for the tip of `master`. They are built
for linux/amd64 only, matching the clusters they run on, so on an arm64 machine
(Apple silicon) add `--platform linux/amd64` to the command above and expect
emulation. Building locally with `make image` gives you a native image instead.

### Build from source

Requires Go 1.26+ and Node 20+.

```bash
make web      # build the PatternFly UI into web/dist (embedded by the Go build)
make build    # build bin/periscope
make test     # go test ./...
make image    # docker build

# serve using your current kubeconfig context
bin/periscope serve --namespace periscope --db ./periscope.db

# one-off matrix printed to stdout (no web UI), useful in CI
bin/periscope report --db ./periscope.db
```

## CLI

```
periscope serve    scrape on an interval and serve the UI + API   (default)
periscope report   print the latest matrix as a text table
periscope version  print the build version
```

Flags common to `serve` and `report`:

| Flag | Default | Meaning |
|---|---|---|
| `--namespace` | pod namespace, else `periscope` | hub namespace holding the joined-cluster Secrets |
| `--label-key` / `--label-value` | `periscope.io/cluster` / `true` | label marking those Secrets |
| `--db` | `/data/periscope.db` (`DB_PATH`) | SQLite database path |
| `--stale-after` | `30m` | mark a cluster stale when its last scrape is older than this |
| `--log-level` | `info` (`LOG_LEVEL`) | `debug` \| `info` \| `warn` \| `error` |
| `--log-format` | `text` (`LOG_FORMAT`) | `text` to read by eye, `json` for a log collector |

`serve` adds `--addr` (`LISTEN_ADDR`, default `:8080`), `--interval` (`10m`),
`--timeout` (per cluster, `30s`), `--concurrency` (`4`), and `--config`, an optional
file that overrides vendor CR field paths without rebuilding. See
[`config/extractors.yaml`](config/extractors.yaml).

## Logs

Everything logs through `log/slog`, tagged with the subsystem that produced it
(`component=scrape`, `cluster`, `store`, `api`, `extract`), so a fleet of thirty
clusters can be read by filtering rather than by grepping prose. The chart sets
`LOG_FORMAT=json` so OpenShift's log collector indexes the fields; a terminal is
better served by `text`.

**info** is what a healthy hub says, and is meant to stay readable: one line per
scrape cycle with how many clusters were degraded or unreachable, one per cluster
with its component count and duration, plus discovery results and startup
configuration. Anything that silently degrades the result is a **warn**: an
extractor that failed (with how much of the per-cluster deadline was left when it
gave up, which separates a slow cluster from a broken extractor), a labeled Secret
missing its credentials, a cluster scraped without a CA bundle, an invalid grouping
ConfigMap.

**debug** explains one scrape in full: every extractor with its duration and
component count, every resource skipped because its CRD is not installed, every
snapshot saved with how many changes it recorded, and every HTTP request. It is
the level to raise while diagnosing, and to lower again afterwards.

```
level=WARN msg="extractor failed" component=scrape cluster=erls-p extractor=olm \
  duration=12.3s remaining=-1.2s error="context deadline exceeded"
level=WARN msg="cluster scraped" component=scrape cluster=erls-p components=42 \
  extractorsFailed=5 duration=30.0s
```

That pair is what a scrape timing out mid-list looks like, which is worth
recognising: the components the failed extractors would have read are missing from
that snapshot, and the matrix shows them as not installed until the next good scrape.

## API

| Endpoint | |
|---|---|
| `GET /` | embedded PatternFly UI |
| `GET /api/matrix` | the full comparison matrix as JSON (what the UI renders) |
| `GET /api/matrix?at=<RFC3339>` | the matrix as it stood at that moment |
| `GET /api/changes` | the change feed, newest first (`from`, `to`, `cluster`, `limit`) |
| `GET /api/changes/calendar` | per-day change counts, for marking a calendar |
| `GET /api/export.csv`, `GET /api/export.json` | current matrix export, `at` honoured |
| `POST /api/refresh` | trigger a scrape now |
| `GET /api/version` | the version stamped into this binary |
| `GET /metrics` | Prometheus text exposition, see below |
| `GET /healthz` | liveness |

Two gauges are exported, both suitable for alerting:

```
periscope_component_drift_severity{cluster,component,state}  # 0 on the fleet leader
periscope_cluster_stale{cluster}                             # 1 when the snapshot is stale
```

## Changes and time travel

Every scrape has always been kept, so the **Changes** page reads that history back
as an audit log: what appeared, what was upgraded, what was uninstalled, and when a
cluster stopped answering. A scrape that found the fleet exactly as it was records
nothing, so the feed holds events rather than heartbeats.

The calendar beside it marks the days something happened, darker as more changed.
Pick a day to read that day's events, then **View the matrix as it was** to load the
whole matrix as of that moment, exports included. A banner says you are looking at
history until you leave it.

Two silences are deliberate, because a feed nobody trusts is a feed nobody reads.
An unreachable cluster reports nothing, so its components are not filed as removed
and re-added around every outage; changes are measured against the last *successful*
scrape, so an upgrade that happened during an outage is still reported when the
cluster comes back. And a scrape with a partial error cannot tell "uninstalled" from
"could not read", so removals wait for a clean scrape.

The feed is derived from the snapshot history rather than being source data, so a
correction to how changes are detected also has to reach the events already
recorded. Each release stamps the logic it used; when that changes, the last 90
days are re-derived from stored history on the next start. The rebuild runs in the
background (tens of seconds on a fleet with months of history) and swaps in when it
commits, so the pod serves throughout and nothing is half-replaced.

Counters that move on nearly every scrape (VM totals, snapshot volumes) are recorded
but hidden behind the **Include counters** switch, and the calendar counts them the
same way the feed does. A day's colour is how much really changed; a day whose only
news was counters moving gets a dot instead, so it does not read as empty and does
not read as busy either. Turning counters on folds them into the colour, because then
they are what you came to look at.

## Knowing what can be upgraded

Being behind the fleet only matters if there is somewhere to go, so Periscope also
reports what each cluster can actually do about it:

- **Update available** — the newest release the cluster is being offered. The cluster
  has already asked the update service on its own channel, so this is read from its
  ClusterVersion and needs no egress from the hub.
- **Upgrade blocked** — an `Upgradeable=False` condition, with the reason and message.
  This is the difference between a cluster that is behind and one that is stuck.
- **Update in progress** — set while an upgrade is running, which explains a version
  that disagrees with the rest of the fleet for the next hour.
- **Operator updates pending** and **InstallPlans awaiting approval** — updates OLM
  has resolved but not applied. On manual approval these sit unnoticed for months,
  and they are the usual reason a cluster quietly falls behind.

## Naming columns from the cluster

If a cluster carries a `ConsoleNotification` named `cluster-name`, its text and
colours head that column, so the matrix labels clusters the way their own operators
already label them in the OpenShift console. The joined name stays in the tooltip and
in exports, metrics and the API. Clusters without one keep their joined name.

## Extending

Adding coverage is usually a small change. If the version you need is a nested field
in a custom resource, no code is required, just declare it in the extractor config:

```yaml
crExtractors:
  - key: my-csi
    display: My CSI
    kind: csi
    group: storage.example.com
    version: v1
    resource: mydrivers
    versionPath: [spec, driver, configVersion]
```

Anything else means implementing the `Extractor` interface and registering it. See
[CONTRIBUTING.md](CONTRIBUTING.md#adding-an-extractor).

## Layout

```
cmd/periscope/            CLI entrypoint (serve | report | version)
internal/
  version/                tolerant semver parse + compare      (unit-tested)
  drift/                  build the comparison matrix          (unit-tested)
  model/                  shared data types
  extract/                per-cluster extractors + registry
                            openshift.go   ClusterVersion (release, channel, updates)
                            console.go     console banner used to name the column
                            olm.go         OLM operators + pending updates/InstallPlans
                            nodes.go       kubelet versions, node counts
                            mcp.go         MachineConfigPools
                            storage.go     default StorageClass
                            volumes.go     PV/PVC counts per storage class
                            certs.go       API + ingress certificate expiry
                            virt.go        OpenShift Virtualization
                            vm.go          VM counts, snapshots, templates
                            crfield.go     Portworx + Dell CSI (nested CR fields)
  cluster/                discover clusters from labeled Secrets
  scrape/                 periodic parallel scrape scheduler
  store/                  SQLite persistence (history + recorded change feed)
  api/                    REST + CSV/JSON export + /metrics
  report/                 text-table report for the CLI
  config/                 optional extractor configuration
web/                      go:embed'd UI, PatternFly app in web/app, built to web/dist
charts/
  periscope/              hub chart (Deployment, oauth-proxy, RBAC, PVC, Route)
  periscope-join/         per-cluster read-only SA + token + RBAC
tekton/                   example on-cluster build (OpenShift Pipelines)
examples/                 custom grouping ConfigMap
```

## CI/CD

Two GitHub Actions workflows:

[`ci.yml`](.github/workflows/ci.yml) runs on every push and pull request. It builds
the UI once and shares it as an artifact, because the Go build embeds `web/dist`, then
runs `go vet`, `go test`, cross-platform builds and a container build.

[`release.yml`](.github/workflows/release.yml) pushes the image to Docker Hub, tagged
`:edge` and `:<sha>` from `master` and `:<tag>` and `:latest` from tags. On tags it
also attaches the binaries and `SHA256SUMS` to a GitHub Release.

### Versions

The release lives in source, as `Base` in [`pkg/version`](pkg/version/version.go), and
every build reports it. Builds that are not a release tag append build metadata saying
where they came from, so a version is always traceable to both a release and a commit:

```
0.2.0             a release build of the v0.2.0 tag
0.2.0+ci-1a2b3c4  the :edge image, or a CI binary, at that commit
0.2.0+tekton-...  built on a cluster by tekton/, tagged with the image tag
0.2.0+dev         a local `make build`
```

The running value is shown on the UI's About page, printed by `periscope version`, and
served from `/api/version`.

Cutting a release means bumping `Base`, both chart versions and `web/app/package.json`
in one commit, then tagging it. The tag must match `Base`, which
[`check-release-tag.sh`](.github/check-release-tag.sh) enforces before anything is
published, so a mistagged release cannot ship binaries that misreport themselves.

Image publishing needs two repository secrets, `DOCKERHUB_USERNAME` and
`DOCKERHUB_TOKEN` (a Docker Hub access token with push rights). Without them the push
is skipped rather than failed. The image name can be overridden with the `IMAGE_NAME`
repository variable.

If you would rather build on a cluster than in a hosted runner,
[`tekton/`](tekton/README.md) has a self-contained OpenShift Pipelines pipeline
(git-clone plus buildah) that builds the same `Dockerfile` and pushes to a registry of
your choice.

## Before running against a real fleet

Verify the Portworx and Dell CR field paths. The defaults in
`internal/extract/crfield.go` (`status.version`, `spec.driver.configVersion`) are
best-effort. Confirm them against your clusters with
`oc get storagecluster,containerstoragemodule -o yaml` and override them via
`--config` rather than patching the code.

Watch extractor errors rather than empty cells. The reader SA binds OpenShift's
`cluster-reader`, so missing RBAC is mostly a non-issue, but if you narrow that role a
missing rule returns `403`, which the scheduler records in the snapshot error. An empty
cell on its own does not prove that a component is absent.

## Contributing

Issues and pull requests are welcome, see [CONTRIBUTING.md](CONTRIBUTING.md). Security
reports go through the process in [SECURITY.md](SECURITY.md) rather than public issues.

## License

[Apache License 2.0](LICENSE), Copyright CROZ d.o.o.
