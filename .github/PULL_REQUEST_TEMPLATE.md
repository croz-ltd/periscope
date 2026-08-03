<!-- Keep the subject line in Conventional Commits form, e.g. feat(extract): ... -->

## What

<!-- What this changes, and why. Link the issue if there is one. -->

## How it was verified

- [ ] `make test` (or `go test ./...`) passes
- [ ] `go vet ./...` clean
- [ ] UI builds (`make web`) if `web/app` changed
- [ ] Verified against a real cluster (state which OpenShift version, if applicable)

## Notes for reviewers

<!-- New extractor? List the CRD group/version/resource and the version field path.
     Chart change? Note whether it affects RBAC or the join convention. -->
