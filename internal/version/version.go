// Package version reports the binary's version. A release build stamps it via
// -ldflags (the Makefile and GoReleaser do this):
//
//	-ldflags "-X github.com/pottom/harmos/internal/version.Version=v1.2.3 …"
//
// Without that stamp — a plain `go build`, or `go install …@version` — it falls
// back to Go's embedded build info so the binary still reports something
// truthful (the module version, or the commit plus a -dirty marker) instead of
// a bare "dev".
package version

import "runtime/debug"

var (
	// Version is the release tag (or a build-info fallback for an unstamped build).
	Version = "dev"
	// GitCommit is the short commit the binary was built from.
	GitCommit = "unknown"
	// BuildDate is the build timestamp.
	BuildDate = "unknown"

	// LatestVersion is populated asynchronously by the background update check
	// (empty = not checked yet, or already up to date).
	LatestVersion string
)

func init() {
	// An -ldflags build already set Version and wins; only fill the defaults.
	if Version != "dev" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	var revision, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value
		case "vcs.time":
			if BuildDate == "unknown" {
				BuildDate = s.Value
			}
		}
	}
	if GitCommit == "unknown" && len(revision) >= 7 {
		GitCommit = revision[:7]
	}

	// `go install …@v1.2.3` (or @latest) records the module version — prefer it.
	if v := info.Main.Version; v != "" && v != "(devel)" {
		Version = v
		return
	}
	// A local `go build`: no tag in the build info, so keep the "dev" label but
	// pin it to the commit — and flag a modified tree — so dev builds differ.
	if revision != "" {
		Version = "dev-" + GitCommit
		if modified == "true" {
			Version += "-dirty"
		}
	}
}

// String is a human-readable version line for `harmos --version`.
func String() string {
	return Version + " (commit " + GitCommit + ", built " + BuildDate + ")"
}
