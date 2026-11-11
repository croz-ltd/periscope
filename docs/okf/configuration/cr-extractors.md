---
type: Configuration File
title: CR extractors
description: A YAML file, passed with --config, that reads a version from a nested field of any custom resource with no code change.
resource: https://github.com/croz-ltd/periscope/blob/master/config/extractors.yaml
tags: [extractors, crd, csi, portworx, dell, configuration]
status: stable
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T04:05:30Z }
sources:
  - id: config
    resource: https://github.com/croz-ltd/periscope/blob/master/config/extractors.yaml
    title: config/extractors.yaml
  - id: code
    resource: https://github.com/croz-ltd/periscope/blob/master/internal/config/config.go
    title: Extractor config code
---

# When to use it

* A vendor CR on your clusters keeps the version in a different field than the
  built-in Portworx and Dell defaults.
* You want a new row for a component whose version is a field in a custom
  resource.

Anything a field path cannot express needs a new extractor in Go. See
`CONTRIBUTING.md`, "Adding an extractor".

# Schema

The file has one list, `crExtractors`. Each entry:[^code]

| Field | Required | Meaning |
|---|---|---|
| `key` | Yes | The component key, the stable identity of the row |
| `display` | Yes | The row name in the UI |
| `kind` | Yes | `csi` or `operator`, for the label in the UI |
| `group` | Yes | API group of the CR, for example `storage.dell.com` |
| `version` | Yes | API version of the CR, for example `v1` |
| `resource` | Yes | Plural resource name, for example `containerstoragemodules` |
| `versionPath` | Yes | The nested field path, as a list, for example `[spec, driver, configVersion]` |
| `imageTag` | No | `true` to take the tag after the last `:` of the field value, for image references |

Rules:

* With `--config`, the entries in the file replace the built-in Portworx and
  Dell entries completely. Copy them into your file to keep them.
* Every other extractor stays on.
* A cluster that does not serve the resource gets "not installed" for the row,
  with no error.

# Examples

The shipped file, with the built-in defaults:[^config]

```yaml
crExtractors:
  - key: portworx-csi
    display: Portworx (CSI)
    kind: csi
    group: core.libopenstorage.org
    version: v1
    resource: storageclusters
    versionPath: [status, version]
    # if status.version is empty on your install:
    #   versionPath: [spec, image]
    #   imageTag: true

  - key: dell-csi
    display: Dell CSM (CSI)
    kind: csi
    group: storage.dell.com
    version: v1
    resource: containerstoragemodules
    versionPath: [spec, driver, configVersion]
```

Find the right path on a real cluster first:

```bash
oc get storagecluster -A -o yaml
oc get containerstoragemodule -A -o yaml
```

Give the file to the pod and pass `--config` (or set `CONFIG_PATH`). The hub
chart does not mount a config file, so add a ConfigMap volume and the flag to
the Deployment.

[^config]: config/extractors.yaml
[^code]: Extractor config code
