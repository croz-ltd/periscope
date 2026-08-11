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

The README has the full layout and [DESIGN.md](DESIGN.md) has the architectural
reasoning. In short:

- `internal/extract/` one file per extractor, registered in `registry.go`
- `internal/drift/` matrix construction and drift classification
- `internal/store/` SQLite persistence with full snapshot history
- `internal/api/` REST, CSV/JSON export, Prometheus metrics
- `web/app/` PatternFly React UI, built into `web/dist`
- `web/app/mock/` synthetic fleet for `npm run dev:mock`, never part of a build
- `charts/` hub chart and per-cluster join chart

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
   components are excluded from the drift baseline, and a zero version would make an
   uninstalled component look like the most outdated one in the fleet.
3. Don't guess a version. If the value does not parse, return it as the raw string.
   `internal/version` marks unparseable values neutral instead of comparing them
   lexically.
4. Use the dynamic client for vendor CRs so no generated types are needed, and gate
   the call on `c.HasResource(gvr)`. Listing a resource whose CRD is absent comes
   back Forbidden, not 404, because a read-only account has no rule for it, and that
   would put a scrape error on every cluster not running that vendor.
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

## Reporting security issues

Please don't open a public issue for those. See [SECURITY.md](SECURITY.md).

## License

By contributing you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE), the license of this project.
