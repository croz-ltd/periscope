# Cluster Comparator

Single pane of glass for **version drift across OpenShift clusters** — releases,
operators, and the CSI driver versions buried inside vendor CRs (Portworx, Dell).
A Go binary that scrapes every cluster on an interval, stores history in SQLite,
and serves a matrix UI + REST API. See [DESIGN.md](DESIGN.md) for the full design.

## Layout

```
cmd/cluster-comparator/   CLI entrypoint (serve | report)
internal/
  version/                tolerant semver parse + compare      (unit-tested)
  drift/                  build the comparison matrix           (unit-tested)
  model/                  shared data types
  extract/                per-cluster extractors + registry
      openshift.go          ClusterVersion
      olm.go                all OLM operators (keyed by package)
      nodes.go              kubelet fleet version
      crfield.go            Portworx + Dell CSI (nested CR fields)
  cluster/                discover clusters from labeled Secrets
  scrape/                 periodic parallel scrape scheduler
  store/                  SQLite persistence (full history)
  api/                    REST + CSV/JSON export + /metrics
  report/                 text-table report for the CLI
web/                      go:embed'd UI (dist/); PatternFly app lands in web/app
charts/
  cluster-comparator/     hub Helm chart (SA, RBAC, oauth-proxy — runtime TODO)
  cluster-comparator-join/ per-cluster read-only SA + token + RBAC
```

## Run

```bash
make web          # build the PatternFly UI into web/dist
make build        # build the Go binary (embeds web/dist)
make test         # go test ./...
make image        # docker build the container

# Serve — uses in-cluster creds in a pod, or your kubeconfig locally
bin/cluster-comparator serve --namespace cluster-comparator --db ./cc.db

# One-off report to stdout
bin/cluster-comparator report --db ./cc.db

# Optional: override Portworx/Dell CR field paths without rebuilding
bin/cluster-comparator serve --config config/extractors.yaml
```

Key endpoints: `/` (UI), `/api/matrix`, `/api/export.csv`, `/api/export.json`,
`/api/refresh` (POST), `/metrics`, `/healthz`.

## Deploy

```bash
# On the hub: app + oauth-proxy + RBAC + PVC + Route
helm install cc charts/cluster-comparator

# On EVERY cluster you want compared — INCLUDING the hub's own cluster —
# create a read-only SA + RBAC + long-lived token:
helm install cc-join charts/cluster-comparator-join
```

There is no implicit "local" cluster: the hub's own cluster is added the same way
as the others. For each cluster, follow the join chart's NOTES to create a labeled
Secret in the hub namespace — its **name** becomes the cluster's display name.

## CI/CD (GitLab)

`.gitlab-ci.yml` runs: `build-web` (Vite → `web/dist`, shared as an artifact since
the Go build embeds it) → `test` (vet + go test) → cross-platform binaries
(darwin-arm64, linux-amd64, windows-amd64) → a **`docker build` image push to Harbor**
→ Nexus upload + GitLab Release (on tags).

The image (linux/amd64 only, no multiarch) is built from the multi-stage `Dockerfile`,
so the pushed container builds and serves the embedded web UI. It pushes
`:<sha>`/`:edge` on the default branch and `:<tag>`/`:latest` on tags.

Required before first run:
- Harbor auth resolved at the CI/runner level (e.g. `DOCKER_AUTH_CONFIG` or runner
  config) with push rights — no login step in the job.
- Set **`IMAGE`** in `.gitlab-ci.yml` to your real Harbor project path.
- Ensure the toolchain image tags exist in Harbor's proxy (`golang:1.26.4`,
  `node:20-alpine`, `docker:20.10.16-dind`).
- The `build-image` job runs on the `docker:dind` image directly (no services) and
  assumes the runner provides a Docker daemon (privileged runner / mounted socket).

Build the image locally:

```bash
make image                 # docker build -t ghcr.io/croz-ltd/cluster-comparator:dev .
docker run -p 8080:8080 ghcr.io/croz-ltd/cluster-comparator:dev
```

## Test

```bash
go test ./...
```

## Before running against real clusters — verify

1. **Portworx / Dell CR field paths** in `internal/extract/crfield.go` are
   best-effort defaults (`status.version`, `spec.driver.configVersion`). Confirm
   with `oc get storagecluster/containerstoragemodule -o yaml` and adjust.
2. **Distinguish forbidden from absent.** In the default `clusterReader` RBAC
   mode this is largely moot, but if you switch to `explicit` mode a missing rule
   returns a 403 — the scheduler records it in the snapshot error, so watch for
   extractor errors rather than trusting an empty cell.

## Status

Done: engine (extractors, scheduler, SQLite history, drift matrix), REST/CSV/JSON/metrics
API, PatternFly UI (`web/app` → embedded `web/dist`), both Helm charts (hub runtime +
join RBAC), config-driven extractor paths, and the container build.

Before production, verify the Portworx/Dell CR field paths (see below) against real
clusters, and — if you deploy a private image — push it to your registry and set
`image.repository`/`image.tag` in the hub chart's values.
