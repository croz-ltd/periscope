// Package version provides tolerant semver parsing and comparison. Cluster
// components emit versions in many shapes ("4.14.9", "v2.13.1", "1.2.3-rc1+build"),
// so parsing records failure in OK rather than erroring, and callers render
// unparseable versions as neutral "unknown" cells instead of guessing drift.
package version

import (
	"strconv"
	"strings"
)

// Version is a tolerantly-parsed semantic version.
type Version struct {
	Major, Minor, Patch int
	Pre                 string // prerelease identifiers, without the leading '-'
	Raw                 string // original input, shown verbatim in the UI
	OK                  bool   // false when Raw did not parse
}

// Parse normalizes and parses a version string: trims space, strips a leading
// 'v'/'V', drops build metadata after '+', tolerates missing minor/patch, and
// splits off a prerelease after '-'. On any failure OK is false.
func Parse(raw string) Version {
	v := Version{Raw: raw}
	s := strings.TrimSpace(raw)
	if s == "" {
		return v
	}
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if i := strings.IndexByte(s, '+'); i >= 0 { // drop build metadata
		s = s[:i]
	}
	core := s
	if i := strings.IndexByte(s, '-'); i >= 0 { // split prerelease
		core = s[:i]
		v.Pre = s[i+1:]
	}
	parts := strings.Split(core, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return v
	}
	nums := [3]int{}
	for i := range parts {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return v
		}
		nums[i] = n
	}
	v.Major, v.Minor, v.Patch = nums[0], nums[1], nums[2]
	v.OK = true
	return v
}

// Compare returns -1, 0, or 1 as a is less than, equal to, or greater than b.
// Only meaningful when both are OK. A version with a prerelease sorts below the
// same core release without one (2.0.0-rc1 < 2.0.0).
func Compare(a, b Version) int {
	if c := cmpInt(a.Major, b.Major); c != 0 {
		return c
	}
	if c := cmpInt(a.Minor, b.Minor); c != 0 {
		return c
	}
	if c := cmpInt(a.Patch, b.Patch); c != 0 {
		return c
	}
	return cmpPre(a.Pre, b.Pre)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// cmpPre compares prerelease strings: absent (release) outranks present
// (prerelease); otherwise compare dot-separated identifiers, numerically when
// both are numeric, else lexically.
func cmpPre(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aerr := strconv.Atoi(as[i])
		bn, berr := strconv.Atoi(bs[i])
		if aerr == nil && berr == nil {
			if c := cmpInt(an, bn); c != 0 {
				return c
			}
			continue
		}
		if as[i] != bs[i] {
			if as[i] < bs[i] {
				return -1
			}
			return 1
		}
	}
	return cmpInt(len(as), len(bs))
}
