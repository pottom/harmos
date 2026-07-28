package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteReplacesContents(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteBytes(p, 0o600, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("content = %q, want new", got)
	}
}

func TestWriteCreatesWithMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := WriteBytes(p, 0o600, []byte("x")); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no POSIX mode bits — Chmod there only toggles the read-only
	// attribute, and a writable file always reports 0666.
	if perm := st.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

// The point of the package: a failed write must leave the previous file whole.
// Callers use this for a password vault and for the config, and a truncated one
// of either is worse than no write at all.
func TestFailedWriteLeavesTheOriginal(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	boom := errors.New("boom")
	err := Write(p, 0o600, func(f *os.File) error {
		if _, werr := f.Write([]byte("partial")); werr != nil {
			return werr
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Errorf("content = %q, want the untouched original", got)
	}
}

func TestFailedWriteLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	_ = Write(p, 0o600, func(*os.File) error { return errors.New("no") })

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".harmos-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("a failed write created the destination")
	}
}

// The temp file has to be a sibling: rename is only atomic within one
// filesystem, and $TMPDIR is regularly a different one.
func TestTempFileIsASibling(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")

	var tmpDir string
	if err := Write(p, 0o600, func(f *os.File) error {
		tmpDir = filepath.Dir(f.Name())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if tmpDir != dir {
		t.Errorf("temp file lived in %q, want %q", tmpDir, dir)
	}
}

func TestWriteReportsAMissingDirectory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope", "f")
	if err := WriteBytes(p, 0o600, []byte("x")); err == nil {
		t.Error("writing into a missing directory should fail")
	}
}
