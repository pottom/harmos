package pleasant

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"

	"github.com/pottom/harmos/internal/secret"
)

func TestSyncProducesOpenableCache(t *testing.T) {
	srv := fakeServer(t, true)
	c := loggedInClient(t, srv)
	cachePath := filepath.Join(t.TempDir(), "sub", "work.kdbx") // dir is created

	const master = "master-pw"
	res, err := Sync(t.Context(), c, srv.URL, SyncOptions{
		Comment:   "harmos test sync",
		CachePath: cachePath,
		Master:    secret.New(master),
		Now:       time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Entries != 7 || res.Attachments != 3 {
		t.Fatalf("stats: entries=%d attachments=%d", res.Entries, res.Attachments)
	}

	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("cache not written: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("cache perms = %#o, want 0600", info.Mode().Perm())
	}

	// the cache decrypts with the master and holds the expected entries
	f, err := os.Open(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials(master)
	if err := gokeepasslib.NewDecoder(f).Decode(db); err != nil {
		t.Fatalf("decode cache: %v", err)
	}
	if err := db.UnlockProtectedEntries(); err != nil {
		t.Fatal(err)
	}
	if got := len(allEntries(db)); got != 7 {
		t.Errorf("cache has %d entries, want 7", got)
	}

	// no temp files left behind in the cache dir
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(cachePath), ".harmos-*"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}

func TestSyncRefusesWhenOfflineUnavailable(t *testing.T) {
	srv := fakeServer(t, false)
	c := loggedInClient(t, srv)
	cachePath := filepath.Join(t.TempDir(), "work.kdbx")

	if _, err := Sync(t.Context(), c, srv.URL, SyncOptions{
		CachePath: cachePath, Master: secret.New("m"), Now: time.Now(),
	}); err == nil {
		t.Fatal("expected sync to refuse when IsOfflineAvailable is false")
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Error("no cache should be written when offline is unavailable")
	}
}

func TestSyncRefusesExpiredPackage(t *testing.T) {
	srv := fakeServer(t, true)
	c := loggedInClient(t, srv)
	cachePath := filepath.Join(t.TempDir(), "work.kdbx")

	// Now past the package's 9999 Expiry so it reads as expired.
	future := time.Date(10001, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := Sync(t.Context(), c, srv.URL, SyncOptions{
		CachePath: cachePath, Master: secret.New("m"), Now: future,
	}); err == nil {
		t.Fatal("expected sync to refuse an expired package")
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Error("no cache should be written for an expired package")
	}
}
