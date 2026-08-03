# Periscope: on-cluster build with OpenShift Pipelines (Tekton)

An **example** pipeline for teams who build container images on the cluster rather
than in a hosted CI runner. It clones this repository, builds the multi-stage
[`Dockerfile`](../Dockerfile) (PatternFly UI + Go binary) with **buildah**, and pushes
the result to a registry you control, typically an internal registry your clusters
already pull from. linux/amd64 only.

The hosted alternative is the GitHub Actions workflow in
[`.github/workflows/release.yml`](../.github/workflows/release.yml), which publishes
`crozltd/periscope` to Docker Hub. Both build the same image from the same
`Dockerfile`, so use whichever fits your environment.

Everything is parameterized, so nothing here is specific to any one environment:

| Param | Default | |
|---|---|---|
| `git-url` | `https://github.com/croz-ltd/periscope.git` | source to clone (a fork or internal mirror works) |
| `revision` | `master` | branch, tag, or SHA |
| `image` | `docker.io/crozltd/periscope` | **change this** to your registry/repository |
| `image-tag` | `latest` | also stamped into the binary as the version |
| `node-image` / `go-image` / `runtime-image` | public upstream images | point at internal mirrors if egress is restricted |

## Prerequisites

- The **OpenShift Pipelines** operator installed (provides the `pipeline` SA +
  `pipelines-scc`, and for triggers the `github` ClusterInterceptor).
- A CI namespace, e.g. `periscope-ci`.

## One-time setup

```bash
oc new-project periscope-ci

# Registry push credentials (buildah reads this to push).
oc create secret docker-registry periscope-registry-push \
  --docker-server=registry.example.com \
  --docker-username='<robot-or-user>' \
  --docker-password='<token>' \
  -n periscope-ci

# If you clone from a private fork, give the `pipeline` SA git read access, e.g. a
# basic-auth token secret annotated for the git host, linked to the SA:
#   oc annotate secret <git-secret> 'tekton.dev/git-0=https://github.com'
#   oc secrets link pipeline <git-secret> -n periscope-ci

oc apply -f tekton/tasks.yaml -n periscope-ci
oc apply -f tekton/pipeline.yaml -n periscope-ci
```

## Run a build manually

```bash
# edit image/image-tag/revision in the file, or override with tkn:
oc create -f tekton/pipelinerun.yaml -n periscope-ci
# or:
tkn pipeline start periscope-build -n periscope-ci \
  -p revision=master \
  -p image=registry.example.com/platform/periscope \
  -p image-tag=0.1.0 \
  -w name=shared,volumeClaimTemplateFile=/dev/stdin <<'EOF'
spec: { accessModes: [ReadWriteOnce], resources: { requests: { storage: 3Gi } } }
EOF
  # (dockerconfig workspace -> secret periscope-registry-push)
```

## Auto-build on git push (optional)

Needs the Triggers component. Create the webhook secret, apply the triggers, then
point a GitHub **push** webhook at the `periscope-el` Route URL:

```bash
oc create secret generic periscope-webhook \
  --from-literal=secretToken='<random-token>' -n periscope-ci
oc apply -f tekton/triggers.yaml -n periscope-ci
oc get route periscope-el -n periscope-ci -o jsonpath='{.spec.host}{"\n"}'
```

Pushes build and push `:edge`. For GitLab instead of GitHub, swap the interceptor
`ref` to `gitlab`, `eventTypes` to `["Push Hook"]`, and bind `revision` to
`$(body.checkout_sha)`. This is noted inline in `triggers.yaml`.

## Notes

- `buildah` runs under the operator's `pipeline` SA / `pipelines-scc` (needs
  `SETFCAP`); it uses the `vfs` storage driver, so no privileged pod or fuse device is
  required.
- Base images come from `--build-arg` params, so an air-gapped cluster can point them
  at internal mirrors without touching the `Dockerfile`.
- Adjust `BUILDER_IMAGE` in `tasks.yaml` if `registry.redhat.io/rhel8/buildah` isn't
  available to you.
