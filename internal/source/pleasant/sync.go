package pleasant

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"

	"github.com/pottom/harmos/internal/secret"
)

// SyncOptions configures a Sync.
type SyncOptions struct {
	Comment   string        // recorded server-side (every fetch is audited)
	CachePath string        // destination kdbx cache
	Master    secret.Secret // encrypts the cache
	Now       time.Time     // provenance + expiry check; defaults to time.Now()
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

	// Fetch to a temp zip in the cache dir (same filesystem, for atomic rename).
	zipPath, err := fetchPackage(ctx, c, dir, opt.Comment)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(zipPath) }()

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("open package: %w", err)
	}
	defer func() { _ = zr.Close() }()

	res, err := Map(&zr.Reader, Meta{SourceURL: sourceURL, FetchedAt: opt.Now})
	if err != nil {
		return nil, err
	}
	if PackageExpired(res.Expiry, opt.Now) {
		return nil, fmt.Errorf("package is expired (Expiry %s); refusing to write cache", res.Expiry)
	}

	if err := writeAtomic(res.DB, opt.CachePath, opt.Master); err != nil {
		return nil, err
	}
	return res, nil
}

func fetchPackage(ctx context.Context, c *Client, dir, comment string) (string, error) {
	tmp, err := os.CreateTemp(dir, ".harmos-pkg-*.zip")
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(path)
		return "", err
	}
	_, err = c.OfflinePackage(ctx, comment, tmp)
	if cerr := tmp.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func writeAtomic(db *gokeepasslib.Database, cachePath string, master secret.Secret) error {
	dir := filepath.Dir(cachePath)
	tmp, err := os.CreateTemp(dir, ".harmos-cache-*.kdbx")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close() // Write reopens with O_TRUNC

	if err := Write(db, tmpPath, master); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
