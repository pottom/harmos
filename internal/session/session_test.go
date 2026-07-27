package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/secret"
)

func makeKDBX(t *testing.T, path, password, title string) {
	t.Helper()
	db := gokeepasslib.NewDatabase(gokeepasslib.WithDatabaseKDBXVersion4())
	db.Credentials = gokeepasslib.NewPasswordCredentials(password)
	e := gokeepasslib.NewEntry()
	e.Values = append(e.Values, gokeepasslib.ValueData{Key: "Title", Value: gokeepasslib.V{Content: title}})
	g := gokeepasslib.NewGroup()
	g.Name = "Root"
	g.Entries = []gokeepasslib.Entry{e}
	db.Content.Root = &gokeepasslib.RootData{Groups: []gokeepasslib.Group{g}}
	if err := db.LockProtectedEntries(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := gokeepasslib.NewEncoder(f).Encode(db); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
}

func writeConfig(t *testing.T, dir string) *config.Config {
	t.Helper()
	body := fmt.Sprintf(`
[[source]]
name  = "work"
type  = "pleasant"
url   = "https://pps:10001"
user  = "u"
cache = %q

[[source]]
name = "personal"
type = "kdbx"
path = %q
`, filepath.Join(dir, "work.kdbx"), filepath.Join(dir, "personal.kdbx"))
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestOpenAllSources(t *testing.T) {
	dir := t.TempDir()
	makeKDBX(t, filepath.Join(dir, "work.kdbx"), "master", "db-prod")
	makeKDBX(t, filepath.Join(dir, "personal.kdbx"), "personalpw", "router")
	cfg := writeConfig(t, dir)

	ask := func(p config.Source, retry bool) (secret.Secret, bool, error) {
		if p.Type == config.Pleasant {
			return secret.New("master"), false, nil
		}
		return secret.New("personalpw"), false, nil
	}
	res := Open(cfg, ask)

	if len(res.Excluded) != 0 {
		t.Fatalf("unexpected excluded: %+v", res.Excluded)
	}
	got := map[string]string{} // title -> source
	for _, e := range res.Entries {
		got[e.Title] = e.Source
	}
	if got["db-prod"] != "work" {
		t.Errorf("db-prod source = %q, want work", got["db-prod"])
	}
	if got["router"] != "personal" {
		t.Errorf("router source = %q, want personal", got["router"])
	}
}

func TestPartialFailureIsNotFatal(t *testing.T) {
	dir := t.TempDir()
	makeKDBX(t, filepath.Join(dir, "work.kdbx"), "master", "db-prod")
	makeKDBX(t, filepath.Join(dir, "personal.kdbx"), "personalpw", "router")
	cfg := writeConfig(t, dir)

	// wrong master (returned again on retry) → the Pleasant cache is excluded,
	// but personal still opens
	ask := func(p config.Source, retry bool) (secret.Secret, bool, error) {
		if p.Type == config.Pleasant {
			return secret.New("WRONG"), false, nil
		}
		return secret.New("personalpw"), false, nil
	}
	res := Open(cfg, ask)

	if len(res.Excluded) != 1 || res.Excluded[0].Source != "work" {
		t.Fatalf("expected 'work' excluded, got %+v", res.Excluded)
	}
	if len(res.Entries) != 1 || res.Entries[0].Title != "router" {
		t.Fatalf("expected only router to open, got %+v", res.Entries)
	}
}

// A wrong password on the first try is re-prompted and unlocks on the retry —
// caught immediately, not deferred to an excluded-source warning.
func TestRetryUnlocksOnSecondTry(t *testing.T) {
	dir := t.TempDir()
	makeKDBX(t, filepath.Join(dir, "work.kdbx"), "master", "db-prod")
	makeKDBX(t, filepath.Join(dir, "personal.kdbx"), "personalpw", "router")
	cfg := writeConfig(t, dir)

	tries := map[string]int{}
	ask := func(p config.Source, retry bool) (secret.Secret, bool, error) {
		tries[p.Name]++
		if p.Type == config.Pleasant {
			if !retry {
				return secret.New("WRONG"), true, nil // interactive → retry allowed
			}
			return secret.New("master"), true, nil
		}
		return secret.New("personalpw"), false, nil
	}
	res := Open(cfg, ask)

	if len(res.Excluded) != 0 {
		t.Fatalf("nothing should be excluded, got %+v", res.Excluded)
	}
	if tries["work"] < 2 {
		t.Errorf("work should have been re-prompted, got %d attempt(s)", tries["work"])
	}
}

// A non-interactive wrong password (keyring/env) is tried once, not retried.
func TestNonInteractiveWrongPasswordNotRetried(t *testing.T) {
	dir := t.TempDir()
	makeKDBX(t, filepath.Join(dir, "work.kdbx"), "master", "db-prod")
	makeKDBX(t, filepath.Join(dir, "personal.kdbx"), "personalpw", "router")
	cfg := writeConfig(t, dir)

	tries := 0
	ask := func(p config.Source, retry bool) (secret.Secret, bool, error) {
		if p.Type == config.Pleasant {
			tries++
			return secret.New("WRONG"), false, nil // not interactive
		}
		return secret.New("personalpw"), false, nil
	}
	res := Open(cfg, ask)

	if tries != 1 {
		t.Errorf("a non-interactive wrong password should be tried once, got %d", tries)
	}
	if len(res.Excluded) != 1 || res.Excluded[0].Source != "work" {
		t.Fatalf("expected 'work' excluded, got %+v", res.Excluded)
	}
}
