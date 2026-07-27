package config

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSetGeneratorRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	// a config with a source, so Load succeeds
	if _, err := WriteKdbxProfile(path, "own", filepath.Join(dir, "own.kdbx"), "", false); err != nil {
		t.Fatal(err)
	}
	if err := SetGenerator(path, 24, true, false, true, false, true, false); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GenLength != 24 {
		t.Errorf("length = %d, want 24", cfg.GenLength)
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
	if err := SetGenerator(path, 30, true, true, true, true, false, false); err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.GenLength != 30 || cfg2.Profile("own") == nil {
		t.Errorf("second write broke the file: len=%d own=%v", cfg2.GenLength, cfg2.Profile("own"))
	}
}

func TestWriteRemoveProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	verb, err := WriteKdbxProfile(path, "own", filepath.Join(dir, "own.kdbx"), "", false)
	if err != nil || verb != "added" {
		t.Fatalf("write kdbx: verb=%q err=%v", verb, err)
	}
	if ok, _ := ProfileExists(path, "own"); !ok {
		t.Error("own should exist after add")
	}

	// a duplicate without overwrite is refused
	if _, err := WriteKdbxProfile(path, "own", filepath.Join(dir, "x.kdbx"), "", false); !errors.Is(err, ErrProfileExists) {
		t.Errorf("duplicate should be ErrProfileExists, got %v", err)
	}

	// overwrite rewrites just that block
	newPath := filepath.Join(dir, "new.kdbx")
	if verb, err := WriteKdbxProfile(path, "own", newPath, "", true); err != nil || verb != "updated" {
		t.Fatalf("overwrite: verb=%q err=%v", verb, err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile("own").Path != newPath {
		t.Errorf("path = %q, want %q", cfg.Profile("own").Path, newPath)
	}

	// add a second profile, then remove the first
	if _, err := WritePleasantProfile(path, "work", "https://x.invalid", "u", filepath.Join(dir, "w.kdbx"), "", false); err != nil {
		t.Fatalf("write pleasant: %v", err)
	}
	remaining, err := RemoveProfile(path, "own")
	if err != nil || remaining != 1 {
		t.Fatalf("remove: remaining=%d err=%v", remaining, err)
	}
	if ok, _ := ProfileExists(path, "own"); ok {
		t.Error("own should be gone after remove")
	}
	if ok, _ := ProfileExists(path, "work"); !ok {
		t.Error("work should remain")
	}
}

func TestSetTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if _, err := WriteKdbxProfile(path, "own", filepath.Join(dir, "own.kdbx"), "", false); err != nil {
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
	if cfg.Profile("own") == nil {
		t.Error("the profile should survive setting a top-level key")
	}
}
