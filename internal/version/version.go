// Package version holds build-time variables injected via -ldflags, e.g.
//
//	-ldflags "-X github.com/pottom/harmos/internal/version.Version=v1.2.3 …"
//
// A plain `go build` leaves the defaults, so a dev binary reads "dev".
package version

var (
	// Version is the release tag (or "dev" for a local build).
	Version = "dev"
	// GitCommit is the short commit the binary was built from.
	GitCommit = "unknown"
	// BuildDate is the build timestamp.
	BuildDate = "unknown"

	// LatestVersion is populated asynchronously by the background update check
	// (empty = not checked yet, or already up to date).
	LatestVersion string
)

// String is a human-readable version line for `harmos --version`.
func String() string {
	return Version + " (commit " + GitCommit + ", built " + BuildDate + ")"
}
