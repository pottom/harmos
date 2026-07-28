// Package atomicfile replaces a file's contents without ever leaving a partial
// one behind: the bytes go to a temp file in the same directory, are flushed to
// stable storage, and only then take the destination's place by rename.
//
// The same directory matters — rename is only atomic within one filesystem, and
// a temp dir is often a different one. Flushing matters because rename orders
// the directory entry, not the data: without the fsync a crash can leave the
// name pointing at a file whose blocks were never written.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write replaces path with whatever fn writes, at mode perm. fn is called with
// the open temp file; if it returns an error, path is left untouched.
func Write(path string, perm os.FileMode, fn func(*os.File) error) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".harmos-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	// Until the rename succeeds the temp file is garbage: remove it on every
	// path out, including a panic in fn.
	done := false
	defer func() {
		if !done {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if err := fn(tmp); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	done = true

	// Flush the rename itself. Best effort: not every platform or filesystem
	// lets you open a directory (Windows does not), and there the rename's
	// durability is the OS's business rather than something we can force.
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// WriteBytes is Write for content already in memory.
func WriteBytes(path string, perm os.FileMode, data []byte) error {
	return Write(path, perm, func(f *os.File) error {
		_, err := f.Write(data)
		return err
	})
}
