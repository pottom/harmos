package vault

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
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
	why    string // "" when writable, else why not
	backed bool   // a backup has been taken this session
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

// Writable reports whether Save may run, and when it may not, the reason to show
// the user. The reason is never swallowed: refusing to write someone's vault
// without saying why is worse than refusing.
func (h *Handle) Writable() (bool, string) { return h.why == "", h.why }

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
	if err := gokeepasslib.NewDecoder(f).Decode(db); err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := db.UnlockProtectedEntries(); err != nil {
		return nil, fmt.Errorf("unlock %s: %w", path, err)
	}

	h := &Handle{db: db, path: path, source: source, creds: dbCreds, fp: fp}
	h.why = h.refuseWriteBecause()
	return h, nil
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
	if hdr.Signature.MinorVersion >= 1 {
		// The minor version round-trips verbatim while gokeepasslib models none
		// of the 4.1-only elements (PreviousParentGroup, QualityCheck,
		// Group.Tags, Group.CustomData) and drops unknown XML silently. Saving
		// would strip the user's data while still claiming to be 4.1.
		return "KDBX 4.1 file: harmos cannot preserve 4.1-only fields, so it will not write it"
	}
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
	// The top-level group is the database root container; its own name is not
	// part of entry paths (the source name provides the top-level identity).
	for _, g := range h.db.Content.Root.Groups {
		v.walk(h.db, g, "")
	}
	return v
}
