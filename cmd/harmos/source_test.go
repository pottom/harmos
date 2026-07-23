package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pottom/harmos/internal/config"
)

func writeDummyKdbx(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("kdbx-placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAddKdbxWritesLoadableProfile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	kdbx := writeDummyKdbx(t, dir, "vault.kdbx")

	var out bytes.Buffer
	if err := runAddSource(cfgPath, kdbx, "", "", false, &out); err != nil {
		t.Fatalf("add: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load after add: %v", err)
	}
	p := cfg.Profile("vault") // name derived from vault.kdbx
	if p == nil {
		t.Fatal("profile 'vault' not found after add")
	}
	if p.Type != config.Kdbx || p.Path != kdbx {
		t.Errorf("profile = %+v, want kdbx at %s", p, kdbx)
	}
	if info, _ := os.Stat(cfgPath); info.Mode().Perm() != 0o600 {
		t.Errorf("config perms = %#o, want 0600", info.Mode().Perm())
	}
}

func TestAddKdbxRejectsDuplicate(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	kdbx := writeDummyKdbx(t, dir, "v.kdbx")
	var out bytes.Buffer
	if err := runAddSource(cfgPath, kdbx, "mine", "", false, &out); err != nil {
		t.Fatal(err)
	}
	// Without --force and no TTY, a duplicate name is an error (never a silent
	// overwrite).
	if err := runAddSource(cfgPath, kdbx, "mine", "", false, &out); err == nil {
		t.Fatal("adding a duplicate name should fail without --force")
	}
}

func TestAddKdbxForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	first := writeDummyKdbx(t, dir, "a.kdbx")
	second := writeDummyKdbx(t, dir, "b.kdbx")
	var out bytes.Buffer
	if err := runAddSource(cfgPath, first, "own", "", false, &out); err != nil {
		t.Fatal(err)
	}
	if err := runAddSource(cfgPath, second, "own", "", true, &out); err != nil {
		t.Fatalf("force overwrite: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(cfg.Profiles); n != 1 {
		t.Fatalf("want 1 profile after overwrite, got %d", n)
	}
	if p := cfg.Profile("own"); p == nil || p.Path != second {
		t.Errorf("profile not overwritten: %+v", p)
	}
}

// Overwriting one profile must leave the rest of the file — comments, other
// profiles, top-level keys — byte-for-byte intact.
func TestAddKdbxOverwritePreservesRest(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	newKdbx := writeDummyKdbx(t, dir, "new.kdbx")
	seed := "# my harmos config\n" +
		"clipboard_timeout = \"45s\"\n" +
		"\n" +
		"[[profile]]\n" +
		"name = \"own\"\n" +
		"type = \"kdbx\"\n" +
		"path = \"/old/own.kdbx\"\n" +
		"\n" +
		"# the work server\n" +
		"[[profile]]\n" +
		"name = \"work\"\n" +
		"type = \"pleasant\"\n" +
		"url = \"https://pps.example.invalid\"\n" +
		"user = \"svc\"\n" +
		"cache = \"" + filepath.Join(dir, "work.kdbx") + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runAddSource(cfgPath, newKdbx, "own", "", true, &out); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{
		"# my harmos config",
		"clipboard_timeout = \"45s\"",
		"# the work server",
		"url = \"https://pps.example.invalid\"",
		"path = " + strconv.Quote(newKdbx), // own's path updated
	} {
		if !strings.Contains(text, want) {
			t.Errorf("result missing %q\n---\n%s", want, text)
		}
	}
	if strings.Contains(text, "/old/own.kdbx") {
		t.Error("old path should be gone after overwrite")
	}
	// still two profiles, own now points at the new file
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Profiles) != 2 {
		t.Fatalf("want 2 profiles, got %d", len(cfg.Profiles))
	}
	if p := cfg.Profile("own"); p == nil || p.Path != newKdbx {
		t.Errorf("own not updated: %+v", p)
	}
}

func TestAddKdbxRejectsMissingFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	var out bytes.Buffer
	if err := runAddSource(cfgPath, filepath.Join(dir, "nope.kdbx"), "x", "", false, &out); err == nil {
		t.Fatal("adding a non-existent file should fail")
	}
}

func TestAddKdbxAppendsAndMasterGating(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	seed := "[[profile]]\n" +
		"name = \"work\"\n" +
		"type = \"pleasant\"\n" +
		"url = \"https://pps.example.invalid\"\n" +
		"user = \"svc\"\n" +
		"cache = \"" + filepath.Join(dir, "work.kdbx") + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	kdbx := writeDummyKdbx(t, dir, "personal.kdbx")

	var out bytes.Buffer
	if err := runAddSource(cfgPath, kdbx, "personal", "", false, &out); err != nil {
		t.Fatalf("add: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Profiles) != 2 {
		t.Fatalf("want 2 profiles, got %d", len(cfg.Profiles))
	}
	if cfg.Profile("work") == nil || cfg.Profile("personal") == nil {
		t.Error("both the seeded pleasant and the added kdbx profile must be present")
	}
}
