# Contributing to Periscope

Thanks for your interest. Issues and pull requests are welcome.

## Development setup

You need Go 1.26+, Node 20+, and optionally Docker or Podman.

```bash
git clone https://github.com/croz-ltd/periscope.git
cd periscope

make web      # build the PatternFly UI into web/dist (the Go build embeds it)
make build    # build bin/periscope
make test     # go test ./...
make vet      # go vet ./...
```

Run `make web` at least once before `make build`. `web/embed.go` embeds `web/dist`
with `go:embed`, so a stale or missing `web/dist` means a stale or failing build.

Run locally against your current kubeconfig context:

```bash
bin/periscope serve --namespace periscope --db ./periscope.db
```

Periscope reads joined clusters from labeled Secrets in `--namespace`. With none
present it starts with an empty matrix. The README describes the join flow.

For UI-only work, `cd web/app && npm run dev` gives you Vite with hot reload against a
running backend. The dev server proxies `/api`, see `web/app/vite.config.ts`.

With no fleet to point it at, use `make web-mock` (`npm run dev:mock`) instead. It runs
the same dev server with a middleware answering `/api/*` from the synthetic fleet in
[`web/app/mock/`](web/app/mock/): eight clusters with real drift, a stale one, one
reporting an extractor error, expiring certificates and a month of change history. Add
to the fixture when you add a row type, so the next person can see it without a
cluster. It is typed against the interfaces in `web/app/src/api.ts`, so a change to a
response shape fails `npm run typecheck` here too, and it is a plugin enabled only by
`--mode mock`, never imported from `src/`, so it cannot reach a production bundle.
The README's screenshots are taken from it.

## Project layout

[DESIGN.md](DESIGN.md) has the architectural reasoning behind this shape.

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
                            grafana.go     Grafana version the operator manages
  cluster/                discover clusters from labeled Secrets
  scrape/                 periodic parallel scrape scheduler
  store/                  SQLite persistence (history + recorded change feed)
  api/                    REST + CSV/JSON export + /metrics
  report/                 text-table report for the CLI
  config/                 optional extractor configuration
web/                      go:embed'd UI, PatternFly app in web/app, built to web/dist
  app/mock/               synthetic fleet + dev-server middleware (npm run dev:mock)
charts/
  periscope/              hub chart (Deployment, oauth-proxy, RBAC, PVC, Route)
  periscope-join/         per-cluster read-only SA + token + RBAC
tekton/                   example on-cluster build (OpenShift Pipelines)
examples/                 custom grouping ConfigMap
docs/                     screenshots used by the README
```

## Adding an extractor

Most contributions are new extractors. An extractor implements:

```go
type Extractor interface {
    Key() string
    Extract(ctx context.Context, c *Clients) ([]model.Component, error)
}
```

A few rules that are easy to break and hard to notice:

1. Use a stable component key. The key is the matrix row identity, so it must not
   change between versions of the component being extracted.
2. Return no component when the thing is not installed, never a zero version. Absent
   components are excluded from the drift baseline, and a zero version makes an
   uninstalled component look like the most outdated one in the fleet.
3. Never guess a version. If the value does not parse, return it as the raw string.
   `internal/version` marks unparseable values neutral instead of comparing them
   lexically.
4. Use the dynamic client for vendor CRs so no generated types are needed, and gate
   the call on `c.HasResource(gvr)`. Listing a resource whose CRD is absent comes
   back Forbidden, not 404, because a read-only account has no rule for it. Without
   that gate, every cluster not running the vendor carries a scrape error.
5. If the version is just a nested field in a CR, prefer the config-driven extractor.
   `extract.NewCRFieldExtractor(...)` covers that case and lets users override the
   field path through `config/extractors.yaml` without rebuilding.
6. Add unit tests. `internal/version` and `internal/drift` have tests to follow, and
   extractors can be tested against fake dynamic clients.

Register the extractor in `extract.Default()`. The UI's Docs page lists component keys
from the API, so a new key appears there automatically.

## Pull requests

- Keep commits focused and use [Conventional Commits](https://www.conventionalcommits.org/)
  subjects, for example `feat(extract): ...`, `fix(ui): ...`, `docs: ...`,
  `chore(chart): ...`.
- Explain why in the commit body when the diff does not make it obvious.
- `go vet ./...` and `go test ./...` must pass. CI runs both plus a UI typecheck,
  cross-platform builds and a container build.
- Say in the PR description whether you verified the change against a real cluster and
  on which OpenShift version.

## CI

Two GitHub Actions workflows.

[`ci.yml`](.github/workflows/ci.yml) runs on every push and pull request. It builds
the UI once and shares it as an artifact, because the Go build embeds `web/dist`, then
runs `go vet`, `go test`, a UI typecheck, cross-platform builds and a container build.

[`release.yml`](.github/workflows/release.yml) pushes the image to Docker Hub, tagged
`:edge` and `:<sha>` from `master` and `:<tag>` and `:latest` from tags. On tags it
also attaches the binaries and `SHA256SUMS` to a GitHub Release.

Image publishing needs two repository secrets, `DOCKERHUB_USERNAME` and
`DOCKERHUB_TOKEN` (a Docker Hub access token with push rights). Without them the push
is skipped rather than failed. The image name can be overridden with the `IMAGE_NAME`
repository variable.

To build on a cluster instead of a hosted runner,
[`tekton/`](tekton/README.md) has a self-contained OpenShift Pipelines pipeline
(git-clone plus buildah) that builds the same `Dockerfile` and pushes to a registry of
your choice.

## Cutting a release

The release lives in source, as `Base` in [`pkg/version`](pkg/version/version.go), and
every build reports it (see the README's [version
strings](README.md#version-strings)). Four places have to move together, in one commit:

- `Base` in `pkg/version/version.go`
- `version` and `appVersion` in `charts/periscope/Chart.yaml`
- `version` and `appVersion` in `charts/periscope-join/Chart.yaml`
- `version` in `web/app/package.json` and its lock file

Then tag it. The tag must match `Base`, which
[`check-release-tag.sh`](.github/check-release-tag.sh) enforces before anything is
published, so a mistagged release cannot ship binaries that misreport themselves.

Versions follow semver against what a user of the image and charts sees: patch for a
fix, minor for a feature or a new extractor, major for a break in the REST or CSV
shape, the chart values, the extractor config schema or the stored snapshot format.
Documentation, screenshots, tests and dev-only tooling ship nothing to that user and
need no release.

## Reporting security issues

Never open a public issue for those. See [SECURITY.md](SECURITY.md).

## License

By contributing you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE), the license of this project.
