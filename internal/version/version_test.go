package version

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	defer func(v, c, d string) { Version, GitCommit, BuildDate = v, c, d }(Version, GitCommit, BuildDate)
	Version, GitCommit, BuildDate = "v1.2.3", "abc1234", "2026-01-02T03:04:05Z"

	got := String()
	for _, want := range []string{"v1.2.3", "abc1234", "2026-01-02T03:04:05Z"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, missing %q", got, want)
		}
	}
}

// A build always reports some version — never the empty string, whether stamped
// by -ldflags, recovered from build info, or the "dev" default.
func TestVersionNotEmpty(t *testing.T) {
	if Version == "" {
		t.Error("Version must never be empty")
	}
}
