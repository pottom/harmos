package pleasant

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pottom/harmos/internal/secret"
)

// Reporter receives live progress during a Sync. Any field may be nil.
type Reporter struct {
	Phase func(name string)       // a new phase begins (downloading, building, writing)
	Bytes func(done, total int64) // download progress; total is -1 when unknown
}

// SyncOptions configures a Sync.
type SyncOptions struct {
	Comment   string        // recorded server-side (every fetch is audited)
	CachePath string        // destination kdbx cache
	Master    secret.Secret // encrypts the cache (with Keyfile, if set)
	Keyfile   string        // cache keyfile; "" writes a password-only cache
	Now       time.Time     // provenance + expiry check; defaults to time.Now()
	Report    *Reporter     // optional live progress; nil is silent
}

// Sync fetches the OfflinePackage with an already-logged-in client, enforces the
// package Expiry (spec §9), maps it, and writes the cache atomically (temp file
// then rename) so a failed sync never leaves a half-written cache. It is an
// explicit, user-initiated action — never a timer.
func Sync(ctx context.Context, c *Client, sourceURL string, opt SyncOptions) (*Result, error) {
	if opt.Now.IsZero() {
		opt.Now = time.Now()
	}
	if opt.CachePath == "" {
		return nil, fmt.Errorf("no cache path")
	}
	report := opt.Report
	if report == nil {
		report = &Reporter{}
	}
	phase := func(name string) {
		if report.Phase != nil {
			report.Phase(name)
		}
	}

	phase("checking server")
	available, err := c.IsOfflineAvailable(ctx)
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, fmt.Errorf("offline packages are disabled on the server (IsOfflineAvailable=false)")
	}

	dir := filepath.Dir(opt.CachePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	// The package is every password on the server, in the clear. It used to be
	// fetched to a temp file beside the cache — 0600, removed on the way out,
	// but on disk for the whole of the mapping and the write, which on a real
	// vault is over a minute, and left behind entirely if the process is killed.
	// A backup or a snapshot taken in that window keeps it forever.
	//
	// So it never touches a filesystem. The cost is holding it in memory, which
	// for the vault this was measured against is tens of megabytes for the
	// length of one explicit, user-initiated sync.
	phase("downloading offline package")
	pkg, err := fetchPackage(ctx, c, opt.Comment, report.Bytes)
	if err != nil {
		return nil, err
	}
	defer pkg.Wipe()

	raw := pkg.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("open package: %w", err)
	}

	phase("building cache")
	res, err := Map(zr, Meta{SourceURL: sourceURL, FetchedAt: opt.Now})
	if err != nil {
		return nil, err
	}
	if PackageExpired(res.Expiry, opt.Now) {
		return nil, fmt.Errorf("package is expired (Expiry %s); refusing to write cache", res.Expiry)
	}

	phase("writing cache")
	if err := Write(res.DB, opt.CachePath, opt.Master, opt.Keyfile); err != nil {
		return nil, err
	}
	return res, nil
}

// fetchPackage downloads the offline package into memory.
//
// It comes back as a Secret because that is what it is: every credential on the
// server, in the clear. The wrapper buys the redaction on any accidental
// formatting, and Wipe, which zeroes this exact buffer — best-effort, like
// everywhere else here, since Go may have moved it while it grew.
func fetchPackage(ctx context.Context, c *Client, comment string, onBytes func(done, total int64)) (secret.Secret, error) {
	var buf bytes.Buffer
	if _, err := c.OfflinePackage(ctx, comment, &buf, onBytes); err != nil {
		return secret.Secret{}, err
	}
	return secret.FromBytes(buf.Bytes()), nil
}
