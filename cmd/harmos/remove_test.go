package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gokeyring "github.com/zalando/go-keyring"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/keyring"
	"github.com/pottom/harmos/internal/secret"
)

func seedTwoKdbx(t *testing.T, dir string) (cfgPath, aPath string) {
	t.Helper()
	cfgPath = filepath.Join(dir, "config.toml")
	aPath = writeDummyKdbx(t, dir, "a.kdbx")
	bPath := writeDummyKdbx(t, dir, "b.kdbx")
	seed := "# my config\n" +
		"[[profile]]\nname = \"a\"\ntype = \"kdbx\"\npath = " + qt(aPath) + "\n" +
		"\n# keep me\n[[profile]]\nname = \"b\"\ntype = \"kdbx\"\npath = " + qt(bPath) + "\n"
	if err := os.WriteFile(cfgPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath, aPath
}

func TestRemoveKdbxPreservesRest(t *testing.T) {
	dir := t.TempDir()
	cfgPath, aPath := seedTwoKdbx(t, dir)

	var out bytes.Buffer
	if err := runRemoveSource(cfgPath, "a", false, false, &out); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(aPath); err != nil {
		t.Error("kdbx file must survive when the file is not deleted")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile("a") != nil {
		t.Error("profile a should be gone")
	}
	if cfg.Profile("b") == nil {
		t.Error("profile b should remain")
	}
	got, _ := os.ReadFile(cfgPath)
	for _, want := range []string{"# my config", "# keep me", "name = \"b\""} {
		if !strings.Contains(string(got), want) {
			t.Errorf("result missing %q\n%s", want, got)
		}
	}
}

func TestRemoveKdbxDeletesFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath, aPath := seedTwoKdbx(t, dir)
	var out bytes.Buffer
	if err := runRemoveSource(cfgPath, "a", true, false, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(aPath); !os.IsNotExist(err) {
		t.Error("kdbx file should be deleted when requested")
	}
}

func TestRemoveSourcePleasantForgetsMasterWhenLast(t *testing.T) {
	gokeyring.MockInit()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	own := writeDummyKdbx(t, dir, "own.kdbx")
	seed := "[[profile]]\nname = \"work\"\ntype = \"pleasant\"\n" +
		"url = \"https://x.invalid\"\nuser = \"u\"\ncache = " + qt(filepath.Join(dir, "w.kdbx")) + "\n" +
		"\n[[profile]]\nname = \"own\"\ntype = \"kdbx\"\npath = " + qt(own) + "\n"
	if err := os.WriteFile(cfgPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := keyring.StoreMaster(secret.New("m")); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	// removing the only Pleasant source, asking to forget the password → master gone
	if err := runRemoveSource(cfgPath, "work", false, true, &out); err != nil {
		t.Fatalf("remove pleasant: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile("work") != nil {
		t.Error("pleasant source should be gone")
	}
	if cfg.Profile("own") == nil {
		t.Error("the kdbx source should remain")
	}
	if _, ok, _ := keyring.FetchMaster(); ok {
		t.Error("the shared master should be forgotten when the last Pleasant source is removed")
	}
}

func TestRemoveSourcePleasantKeepsMasterWhenOthersRemain(t *testing.T) {
	gokeyring.MockInit()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	seed := "[[profile]]\nname = \"a\"\ntype = \"pleasant\"\nurl = \"https://a.invalid\"\nuser = \"u\"\ncache = " + qt(filepath.Join(dir, "a.kdbx")) + "\n" +
		"\n[[profile]]\nname = \"b\"\ntype = \"pleasant\"\nurl = \"https://b.invalid\"\nuser = \"u\"\ncache = " + qt(filepath.Join(dir, "b.kdbx")) + "\n"
	if err := os.WriteFile(cfgPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := keyring.StoreMaster(secret.New("m")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runRemoveSource(cfgPath, "a", false, true, &out); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := keyring.FetchMaster(); !ok {
		t.Error("the master must be kept while another Pleasant source remains")
	}
}

func TestRemoveSourceCleansDefault(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	own := writeDummyKdbx(t, dir, "own.kdbx")
	work := writeDummyKdbx(t, dir, "work.kdbx")
	seed := "default = \"work\"\n\n" +
		"[[profile]]\nname = \"work\"\ntype = \"kdbx\"\npath = " + qt(work) + "\n\n" +
		"[[profile]]\nname = \"own\"\ntype = \"kdbx\"\npath = " + qt(own) + "\n"
	if err := os.WriteFile(cfgPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runRemoveSource(cfgPath, "work", false, false, &out); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(got), "default =") {
		t.Errorf("the dangling default line should be removed with its profile:\n%s", got)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config should load after removal: %v", err)
	}
	if cfg.Profile("own") == nil {
		t.Error("own should remain")
	}
}

func TestRemoveKdbxLastProfile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	kdbx := writeDummyKdbx(t, dir, "only.kdbx")
	var out bytes.Buffer
	if err := runAddSource(cfgPath, kdbx, "only", "", false, &out); err != nil {
		t.Fatal(err)
	}
	if err := runRemoveSource(cfgPath, "only", false, false, &out); err != nil {
		t.Fatalf("removing last profile: %v", err)
	}
	if !strings.Contains(out.String(), "no sources") {
		t.Error("removing the last profile should note the config has no sources")
	}
}

func TestRemovePasswordAll(t *testing.T) {
	gokeyring.MockInit()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	own := writeDummyKdbx(t, dir, "own.kdbx")
	seed := "[[profile]]\nname = \"work\"\ntype = \"pleasant\"\n" +
		"url = \"https://x.invalid\"\nuser = \"u\"\ncache = " + qt(filepath.Join(dir, "w.kdbx")) + "\n" +
		"\n[[profile]]\nname = \"own\"\ntype = \"kdbx\"\npath = " + qt(own) + "\n"
	if err := os.WriteFile(cfgPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := keyring.Store("own", secret.New("p")); err != nil {
		t.Fatal(err)
	}
	if err := keyring.StoreMaster(secret.New("m")); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runRemovePassword(cfgPath, "all", &out); err != nil {
		t.Fatalf("remove all: %v", err)
	}
	if _, ok, _ := keyring.Fetch("own"); ok {
		t.Error("own password should be gone")
	}
	if _, ok, _ := keyring.FetchMaster(); ok {
		t.Error("master should be gone")
	}
}

func TestRemovePasswordRejectsUnknown(t *testing.T) {
	gokeyring.MockInit()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	own := writeDummyKdbx(t, dir, "own.kdbx")
	if err := os.WriteFile(cfgPath, []byte("[[profile]]\nname = \"own\"\ntype = \"kdbx\"\npath = "+qt(own)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runRemovePassword(cfgPath, "own,bogus", &out); err == nil {
		t.Fatal("remove-password should reject an unknown name")
	}
}
