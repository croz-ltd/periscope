# Periscope

[![CI](https://github.com/croz-ltd/periscope/actions/workflows/ci.yml/badge.svg)](https://github.com/croz-ltd/periscope/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/croz-ltd/periscope.svg)](https://pkg.go.dev/github.com/croz-ltd/periscope)
[![Docker Hub](https://img.shields.io/docker/v/crozltd/periscope?label=docker&sort=semver)](https://hub.docker.com/r/crozltd/periscope)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

**A single pane of glass for version and configuration drift across your OpenShift
fleet.**

Running more than a handful of OpenShift clusters, the question "which cluster is
behind on what?" has no good answer. The console shows one cluster at a time, `oc`
loops don't scale, and the versions that matter most — the CSI driver version buried
inside a Portworx or Dell custom resource, the kubelet on that one straggler node,
the API server certificate that expires in three weeks — aren't in any single view.

Periscope is a single Go binary that scrapes every cluster on an interval, keeps the
full history in an embedded SQLite database, and serves a comparison matrix (plus a
REST API, CSV/JSON export and Prometheus metrics) showing exactly where your fleet
disagrees with itself.

<!-- Add a screenshot here: docs/screenshot.png -->

## What it shows

Per cluster, in one matrix:

| | |
|---|---|
| **Platform** | OpenShift release + channel, node kubelet fleet versions, MachineConfigPool state |
| **Operators** | every OLM-installed operator, keyed by stable package name (auto-discovered — no configuration) |
| **Storage** | default StorageClass, PV/PVC counts per storage class, Portworx and Dell CSI operator *and* managed driver versions |
| **Virtualization** | OpenShift Virtualization version, VM counts by state, VM snapshots per storage class, VM templates |
| **Certificates** | API server and ingress certificate expiry, colour-coded by urgency |

Two views over the same data: **Compare** (version and configuration drift) and
**Statistics** (fleet counts and capacity). Both are groupable via a ConfigMap — see
[`examples/periscope-groups.configmap.yaml`](examples/periscope-groups.configmap.yaml).

### How drift is decided

- The **baseline is the highest version seen across your fleet** — the leader renders
  green, everything behind it shades red, darker with a bigger gap. This is
  deliberately *intra-fleet* drift: Periscope tells you whether your clusters agree
  with each other, not whether they match the newest upstream release. A uniformly
  outdated fleet is all-green, and that is the intended answer.
- **Absent is not behind.** A component that isn't installed on a cluster renders as
  "not installed" and is excluded from the baseline, so partial rollouts stay visible
  without being scored as drift.
- **Never guess.** Version parsing is tolerant (leading `v` stripped, build metadata
  ignored), but an unparseable value renders neutral grey with the raw string shown —
  never lexically compared.
- **Freshness is honest.** If a scrape fails or times out, the last good snapshot is
  kept and the cluster is badged `stale since HH:MM` with the error reason.

## How it works

```
                    ┌─────────── hub cluster ───────────┐
                    │  periscope pod                    │
labeled Secrets ───►│   scheduler ──► SQLite (PVC)      │──► matrix UI + REST/CSV/metrics
(one per cluster)   │      │           full history     │
                    └──────┼────────────────────────────┘
                           │ read-only SA token per cluster
              ┌────────────┼────────────┬────────────┐
           cluster A    cluster B    cluster C    cluster D …
```

Central pull, no per-cluster agents. Clusters are discovered from **labeled Secrets**
in the hub namespace — the Secret's *name* is the cluster's display name, its data is
`apiURL` + `token` (+ optional `caBundle` / `insecureTLS`). There is no implicit
"local" cluster: Periscope starts with an empty matrix, and the hub's own cluster is
joined exactly like any other if you want it compared.

Page loads read the cache; they never fan out live. See [DESIGN.md](DESIGN.md) for the
full rationale and the deliberately deferred v2 items.

## Quick start

### Deploy on OpenShift

```bash
# 1. On the hub: app + oauth-proxy + RBAC + PVC + Route
helm install periscope charts/periscope --set version=latest

# 2. On every cluster you want to compare (including the hub, if you want it in the
#    matrix): create the read-only SA + cluster-scoped read RBAC + a long-lived token
helm install periscope-join charts/periscope-join
```

The join chart's `NOTES` then print the two commands that register the cluster on the
hub — capture the API URL and token, and create the labeled Secret:

```bash
oc -n periscope create secret generic prod-emea \
  --from-literal=apiURL="$API_URL" --from-literal=token="$TOKEN"
oc -n periscope label secret prod-emea periscope.io/cluster=true

# optional: column order in the matrix (lower = further left)
oc -n periscope label secret prod-emea periscope.io/order=10
```

The hub picks the cluster up on the next scrape. UI authentication is handled by an
`oauth-proxy` sidecar against OpenShift SSO — Periscope has no auth code of its own.

### Run the container

```bash
docker run --rm -p 8080:8080 \
  -v "$PWD/data:/data" \
  -v "$HOME/.kube:/root/.kube:ro" \
  crozltd/periscope:latest serve --namespace periscope
```

Images: [`crozltd/periscope`](https://hub.docker.com/r/crozltd/periscope) —
`:latest` and `:<tag>` for releases, `:edge` for the tip of `master`. linux/amd64.

### Build from source

Requires Go 1.26+ and Node 20+.

```bash
make web      # build the PatternFly UI into web/dist (embedded by the Go build)
make build    # build bin/periscope
make test     # go test ./...
make image    # docker build

# serve using your current kubeconfig context
bin/periscope serve --namespace periscope --db ./periscope.db

# one-off matrix printed to stdout (no web UI) — useful in CI
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

`serve` adds `--addr` (`LISTEN_ADDR`, default `:8080`), `--interval` (`10m`),
`--timeout` (per-cluster, `30s`), `--concurrency` (`4`), and `--config` —
an optional file that overrides vendor CR field paths without rebuilding, see
[`config/extractors.yaml`](config/extractors.yaml).

## API

| Endpoint | |
|---|---|
| `GET /` | embedded PatternFly UI |
| `GET /api/matrix` | the full comparison matrix as JSON (what the UI renders) |
| `GET /api/export.csv`, `GET /api/export.json` | current matrix export |
| `POST /api/refresh` | trigger a scrape now |
| `GET /metrics` | Prometheus: per-component drift severity + per-cluster staleness gauges |
| `GET /healthz` | liveness |

## Extending

Adding coverage is usually a small change. If the version you need is a nested field
in a custom resource, no code is required — declare it in the extractor config:

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

For anything else, implement the `Extractor` interface and register it. See
[CONTRIBUTING.md](CONTRIBUTING.md#adding-an-extractor).

## Layout

```
cmd/periscope/            CLI entrypoint (serve | report | version)
internal/
  version/                tolerant semver parse + compare      (unit-tested)
  drift/                  build the comparison matrix          (unit-tested)
  model/                  shared data types
  extract/                per-cluster extractors + registry
                            openshift.go   ClusterVersion (release + channel)
                            olm.go         all OLM operators, keyed by package
                            nodes.go       kubelet fleet, node counts
                            mcp.go         MachineConfigPools
                            storage.go     default StorageClass
                            volumes.go     PV/PVC counts per storage class
                            certs.go       API + ingress certificate expiry
                            virt.go        OpenShift Virtualization
                            vm.go          VM counts, snapshots, templates
                            crfield.go     Portworx + Dell CSI (nested CR fields)
  cluster/                discover clusters from labeled Secrets
  scrape/                 periodic parallel scrape scheduler
  store/                  SQLite persistence (full snapshot history)
  api/                    REST + CSV/JSON export + /metrics
  report/                 text-table report for the CLI
  config/                 optional extractor configuration
web/                      go:embed'd UI — PatternFly app in web/app, built to web/dist
charts/
  periscope/              hub chart (Deployment, oauth-proxy, RBAC, PVC, Route)
  periscope-join/         per-cluster read-only SA + token + RBAC
tekton/                   example on-cluster build (OpenShift Pipelines)
examples/                 custom grouping ConfigMap
```

## CI/CD

GitHub Actions:

- **[`ci.yml`](.github/workflows/ci.yml)** — on every push and PR: build the UI
  (shared as an artifact, since the Go build embeds it), `go vet`, `go test`,
  cross-platform binaries, and a container build.
- **[`release.yml`](.github/workflows/release.yml)** — pushes the image to Docker Hub
  (`:edge` + `:<sha>` on `master`, `:<tag>` + `:latest` on tags) and, for tags,
  attaches binaries and `SHA256SUMS` to a GitHub Release.

Image publishing needs two repository secrets: `DOCKERHUB_USERNAME` and
`DOCKERHUB_TOKEN` (a Docker Hub access token with push rights). The image name can be
overridden with the `IMAGE_NAME` repository variable.

Building on a cluster instead of in a hosted runner? [`tekton/`](tekton/README.md) has
a self-contained OpenShift Pipelines pipeline (git-clone + buildah) that builds the
same `Dockerfile` and pushes to a registry of your choice.

## Before running against a real fleet

**Verify the Portworx / Dell CR field paths.** The defaults in
`internal/extract/crfield.go` (`status.version`, `spec.driver.configVersion`) are
best-effort. Confirm against your clusters with
`oc get storagecluster,containerstoragemodule -o yaml` and, if they differ, override
them via `--config` rather than patching the code.

**Watch extractor errors, not empty cells.** The reader SA binds OpenShift's
`cluster-reader`, so missing RBAC is largely moot — but if you narrow that role, a
missing rule returns `403`, which the scheduler records in the snapshot error. An
empty cell alone doesn't prove a component is absent.

## Contributing

Issues and PRs welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). Security reports go
through the process in [SECURITY.md](SECURITY.md), not public issues.

## License

[Apache License 2.0](LICENSE) © CROZ d.o.o.
