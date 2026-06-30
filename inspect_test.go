package racc

import "testing"

func TestRubyInspect(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, "nil"},
		{true, "true"},
		{false, "false"},
		{"+", `"+"`},
		{`a"b\c`, `"a\"b\\c"`},
		{"tab\tnl\n", `"tab\tnl\n"`},
		{Symbol("NUMBER"), ":NUMBER"},
		{42, "42"},
		{int64(7), "7"},
		{1.0, "1.0"},
		{1.5, "1.5"},
		{1e10, "1e+10"},        // Go's shortest form; documented divergence from Ruby
		{[]int{1, 2}, "[1 2]"}, // default %v fallback
	}
	for _, c := range cases {
		if got := rubyInspect(c.in); got != c.want {
			t.Errorf("rubyInspect(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}
