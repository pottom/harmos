package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/pottom/harmos/internal/config"
)

// qt quotes a value as a TOML basic string, escaping backslashes so Windows
// paths (C:\Users\…) don't turn into invalid \U escapes.
func qt(s string) string { return strconv.Quote(s) }

func writeDummyKdbx(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("kdbx-placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAddKdbxWritesLoadableSource(t *testing.T) {
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
	p := cfg.Source("vault") // name derived from vault.kdbx
	if p == nil {
		t.Fatal("source 'vault' not found after add")
	}
	if p.Type != config.Kdbx || p.Path != kdbx {
		t.Errorf("source = %+v, want kdbx at %s", p, kdbx)
	}
	if info, _ := os.Stat(cfgPath); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
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
	if n := len(cfg.Sources); n != 1 {
		t.Fatalf("want 1 source after overwrite, got %d", n)
	}
	if p := cfg.Source("own"); p == nil || p.Path != second {
		t.Errorf("source not overwritten: %+v", p)
	}
}

// Overwriting one source must leave the rest of the file — comments, other
// sources, top-level keys — byte-for-byte intact.
func TestAddKdbxOverwritePreservesRest(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	newKdbx := writeDummyKdbx(t, dir, "new.kdbx")
	seed := "# my harmos config\n" +
		"clipboard_timeout = \"45s\"\n" +
		"\n" +
		"[[source]]\n" +
		"name = \"own\"\n" +
		"type = \"kdbx\"\n" +
		"path = \"/old/own.kdbx\"\n" +
		"\n" +
		"# the work server\n" +
		"[[source]]\n" +
		"name = \"work\"\n" +
		"type = \"pleasant\"\n" +
		"url = \"https://pps.example.invalid\"\n" +
		"user = \"svc\"\n" +
		"cache = " + qt(filepath.Join(dir, "work.kdbx")) + "\n"
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
	// still two sources, own now points at the new file
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Sources) != 2 {
		t.Fatalf("want 2 sources, got %d", len(cfg.Sources))
	}
	if p := cfg.Source("own"); p == nil || p.Path != newKdbx {
		t.Errorf("own not updated: %+v", p)
	}
}

func TestSourcesShowsKeyfileWhenPresent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	kdbx := writeDummyKdbx(t, dir, "own.kdbx")
	key := writeDummyKdbx(t, dir, "id.key")
	var out bytes.Buffer
	if err := runAddSource(cfgPath, kdbx, "own", key, false, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runSources(cfgPath, true, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "KEYFILE") || !strings.Contains(s, key) {
		t.Errorf("sources should show the keyfile:\n%s", s)
	}
}

func TestSourcesOmitsKeyfileColumnWhenNone(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	kdbx := writeDummyKdbx(t, dir, "own.kdbx")
	var out bytes.Buffer
	if err := runAddSource(cfgPath, kdbx, "own", "", false, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runSources(cfgPath, true, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "KEYFILE") {
		t.Errorf("no keyfile anywhere → no KEYFILE column:\n%s", out.String())
	}
}

func TestAddSourceToEmptyConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	// an existing but source-less config (e.g. after removing the last source)
	if err := os.WriteFile(cfgPath, []byte("# only a comment\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	kdbx := writeDummyKdbx(t, dir, "own.kdbx")
	var out bytes.Buffer
	if err := runAddSource(cfgPath, kdbx, "own", "", false, &out); err != nil {
		t.Fatalf("add to empty config: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source("own") == nil {
		t.Error("own not added to the empty config")
	}
}

func TestAddPleasantSource(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	cache := filepath.Join(dir, "work.kdbx")
	var out bytes.Buffer
	if err := runAddPleasant(cfgPath, "work", "https://pps.invalid:10001", "svc", cache, "", false, &out); err != nil {
		t.Fatalf("add pleasant: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Source("work")
	if p == nil || p.Type != config.Pleasant {
		t.Fatalf("pleasant source missing: %+v", p)
	}
	if p.URL != "https://pps.invalid:10001" || p.User != "svc" || p.Cache != cache {
		t.Errorf("pleasant fields wrong: %+v", p)
	}
}

func TestAddPleasantRequiresFields(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	var out bytes.Buffer
	if err := runAddPleasant(cfgPath, "work", "", "svc", "/x", "", false, &out); err == nil {
		t.Fatal("a Pleasant add must require --url")
	}
}

func TestAddPleasantDefaultCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	cfgPath := filepath.Join(dir, "config.toml")
	var out bytes.Buffer
	if err := runAddPleasant(cfgPath, "work", "https://x.invalid", "u", "", "", false, &out); err != nil {
		t.Fatalf("add pleasant: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "data", "harmos", "work.kdbx")
	if p := cfg.Source("work"); p == nil || p.Cache != want {
		t.Fatalf("cache = %+v, want %s", p, want)
	}
	if _, err := os.Stat(filepath.Dir(want)); err != nil {
		t.Errorf("cache directory not created: %v", err)
	}
}

func TestAddPleasantDefaultCacheNeedsName(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	var out bytes.Buffer
	if err := runAddPleasant(cfgPath, "", "https://x.invalid", "u", "", "", false, &out); err == nil {
		t.Fatal("defaulting the cache requires a --name")
	}
}

func TestLoadConfigNoSourcesGuidance(t *testing.T) {
	dir := t.TempDir()
	// a missing config file
	if _, err := loadConfigAt(filepath.Join(dir, "nope.toml")); err == nil || !strings.Contains(err.Error(), "add-source") {
		t.Errorf("missing config should guide to add-source, got %v", err)
	}
	// an empty config file (no sources)
	empty := filepath.Join(dir, "empty.toml")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfigAt(empty); err == nil || !strings.Contains(err.Error(), "add-source") {
		t.Errorf("empty config should guide to add-source, got %v", err)
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
	seed := "[[source]]\n" +
		"name = \"work\"\n" +
		"type = \"pleasant\"\n" +
		"url = \"https://pps.example.invalid\"\n" +
		"user = \"svc\"\n" +
		"cache = " + qt(filepath.Join(dir, "work.kdbx")) + "\n"
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
	if len(cfg.Sources) != 2 {
		t.Fatalf("want 2 sources, got %d", len(cfg.Sources))
	}
	if cfg.Source("work") == nil || cfg.Source("personal") == nil {
		t.Error("both the seeded pleasant and the added kdbx source must be present")
	}
}
