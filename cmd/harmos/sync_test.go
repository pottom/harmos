package main

import (
	"bytes"
	"strings"
	"testing"
)

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

func TestSyncReporterPercent(t *testing.T) {
	var buf bytes.Buffer
	r := &syncReporter{w: &buf}

	r.bytes(50, 100)
	if !strings.Contains(buf.String(), "50%") {
		t.Errorf("expected a percentage when total is known, got %q", buf.String())
	}

	buf.Reset()
	r.bytes(50, -1) // server gave no length
	if strings.Contains(buf.String(), "%") {
		t.Errorf("no percentage when total is unknown, got %q", buf.String())
	}
}
