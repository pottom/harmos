package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetGeneratorRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	// a config with a source, so Load succeeds
	if _, err := WriteKdbxSource(path, "own", filepath.Join(dir, "own.kdbx"), "", false); err != nil {
		t.Fatal(err)
	}
	if err := SetGenerator(path, 24, true, false, true, false, true, false, "xy%"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GenLength != 24 {
		t.Errorf("length = %d, want 24", cfg.GenLength)
	}
	if cfg.GenExclude != "xy%" {
		t.Errorf("exclude = %q, want xy%%", cfg.GenExclude)
	}
	for name, got := range map[string]*bool{
		"lower": cfg.GenLower, "upper": cfg.GenUpper, "digit": cfg.GenDigit,
		"symbol": cfg.GenSymbol, "no_ambiguous": cfg.GenNoAmbig, "one_each": cfg.GenOneEach,
	} {
		if got == nil {
			t.Errorf("%s should be set", name)
		}
	}
	if *cfg.GenLower != true || *cfg.GenUpper != false || *cfg.GenOneEach != false {
		t.Error("boolean values round-tripped wrong")
	}
	// updating again rewrites in place (no duplicate keys)
	if err := SetGenerator(path, 30, true, true, true, true, false, false, ""); err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.GenLength != 30 || cfg2.Source("own") == nil {
		t.Errorf("second write broke the file: len=%d own=%v", cfg2.GenLength, cfg2.Source("own"))
	}
}

func TestWriteRemoveSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	verb, err := WriteKdbxSource(path, "own", filepath.Join(dir, "own.kdbx"), "", false)
	if err != nil || verb != "added" {
		t.Fatalf("write kdbx: verb=%q err=%v", verb, err)
	}
	if ok, _ := SourceExists(path, "own"); !ok {
		t.Error("own should exist after add")
	}

	// a duplicate without overwrite is refused
	if _, err := WriteKdbxSource(path, "own", filepath.Join(dir, "x.kdbx"), "", false); !errors.Is(err, ErrSourceExists) {
		t.Errorf("duplicate should be ErrSourceExists, got %v", err)
	}

	// overwrite rewrites just that block
	newPath := filepath.Join(dir, "new.kdbx")
	if verb, err := WriteKdbxSource(path, "own", newPath, "", true); err != nil || verb != "updated" {
		t.Fatalf("overwrite: verb=%q err=%v", verb, err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source("own").Path != newPath {
		t.Errorf("path = %q, want %q", cfg.Source("own").Path, newPath)
	}

	// add a second source, then remove the first
	if _, err := WritePleasantSource(path, "work", "https://x.invalid", "u", filepath.Join(dir, "w.kdbx"), "", false); err != nil {
		t.Fatalf("write pleasant: %v", err)
	}
	remaining, err := RemoveSource(path, "own")
	if err != nil || remaining != 1 {
		t.Fatalf("remove: remaining=%d err=%v", remaining, err)
	}
	if ok, _ := SourceExists(path, "own"); ok {
		t.Error("own should be gone after remove")
	}
	if ok, _ := SourceExists(path, "work"); !ok {
		t.Error("work should remain")
	}
}

func TestSetTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if _, err := WriteKdbxSource(path, "own", filepath.Join(dir, "own.kdbx"), "", false); err != nil {
		t.Fatal(err)
	}
	// insert a new top-level key
	if err := SetTopLevelKey(path, "theme", "nord"); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := Load(path); cfg.Theme != "nord" {
		t.Fatalf("theme = %q, want nord", cfg.Theme)
	}
	// update it in place
	if err := SetTopLevelKey(path, "theme", "dracula"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "dracula" {
		t.Errorf("theme = %q, want dracula", cfg.Theme)
	}
	if cfg.Source("own") == nil {
		t.Error("the source should survive setting a top-level key")
	}
}

// The write opt-in survives a round trip, and turning it off removes the key
// rather than writing a default value into the file.
func TestSetSourceWritable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `default = "work"

[[source]]
name = "work"
type = "kdbx"
path = "/tmp/work.kdbx"

[[source]]
name = "other"
type = "kdbx"
path = "/tmp/other.kdbx"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := SetSourceWritable(path, "work", true); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range cfg.Sources {
		switch s.Name {
		case "work":
			if !s.Writable {
				t.Error("work should be writable")
			}
		case "other":
			if s.Writable {
				t.Error("setting one source must not touch another")
			}
		}
	}

	// Everything else in the file survives.
	got, _ := os.ReadFile(path)
	for _, want := range []string{`default = "work"`, `path = "/tmp/work.kdbx"`, `name = "other"`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("the rest of the config should be untouched, %q is gone:\n%s", want, got)
		}
	}

	// Off again removes the key: absent is already the safe default, and a
	// config carrying every setting at its default is harder to read.
	if err := SetSourceWritable(path, "work", false); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if strings.Contains(string(got), "writable") {
		t.Errorf("turning it off should remove the key:\n%s", got)
	}
	cfg, _ = Load(path)
	for _, s := range cfg.Sources {
		if s.Writable {
			t.Errorf("%s should be read-only again", s.Name)
		}
	}
}

// Setting it twice must not leave two keys behind.
func TestSetSourceWritableIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[[source]]\nname = \"a\"\ntype = \"kdbx\"\npath = \"/tmp/a.kdbx\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := SetSourceWritable(path, "a", true); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(path)
	if n := strings.Count(string(got), "writable"); n != 1 {
		t.Errorf("writable appears %d times:\n%s", n, got)
	}
}

func TestSetSourceWritableUnknownSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[[source]]\nname = \"a\"\ntype = \"kdbx\"\npath = \"/tmp/a.kdbx\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetSourceWritable(path, "nope", true); err == nil {
		t.Error("setting an unknown source should fail rather than doing nothing")
	}
}

// A source name is not only a label: identities are "<name>:<uuid>" and
// "<name>:g:<uuid>" with a "#n" suffix for duplicates, so a name carrying those
// characters makes every ID in that source ambiguous. The failure used to be
// delayed and total — staging worked, and every save failed forever with
// "malformed id".
func TestSourceNamesThatWouldBreakIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	bad := []string{"a:b", "a#1", "prod:2026", "x#", "", "   ", " lead", "trail ", "a/b", "a\\b", "a\tb", "a\nb"}
	for _, name := range bad {
		if err := ValidSourceName(name); err == nil {
			t.Errorf("%q should be refused as a source name", name)
		}
		if _, err := WriteKdbxSource(path, name, filepath.Join(dir, "v.kdbx"), "", false); err == nil {
			t.Errorf("%q was written to the config", name)
		}
	}

	good := []string{"own", "work", "acme-prod", "my vault", "Vault_2", "ékezetes"}
	for _, name := range good {
		if err := ValidSourceName(name); err != nil {
			t.Errorf("%q should be allowed: %v", name, err)
		}
	}
}

// Editing a source keeps every key the form does not collect.
//
// The block builders write the fields their form has, which is not every field
// a source can carry: editing or renaming one dropped `writable = true`, so a
// vault the user had unlocked for editing came back read-only at the next
// launch with nothing having said so — and until then the running session and
// the config disagreed about it.
func TestEditingASourceKeepsWhatTheFormDoesNotAskFor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if _, err := WriteKdbxSource(path, "own", "/tmp/own.kdbx", "/tmp/own.key", false); err != nil {
		t.Fatal(err)
	}
	if err := SetSourceWritable(path, "own", true); err != nil {
		t.Fatal(err)
	}

	// Edit it the way the Settings form does: same name, a new path.
	if _, err := WriteKdbxSource(path, "own", "/tmp/moved.kdbx", "/tmp/own.key", true); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Source("own")
	if p == nil {
		t.Fatal("the source went missing")
	}
	if p.Path != "/tmp/moved.kdbx" {
		t.Errorf("path = %q, the edit should have landed", p.Path)
	}
	if !p.Writable {
		t.Error("writable = true was dropped by an edit that never asked about it")
	}
	if p.Keyfile == "" {
		t.Error("and the keyfile went with it")
	}
}
