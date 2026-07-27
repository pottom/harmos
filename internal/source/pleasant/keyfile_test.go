package pleasant

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/secret"
)

// TestCacheKeyfileCompositeKey is the crux of spec §15: a cache written with a
// keyfile needs both the master password and the keyfile to open. The master
// alone — the only secret a thief who copied the .kdbx is likely to have — must
// not be enough.
func TestCacheKeyfileCompositeKey(t *testing.T) {
	const master = "composite-master"
	res := mapFixture(t, Meta{SourceURL: "https://pps.example:10001", FetchedAt: time.Now()})

	dir := t.TempDir()
	keyfile := filepath.Join(dir, "work.key")
	if err := config.EnsureKeyfile(keyfile); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(dir, "cache.kdbx")
	if err := Write(res.DB, cache, secret.New(master), keyfile); err != nil {
		t.Fatal(err)
	}

	// The master alone must fail.
	if err := decodeCache(cache, gokeepasslib.NewPasswordCredentials(master)); err == nil {
		t.Fatal("cache opened with the master alone; the keyfile was not required")
	}

	// The master plus the keyfile must succeed.
	data, err := os.ReadFile(keyfile)
	if err != nil {
		t.Fatal(err)
	}
	creds, err := gokeepasslib.NewPasswordAndKeyDataCredentials(master, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := decodeCache(cache, creds); err != nil {
		t.Fatalf("cache did not open with master+keyfile: %v", err)
	}
}

// TestCacheNoKeyfileStillPasswordOnly keeps the fallback honest: an empty keyfile
// writes a password-only cache (what pre-§15 caches were), so a legacy cache
// still opens with the master until the next sync re-encrypts it.
func TestCacheNoKeyfileStillPasswordOnly(t *testing.T) {
	const master = "legacy-master"
	res := mapFixture(t, Meta{SourceURL: "https://pps.example:10001", FetchedAt: time.Now()})
	cache := filepath.Join(t.TempDir(), "cache.kdbx")
	if err := Write(res.DB, cache, secret.New(master), ""); err != nil {
		t.Fatal(err)
	}
	if err := decodeCache(cache, gokeepasslib.NewPasswordCredentials(master)); err != nil {
		t.Fatalf("password-only cache did not open with the master: %v", err)
	}
}

func decodeCache(path string, creds *gokeepasslib.DBCredentials) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	db := gokeepasslib.NewDatabase()
	db.Credentials = creds
	if err := gokeepasslib.NewDecoder(f).Decode(db); err != nil {
		return err
	}
	return db.UnlockProtectedEntries()
}
