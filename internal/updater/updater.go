// Package updater checks GitHub for a newer harmos release. It only ever reads —
// it asks the public releases API for the latest tag and compares it to this
// build. It sends nothing about the user or the machine, and the check is
// skippable with HARMOS_NO_UPDATE_CHECK, so a locked-down install can stay
// offline.
package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrNoReleases means the repository has no published releases yet.
var ErrNoReleases = errors.New("no releases published yet")

const (
	repo   = "pottom/harmos"
	apiURL = "https://api.github.com/repos/" + repo + "/releases/latest"
	// A short timeout: a background check that hangs the interface waiting on a
	// slow network would be worse than never checking.
	timeout = 4 * time.Second
)

// Disabled reports whether the update check is switched off via the environment.
func Disabled() bool { return os.Getenv("HARMOS_NO_UPDATE_CHECK") != "" }

// release is the one field of the GitHub releases API we read.
type release struct {
	TagName string `json:"tag_name"`
}

// LatestTag returns the tag of the newest published release, or an error the
// caller is expected to swallow — a failed check is not worth a word to the user.
func LatestTag() (string, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching release info: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", ErrNoReleases
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", fmt.Errorf("parsing release info: %w", err)
	}
	return rel.TagName, nil
}

// IsNewer reports whether tag names a version above current. It compares the
// numeric components in order, so v1.10.0 beats v1.9.0 — a plain string compare
// gets that backwards. A build with no real version ("dev") is below everything,
// so a dev binary is never told it is up to date.
func IsNewer(current, tag string) bool {
	cur := versionParts(current)
	next := versionParts(tag)
	if next == nil {
		return false
	}
	if cur == nil {
		return true
	}
	for i := 0; i < len(cur) || i < len(next); i++ {
		var a, b int
		if i < len(cur) {
			a = cur[i]
		}
		if i < len(next) {
			b = next[i]
		}
		if a != b {
			return b > a
		}
	}
	return false
}

// versionParts turns "v1.2.3" into [1,2,3], stopping at the first component that
// is not a plain number, so the comparison is over the release version alone. It
// returns nil for anything with no numeric lead, which is how "dev" reads as "no
// version".
func versionParts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "+")
	v, _, _ = strings.Cut(v, "-")
	if v == "" {
		return nil
	}
	var parts []int
	for _, seg := range strings.Split(v, ".") {
		n, err := strconv.Atoi(seg)
		if err != nil {
			break
		}
		parts = append(parts, n)
	}
	return parts
}
