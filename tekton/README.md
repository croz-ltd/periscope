# Periscope — OpenShift Pipelines (Tekton) build

Builds the multi-stage `Dockerfile` (PatternFly UI + Go binary) with **buildah**
and pushes `docker.io/crozltd/periscope:<tag>`. linux/amd64 only. This is an
alternative to `.gitlab-ci.yml` for teams that build on-cluster.

## Prerequisites
- The **OpenShift Pipelines** operator installed (provides the `pipeline` SA +
  `pipelines-scc`, and — for triggers — the `gitlab` ClusterInterceptor).
- A CI namespace, e.g. `periscope-ci`.

## One-time setup
```bash
oc new-project periscope-ci

# Harbor push credentials (buildah reads this to push).
oc create secret docker-registry periscope-harbor-push \
  --docker-server=registry.example.com \
  --docker-username='<robot-or-user>' \
  --docker-password='<token>' \
  -n periscope-ci

# If the repo is private, give the `pipeline` SA git read access, e.g. a basic-auth
# token secret annotated for the git host, linked to the SA:
#   oc annotate secret <git-secret> 'tekton.dev/git-0=https://github.com'
#   oc secrets link pipeline <git-secret> -n periscope-ci

oc apply -f tekton/tasks.yaml -n periscope-ci
oc apply -f tekton/pipeline.yaml -n periscope-ci
```

## Run a build manually
```bash
# edit image-tag/revision in the file, or override with tkn:
oc create -f tekton/pipelinerun.yaml -n periscope-ci
# or:
tkn pipeline start periscope-build -n periscope-ci \
  -p revision=master -p image-tag=v0.1.0 \
  -w name=shared,volumeClaimTemplateFile=/dev/stdin <<'EOF'
spec: { accessModes: [ReadWriteOnce], resources: { requests: { storage: 3Gi } } }
EOF
  # (dockerconfig workspace -> secret periscope-harbor-push)
```

## Auto-build on git push (optional)
Needs the Triggers component. Create the webhook token secret, apply the triggers,
then point a GitLab **Push events** webhook at the `periscope-el` Route URL:
```bash
oc create secret generic periscope-gitlab-webhook \
  --from-literal=secretToken='<random-token>' -n periscope-ci
oc apply -f tekton/triggers.yaml -n periscope-ci
oc get route periscope-el -n periscope-ci -o jsonpath='{.spec.host}{"\n"}'
```
Push events build and push `:edge`.

## Notes
- `buildah` runs under the operator's `pipeline` SA / `pipelines-scc` (needs
  `SETFCAP`); it uses the `vfs` storage driver, so no privileged/fuse required.
- Base images are pulled through Harbor mirrors via `--build-arg` (see
  `pipeline.yaml`), matching the GitLab CI. Adjust `BUILDER_IMAGE` in
  `tasks.yaml` if `registry.redhat.io/rhel8/buildah` isn't available to you.
