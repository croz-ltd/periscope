#!/bin/sh
# Fails unless the release tag matches the release carried in source
# (pkg/version.Base). A release binary reports Base with no build metadata, so a
# tag that disagrees with it would publish v0.3.0 binaries calling themselves
# 0.2.0. Run as: .github/check-release-tag.sh "$GITHUB_REF_NAME"
set -eu

tag="${1:?usage: check-release-tag.sh <tag>}"
base=$(sed -n 's/^const Base = "\(.*\)"$/\1/p' pkg/version/version.go)

if [ -z "${base}" ]; then
    echo "cannot read Base from pkg/version/version.go" >&2
    exit 1
fi

# Tags are cut as either "v0.2.0" or "0.2.0".
if [ "${tag#v}" != "${base}" ]; then
    echo "tag ${tag} does not match pkg/version.Base ${base}." >&2
    echo "Bump Base (and the chart versions and web/app/package.json) or retag." >&2
    exit 1
fi

echo "tag ${tag} matches pkg/version.Base ${base}"
