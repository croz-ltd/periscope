// Package version carries the build version, injected at release time via
// -ldflags "-X github.com/croz-ltd/cluster-comparator/pkg/version.Raw=<tag>+<sha>".
package version

// Raw is the build version. It stays "dev" for local/untagged builds.
var Raw = "dev"
