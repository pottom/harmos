package config

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsureKeyfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "work.key")
	if err := EnsureKeyfile(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 48 {
		t.Errorf("keyfile size = %d, want 48", info.Size())
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("keyfile perms = %#o, want 0600", info.Mode().Perm())
	}

	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A second call must leave the existing key untouched — a source keeps its
	// key across syncs, or every sync would orphan the previous cache.
	if err := EnsureKeyfile(path); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Error("EnsureKeyfile regenerated an existing keyfile")
	}
}

func TestDefaultCacheKeyfilePath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.FromSlash("/tmp/cfg"))
	got, err := DefaultCacheKeyfilePath("work")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.FromSlash("/tmp/cfg/harmos/work.key")
	if got != want {
		t.Errorf("DefaultCacheKeyfilePath = %q, want %q", got, want)
	}
}
