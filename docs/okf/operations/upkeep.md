---
type: Playbook
title: Upkeep
description: Upgrade the hub, choose an image tag, rotate a cluster token, read version strings, and cut a release.
tags: [upgrade, release, versions, operations]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: readme
    resource: https://github.com/croz-ltd/periscope/blob/master/README.md#version-strings
    title: "README: Version strings"
  - id: contributing
    resource: https://github.com/croz-ltd/periscope/blob/master/CONTRIBUTING.md#cutting-a-release
    title: "CONTRIBUTING.md: Cutting a release"
---

# Upgrade the hub

```bash
git pull
helm upgrade periscope charts/periscope --set version=<tag>
```

Apply the chart, not only the new image. Some features need chart changes
too. For example, the admin API needs proxy flags that older charts do not set.

The database lives on the PVC and survives the upgrade. When a release changes
how changes are detected, the first start rebuilds the last 90 days of the
Changes feed in the background. The pod serves during the rebuild.

# Image tags

Images are on Docker Hub as `crozltd/periscope`, for linux/amd64:

| Tag | Content |
|---|---|
| `:<tag>`, for example `:1.9.0` | A release |
| `:latest` | The newest release |
| `:edge` | The tip of `master` |
| `:<sha>` | One commit of `master` |

Pin a release tag in production.

# Rotate a cluster token

Import the cluster again from the **Add a cluster** wizard. The import is
idempotent and replaces the stored token. To do it by hand, mint a new token on
the joined cluster and replace the `token` key of its Secret on the hub.

# Version strings

The About page, `periscope version` and `GET /api/version` report the build:[^readme]

```
1.9.0             a release build of the v1.9.0 tag
1.9.0+ci-1a2b3c4  the :edge image, or a CI binary, at that commit
1.9.0+tekton-...  built on a cluster by tekton/
1.9.0+dev         a local make build
```

# Cut a release

For maintainers. Four places move together, in one commit:[^contributing]

* `Base` in `pkg/version/version.go`
* `version` and `appVersion` in `charts/periscope/Chart.yaml`
* `version` and `appVersion` in `charts/periscope-join/Chart.yaml`
* `version` in `web/app/package.json` and its lock file

Then push a tag that matches `Base`. CI refuses a tag that does not match.

Semver rules: patch for a fix, minor for a feature or a new extractor, major for
a break in the REST or CSV shape, the chart values, the extractor config schema
or the stored snapshot format. Documentation and tests need no release.

# Dependencies

Dependabot opens pull requests for Go modules, npm packages, base images and
GitHub Actions. The build uses `golang:1.27`, `node:26-alpine` and
`ubi10/ubi-micro` as the runtime image.

[^readme]: README: Version strings
[^contributing]: CONTRIBUTING.md: Cutting a release
