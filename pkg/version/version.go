// Package version carries the version this binary reports.
//
// The release lives here in source, so every build says which release it came
// from. Builds that are not a release tag add build metadata saying where they
// came from, giving strings like:
//
//	1.0.0             a release build of the v1.0.0 tag
//	1.0.0+ci-1a2b3c4  a CI build of master, at that commit
//	1.0.0+dev         a local `make build`
package version

// Base is the release this source tree carries. Bump it by hand when cutting a
// release, together with the chart versions and web/app/package.json. The
// release tag must match it, which CI checks before publishing.
const Base = "1.4.0"

// Build is the build metadata, stamped at link time with
//
//	-ldflags "-X github.com/croz-ltd/periscope/pkg/version.Build=ci-1a2b3c4"
//
// It stays "dev" for an unstamped local build. A release build clears it, so
// the version reads exactly Base.
var Build = "dev"

// Raw is the full version string: Base, plus build metadata when present.
var Raw = compose()

func compose() string {
	if Build == "" {
		return Base
	}
	return Base + "+" + Build
}
