# Contributing to Periscope

Thanks for your interest. Issues and pull requests are welcome.

## Development setup

You need **Go 1.26+**, **Node 20+**, and (optionally) Docker or Podman.

```bash
git clone https://github.com/croz-ltd/periscope.git
cd periscope

make web      # build the PatternFly UI into web/dist (the Go build embeds it)
make build    # build bin/periscope
make test     # go test ./...
make vet      # go vet ./...
```

`make web` must run at least once before `make build` — `web/embed.go` embeds
`web/dist` with `go:embed`, so a stale or missing `web/dist` means a stale or
failing build.

Run locally against your current kubeconfig context:

```bash
bin/periscope serve --namespace periscope --db ./periscope.db
```

Periscope reads joined clusters from labeled Secrets in `--namespace`; with none
present it starts with an empty matrix. See the README for the join flow.

For UI-only work, `cd web/app && npm run dev` gives you Vite with hot reload;
point it at a running backend (the dev server proxies `/api`, see
`web/app/vite.config.ts`).

## Project layout

See the layout section in [README.md](README.md) and the architecture rationale in
[DESIGN.md](DESIGN.md). In short:

- `internal/extract/` — one file per extractor; register it in `registry.go`
- `internal/drift/` — matrix construction and drift classification
- `internal/store/` — SQLite persistence (full snapshot history)
- `internal/api/` — REST, CSV/JSON export, Prometheus metrics
- `web/app/` — PatternFly React UI (built into `web/dist`)
- `charts/` — hub chart and per-cluster join chart

## Adding an extractor

Most contributions are new extractors. An extractor implements:

```go
type Extractor interface {
    Key() string
    Extract(ctx context.Context, c *Clients) ([]model.Component, error)
}
```

Guidelines:

1. **Use a stable component key.** The key is the matrix row identity and must not
   change between versions of the extracted component.
2. **Absent is not behind.** Return no component when the thing is not installed —
   never a zero version. Absent components are excluded from the drift baseline.
3. **Never guess a version.** If the value does not parse as a version, return it as
   the raw string; `internal/version` marks unparseable values neutral rather than
   comparing them lexically.
4. **Use the dynamic client for vendor CRs** so no generated types are needed.
5. **Prefer a config-driven CR field extractor** when the version is just a nested
   field: `extract.NewCRFieldExtractor(...)` covers that case, and users can
   override the field path via `config/extractors.yaml` without rebuilding.
6. **Add unit tests.** `internal/version` and `internal/drift` have tests to follow;
   extractors can be tested against fake dynamic clients.

Register the extractor in `extract.Default()` and document its key — the UI's Docs
page lists component keys from the API, so a new key shows up automatically.

## Pull requests

- Keep commits focused; use [Conventional Commits](https://www.conventionalcommits.org/)
  subjects (`feat(extract): ...`, `fix(ui): ...`, `docs: ...`, `chore(chart): ...`).
- Explain *why* in the commit body when the diff does not make it obvious.
- `go vet ./...` and `go test ./...` must pass; CI runs both plus a UI typecheck,
  cross-platform builds, and a container build.
- Note in the PR description whether the change was verified against a real cluster
  and which OpenShift version.

## Reporting security issues

Do not open a public issue. See [SECURITY.md](SECURITY.md).

## License

By contributing, you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE), the license of this project.
