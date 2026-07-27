// Package selfupdate replaces the running harmos binary with the latest GitHub
// release. It downloads the release archive for this OS and architecture, checks
// it against the published sha256sums, extracts the binary, and swaps it in
// atomically. It reads only public release assets and sends nothing about the
// machine — the same restraint as internal/updater, whose LatestTag/IsNewer it
// reuses.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"

	"github.com/pottom/harmos/internal/updater"
)

const (
	osWindows = "windows"

	repo         = "pottom/harmos"
	downloadBase = "https://github.com/" + repo + "/releases/download"
	// The binary is ~12 MB, so this is generous for a slow link but still bounded —
	// a self-update that hangs forever is worse than one that fails and can retry.
	timeout = 2 * time.Minute
)

var (
	// ErrUpToDate means the running build is already the latest release.
	ErrUpToDate = errors.New("already the latest release")
	// ErrUnsupported means there is no release asset this runtime can name and
	// fetch for its platform — reinstalling covers those cases.
	ErrUnsupported = errors.New("no self-update build for this platform")
	// ErrManagedInstall means the running binary sits where the current user
	// cannot replace it — typically a .deb/.rpm install under a root-owned
	// directory, which should be updated through the package manager instead.
	ErrManagedInstall = errors.New("harmos is installed in a location you cannot write to")
)

// Update fetches the latest release and, when it is newer than current, replaces
// the running binary with it, returning the new tag. ErrUpToDate means there was
// nothing to do. The download and swap are all-or-nothing: a failed apply rolls
// back.
func Update(current string) (string, error) {
	tag, err := updater.LatestTag()
	if err != nil {
		return "", err
	}
	if !updater.IsNewer(current, tag) {
		return "", ErrUpToDate
	}
	// Fail fast before downloading ~12 MB if we could never swap the binary in.
	if err := checkWritable(); err != nil {
		return "", err
	}
	// Archive names carry the version without its leading v (GoReleaser default).
	version := strings.TrimPrefix(tag, "v")

	asset, err := assetName(version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	archive, err := download(downloadBase + "/" + tag + "/" + asset)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", asset, err)
	}
	sums, err := download(downloadBase + "/" + tag + "/sha256sums.txt")
	if err != nil {
		return "", fmt.Errorf("downloading checksums: %w", err)
	}
	if err := verifyChecksum(archive, string(sums), asset); err != nil {
		return "", err
	}
	bin, err := extractBinary(archive, runtime.GOOS == osWindows, binaryName(runtime.GOOS))
	if err != nil {
		return "", err
	}
	if err := selfupdate.Apply(bytes.NewReader(bin), selfupdate.Options{}); err != nil {
		if rollbackErr := selfupdate.RollbackError(err); rollbackErr != nil {
			return "", fmt.Errorf("update failed, and rolling back failed too: %w (original: %w)", rollbackErr, err)
		}
		return "", fmt.Errorf("applying update (rolled back): %w", err)
	}
	return tag, nil
}

// checkWritable reports whether the running binary can be replaced in place. The
// atomic swap writes the new binary into the current one's directory and renames
// it over the old file, so that directory must be writable by this user. When it
// is not — a package-managed install under a root-owned path — it returns
// ErrManagedInstall, naming the path, so the caller can point at the package
// manager instead of failing mid-download.
func checkWritable() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	f, err := os.CreateTemp(dir, ".harmos-update-*")
	if err != nil {
		return fmt.Errorf("%w: %s", ErrManagedInstall, exe)
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return nil
}

// binaryName is the executable's name inside the archive for the given OS.
func binaryName(goos string) string {
	if goos == osWindows {
		return "harmos.exe"
	}
	return "harmos"
}

// assetName is the release archive for this platform, matching GoReleaser's
// name_template harmos_<version>_<os>_<arch>.<ext>.
func assetName(version, goos, goarch string) (string, error) {
	switch goarch {
	case "amd64", "arm64", "386":
	default:
		return "", fmt.Errorf("%w: %s/%s", ErrUnsupported, goos, goarch)
	}
	ext := "tar.gz"
	if goos == osWindows {
		ext = "zip"
	}
	return fmt.Sprintf("harmos_%s_%s_%s.%s", version, goos, goarch, ext), nil
}

func download(url string) ([]byte, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// verifyChecksum checks the archive against its line in a GoReleaser
// sha256sums.txt, whose lines are "<hex sha256>  <filename>".
func verifyChecksum(archive []byte, sums, asset string) error {
	sum := sha256.Sum256(archive)
	got := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			if fields[0] == got {
				return nil
			}
			return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", asset, fields[0], got)
		}
	}
	return fmt.Errorf("%s is not listed in the checksums", asset)
}

// extractBinary pulls the named file out of a .tar.gz (or .zip on Windows).
func extractBinary(archive []byte, isZip bool, name string) ([]byte, error) {
	if isZip {
		return extractZip(archive, name)
	}
	return extractTarGz(archive, name)
}

func extractTarGz(archive []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if path.Base(hdr.Name) == name {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%s not found in the archive", name)
}

func extractZip(archive []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if path.Base(f.Name) == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer func() { _ = rc.Close() }()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("%s not found in the archive", name)
}
