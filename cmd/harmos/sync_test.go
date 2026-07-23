package main

import "testing"

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		512:                    "512 B",
		1024:                   "1.0 KiB",
		1536:                   "1.5 KiB",
		52 * 1024 * 1024:       "52.0 MiB",
		3 * 1024 * 1024 * 1024: "3.0 GiB",
	}
	for n, want := range cases {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
