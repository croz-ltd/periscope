package version

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in                  string
		ok                  bool
		maj, min, pat       int
		pre                 string
	}{
		{"4.14.9", true, 4, 14, 9, ""},
		{"v2.13.1", true, 2, 13, 1, ""},
		{"V1.2.3", true, 1, 2, 3, ""},
		{"1.2.3-rc1+build.5", true, 1, 2, 3, "rc1"},
		{"1.2", true, 1, 2, 0, ""},
		{"3", true, 3, 0, 0, ""},
		{"  4.10.0 ", true, 4, 10, 0, ""},
		{"", false, 0, 0, 0, ""},
		{"latest", false, 0, 0, 0, ""},
		{"1.2.3.4", false, 0, 0, 0, ""},
		{"a.b.c", false, 0, 0, 0, ""},
	}
	for _, c := range cases {
		v := Parse(c.in)
		if v.OK != c.ok {
			t.Errorf("Parse(%q).OK = %v, want %v", c.in, v.OK, c.ok)
			continue
		}
		if !c.ok {
			continue
		}
		if v.Major != c.maj || v.Minor != c.min || v.Patch != c.pat || v.Pre != c.pre {
			t.Errorf("Parse(%q) = %d.%d.%d-%q, want %d.%d.%d-%q",
				c.in, v.Major, v.Minor, v.Patch, v.Pre, c.maj, c.min, c.pat, c.pre)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.4", "1.2.3", 1},
		{"1.3.0", "1.2.9", 1},
		{"2.0.0", "1.9.9", 1},
		{"1.2.3", "1.2.4", -1},
		{"2.0.0-rc1", "2.0.0", -1},
		{"2.0.0", "2.0.0-rc1", 1},
		{"2.0.0-rc2", "2.0.0-rc1", 1},
		{"v4.14.9", "4.14.10", -1},
	}
	for _, c := range cases {
		got := Compare(Parse(c.a), Parse(c.b))
		if got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
