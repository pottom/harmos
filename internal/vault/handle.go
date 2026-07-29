package vault

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

// Handle is an opened kdbx that can be written back. Open (the read-only path)
// throws away everything a write would need — the decoded database, the file
// path, the credentials — so a caller that intends to save keeps a Handle
// instead and takes its read-only Snapshot for browsing.
//
// Locking discipline, because getting it wrong silently corrupts every
// protected value:
//
//   - OpenHandle leaves the database UNLOCKED (protected values are plaintext).
//   - Encode expects it LOCKED and leaves it LOCKED — it unlocks and re-locks
//     internally to fix the value order, which only works from a locked start.
//
// So Save locks exactly once on the way in and unlocks exactly once on the way
// out. Nowhere else in this package may call either.
type Handle struct {
	db     *gokeepasslib.Database
	path   string
	source string
	creds  *gokeepasslib.DBCredentials // sha256 hashes, never a plaintext secret
	fp     fingerprint
	why    string // cheap refusal computed at open; "" if none
	backed bool   // a backup has been taken for this file, this run

	// The round-trip proof is expensive — an encode plus a decode, so two KDF
	// derivations — and its answer is only needed when somebody actually wants
	// to write. Running it at open made every startup pay for a capability most
	// sessions never use: on a real vault it was three quarters of the open.
	// So it runs once, on demand, and is remembered.
	verified    bool
	verifiedWhy string
}

// String is redacted. A Handle reaches a log or an error message only by
// accident, and it holds the key material for someone's whole vault.
func (h *Handle) String() string { return "vault.Handle[" + h.source + "]" }

// GoString is redacted for the same reason (%#v).
func (h *Handle) GoString() string { return h.String() }

// Path is the kdbx file this handle reads and writes.
func (h *Handle) Path() string { return h.path }

// Source is the configured source name entries are tagged with.
func (h *Handle) Source() string { return h.source }

// Writable is the cheap verdict: the checks that cost nothing, plus the result
// of the round-trip proof if it has already been run.
//
// It has to stay cheap because the UI asks it while drawing — once per source,
// every frame. VerifyWritable is the one that does the real work.
//
// The reason is never swallowed: refusing to write someone's vault without
// saying why is worse than refusing.
func (h *Handle) Writable() (bool, string) {
	if h.why != "" {
		return false, h.why
	}
	if h.verified && h.verifiedWhy != "" {
		return false, h.verifiedWhy
	}
	return true, ""
}

// VerifyWritable proves that a save would lose nothing, by encoding the database
// and comparing the element census of the result against the original.
//
// Called when the user asks to unlock a source for writing — the moment its
// answer matters, and a moment they are already waiting through. The result is
// cached, so every later check is free.
func (h *Handle) VerifyWritable() (bool, string) {
	if h.why != "" {
		return false, h.why
	}
	if h.verified {
		return h.verifiedWhy == "", h.verifiedWhy
	}

	h.verified = true
	lost, err := roundTripLoss(h.db, h.creds)
	switch {
	case err != nil:
		h.verifiedWhy = "could not verify the file would survive a save: " + err.Error()
	case len(lost) > 0:
		h.verifiedWhy = "harmos would lose data it cannot represent: " + strings.Join(lost, ", ")
	}
	return h.verifiedWhy == "", h.verifiedWhy
}

// fingerprint identifies the file contents we opened, so a save can tell that
// something else (KeePassXC, a sync tool) rewrote it in the meantime. Size and
// mtime are the cheap screen; the hash is what actually decides.
type fingerprint struct {
	size  int64
	mtime time.Time
	sum   [32]byte
}

func fingerprintOf(path string) (fingerprint, error) {
	f, err := os.Open(path)
	if err != nil {
		return fingerprint{}, err
	}
	defer func() { _ = f.Close() }()

	st, err := f.Stat()
	if err != nil {
		return fingerprint{}, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fingerprint{}, err
	}
	fp := fingerprint{size: st.Size(), mtime: st.ModTime()}
	copy(fp.sum[:], h.Sum(nil))
	return fp, nil
}

func (a fingerprint) equal(b fingerprint) bool { return a.sum == b.sum && a.size == b.size }

// OpenHandle reads the kdbx at path and keeps everything needed to write it
// back. It opens the file read-only; nothing is written until Save is called.
func OpenHandle(path, source string, creds Credentials) (*Handle, error) {
	dbCreds, err := buildCredentials(creds)
	if err != nil {
		return nil, err
	}
	return openHandle(path, source, dbCreds)
}

// openHandle is OpenHandle once the credentials are built. Reopen enters here
// with the credentials the first open produced — they are sha256 hashes, so a
// handle can re-read its own file without holding, or asking for, a password.
func openHandle(path, source string, dbCreds *gokeepasslib.DBCredentials) (*Handle, error) {
	fp, err := fingerprintOf(path)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path) // O_RDONLY — Save is the only writer
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	db := gokeepasslib.NewDatabase()
	db.Credentials = dbCreds
	if err := decode(f, db); err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := db.UnlockProtectedEntries(); err != nil {
		return nil, fmt.Errorf("unlock %s: %w", path, err)
	}

	h := &Handle{db: db, path: path, source: source, creds: dbCreds, fp: fp}
	h.why = h.refuseWriteBecause()
	return h, nil
}

// decode reads a kdbx, and survives a file that is not one.
//
// The library indexes its own buffer with lengths taken from the file and does
// not check them against it, so a truncated kdbx — an interrupted sync, a full
// disk, a copy that stopped halfway — panics with a slice bounds error rather
// than returning one. Opening a vault must not be able to kill the program: a
// panic here takes the TUI down mid-unlock and prints a stack trace over the
// CLI. Reported upstream; the guard stays regardless, because a decoder fed a
// broken file is exactly where a guard belongs.
func decode(r io.Reader, db *gokeepasslib.Database) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("this file is not a readable kdbx (it may be truncated or corrupt): %v", p)
		}
	}()
	return gokeepasslib.NewDecoder(r).Decode(db)
}

// refuseWriteBecause returns "" when this file is safe for harmos to write, else
// the reason. Each case is a way we would silently damage the file, so the
// honest answer is to refuse rather than to write something lossy.
func (h *Handle) refuseWriteBecause() string {
	hdr := h.db.Header
	if hdr == nil || hdr.Signature == nil {
		return "unrecognised kdbx header"
	}
	if !hdr.IsKdbx4() {
		// KDBX 3.1 needs a different set of regenerated header fields
		// (TransformSeed, StreamStartBytes, ProtectedStreamKey, a 16-byte IV,
		// and the Salsa20 inner stream). Doable, but its own blast radius.
		return "KDBX 3.1 file: harmos can only write KDBX 4.0 — upgrade it in KeePassXC"
	}
	// What the file *contains* decides the rest, not what its version label
	// says — but that check costs an encode and a decode, so it waits for
	// VerifyWritable and the moment somebody actually wants to write.

	if h.db.Content == nil || h.db.Content.Root == nil {
		return "kdbx has no root group"
	}
	if n := len(h.db.Content.Root.Groups); n != 1 {
		// gokeepasslib's binary garbage collector only walks Root.Groups[0], so
		// attachments anywhere else would be collected as unused and dropped.
		return fmt.Sprintf("kdbx has %d root groups: harmos would lose attachments outside the first", n)
	}
	if err := writableOnDisk(h.path); err != nil {
		return err.Error()
	}
	return ""
}

// writableOnDisk checks that we could replace the file: both the file itself and
// the directory the rename happens in.
func writableOnDisk(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("file is not writable")
	}
	_ = f.Close()

	dir := filepath.Dir(path)
	probe, err := os.CreateTemp(dir, ".harmos-probe-*")
	if err != nil {
		return fmt.Errorf("directory %s is not writable", dir)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

// Snapshot is the read-only projection used for browsing, searching and copying.
//
// It is lossy on purpose — see the Entry doc — which is exactly why it is never
// the source of a write. A Handle mutation reads the database directly.
func (h *Handle) Snapshot() *Vault {
	v := &Vault{Source: h.source}
	h.walkTree(
		func(g *gokeepasslib.Group, id, parentID, path string) {
			if parentID == "" {
				return // the root container is not a folder anyone browses
			}
			v.Folders = append(v.Folders, Folder{
				ID:       id,
				ParentID: parentID,
				Source:   h.source,
				Path:     path,
				Name:     oneline(g.Name),
			})
		},
		func(e *gokeepasslib.Entry, id, gid, path string) {
			v.Entries = append(v.Entries, entryFrom(h.db, e, h.source, id, gid, path))
		},
	)
	return v
}

// Reopen re-reads the file behind a handle, with the same credentials.
//
// For after a save, when the caller needs a view of what is now on disk — and
// for recovering from a concurrent modification, where the honest move is to
// re-read rather than to reconcile. The old handle is left alone; the caller
// swaps in the new one.
//
// No password is needed or asked for: DBCredentials holds sha256 hashes, not
// the secrets themselves, so the handle can reopen its own file without ever
// having kept one.
func Reopen(h *Handle) (*Handle, error) {
	fresh, err := openHandle(h.path, h.source, h.creds)
	if err != nil {
		return nil, err
	}
	// "One backup per run" is a promise about the file, not about a Go value.
	// The interface reopens the handle after every save, so a fresh zero value
	// here meant a full copy of the vault beside it on every save, forever —
	// and nothing prunes them.
	fresh.backed = h.backed
	return fresh, nil
}

// DisableRecycleBinForTest switches the database's recycle bin off.
//
// Exported for tests in other packages that need to exercise the
// bin-is-off path, where a "move to bin" delete is really permanent. Nothing in
// the program calls it: the setting belongs to the file's owner.
func (h *Handle) DisableRecycleBinForTest() {
	if h.db.Content != nil && h.db.Content.Meta != nil {
		h.db.Content.Meta.RecycleBinEnabled = w.NewBoolWrapper(false)
	}
}
