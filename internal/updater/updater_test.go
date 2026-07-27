package updater

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		cur, tag string
		want     bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.9.0", "v1.10.0", true}, // numeric, not string
		{"v1.2.3", "v1.2.3", false},
		{"v2.0.0", "v1.9.9", false},
		{"dev", "v0.1.0", true},  // a dev build is below everything
		{"v0.1.0", "dev", false}, // an unparseable tag is never "newer"
		{"v0.1.0", "", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.cur, c.tag); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.cur, c.tag, got, c.want)
		}
	}
}
