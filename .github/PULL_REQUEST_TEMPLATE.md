<!-- Keep the subject line in Conventional Commits form, for example feat(extract): ... -->

## What

<!-- What this changes, and why. Link the issue if there is one. -->

## How it was verified

- [ ] `make test` (or `go test ./...`) passes
- [ ] `go vet ./...` clean
- [ ] UI builds (`make web`) if `web/app` changed
- [ ] Verified against a real cluster (state which OpenShift version, if applicable)

## Notes for reviewers

<!-- For a new extractor, list the CRD group, version and resource, and the field
     path that holds the version. For a chart change, say whether it affects RBAC
     or the join convention. -->
