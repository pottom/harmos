package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// write a config file at 0600 in a temp dir and return its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const valid = `
default = "work"
clipboard_timeout = "45s"

[[source]]
name  = "work"
type  = "pleasant"
url   = "https://pps.example:10001"
user  = "alice"
cache = "~/caches/work.kdbx"

[[source]]
name = "personal"
type = "kdbx"
path = "~/vaults/personal.kdbx"
`

func TestLoadValid(t *testing.T) {
	c, err := Load(writeConfig(t, valid))
	if err != nil {
		t.Fatal(err)
	}
	if c.ClipboardTimeout.Duration != 45*time.Second {
		t.Errorf("clipboard_timeout = %v, want 45s", c.ClipboardTimeout.Duration)
	}
	if len(c.Sources) != 2 {
		t.Fatalf("got %d sources, want 2", len(c.Sources))
	}
	work := c.Source("work")
	if work == nil || work.Type != Pleasant {
		t.Fatal("work source missing or wrong type")
	}
	if c.Source("nope") != nil {
		t.Error("Source(nonexistent) should be nil")
	}
	home, _ := os.UserHomeDir()
	if want := filepath.Join(home, "vaults/personal.kdbx"); c.Source("personal").Path != want {
		t.Errorf("~ not expanded: got %q want %q", c.Source("personal").Path, want)
	}
}

func TestDefaultClipboardTimeout(t *testing.T) {
	body := `
[[source]]
name = "x"
type = "kdbx"
path = "/tmp/x.kdbx"
`
	c, err := Load(writeConfig(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if c.ClipboardTimeout.Duration != DefaultClipboardTimeout {
		t.Errorf("default timeout = %v, want %v", c.ClipboardTimeout.Duration, DefaultClipboardTimeout)
	}
}

func TestRejectsLoosePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions are not enforced on Windows")
	}
	path := writeConfig(t, valid)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for group/world-readable config")
	}
}

func TestValidationErrors(t *testing.T) {
	cases := map[string]string{
		"duplicate name": `
[[source]]
name = "dup"
type = "kdbx"
path = "/a.kdbx"
[[source]]
name = "dup"
type = "kdbx"
path = "/b.kdbx"
`,
		"unknown type": `
[[source]]
name = "x"
type = "mystery"
path = "/a.kdbx"
`,
		"pleasant missing fields": `
[[source]]
name = "x"
type = "pleasant"
url  = "https://h:10001"
`,
		"kdbx missing path": `
[[source]]
name = "x"
type = "kdbx"
`,
		"no sources": `
default = "x"
`,
	}
	for name, body := range cases {
		if _, err := Load(writeConfig(t, body)); err == nil {
			t.Errorf("%s: expected a validation error, got nil", name)
		}
	}
}

func TestDanglingDefaultIsIgnored(t *testing.T) {
	path := writeConfig(t, "default = \"work\"\n[[source]]\nname = \"own\"\ntype = \"kdbx\"\npath = \"/x.kdbx\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("a dangling default should not fail load: %v", err)
	}
	if cfg.Default != "" {
		t.Errorf("dangling default should be cleared, got %q", cfg.Default)
	}
}
