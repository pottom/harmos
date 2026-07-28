package vault

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"

	"github.com/pottom/harmos/internal/atomicfile"
)

// ErrChangedUnderneath means the file was rewritten by something else since we
// opened it. Saving would silently discard whatever that other writer did, so we
// refuse and leave the caller's staged changes intact — the decision to reload
// or to overwrite belongs to the user, not to us.
var ErrChangedUnderneath = errors.New("the file changed on disk since it was opened")

// ErrNotWritable means this file is one harmos declines to write. Handle.Writable
// carries the reason.
var ErrNotWritable = errors.New("this source is not writable")

// Save writes the database back to its file.
//
// Every step below is defence for someone's primary password vault, in the order
// that matters:
//
//  1. re-check the fingerprint, so we do not clobber another writer
//  2. back the original up once per session, before it can be replaced
//  3. regenerate the nonces, so two saves never reuse a keystream
//  4. lock once (Encode wants a locked database — see Handle)
//  5. encode into a temp file in the same directory and flush it
//  6. decode the temp file back before the rename — a corrupt write must never
//     become the user's vault
//  7. compare the attachment census, in case the encoder dropped binaries
//  8. rename into place, then flush the directory entry
//  9. unlock again, so the handle stays usable, and re-fingerprint
func (h *Handle) Save() (err error) {
	// The full proof, not the cheap verdict: this is the last moment before the
	// file is replaced, and it is cached from the unlock anyway.
	if ok, why := h.VerifyWritable(); !ok {
		return fmt.Errorf("%w: %s", ErrNotWritable, why)
	}

	current, err := fingerprintOf(h.path)
	if err != nil {
		return err
	}
	if !current.equal(h.fp) {
		return ErrChangedUnderneath
	}

	if err := h.backupOnce(); err != nil {
		return fmt.Errorf("backup: %w", err)
	}

	// Rekey while the values are still plaintext: Lock below re-encrypts them
	// under the new inner stream key.
	if err := Rekey(h.db); err != nil {
		return err
	}

	before := attachmentCensus(h.db)

	if err := h.db.LockProtectedEntries(); err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	// From here the database is locked. Encode leaves it locked too, so unlock
	// on every path out or the handle's protected values become unreadable.
	defer func() {
		if uerr := h.db.UnlockProtectedEntries(); uerr != nil && err == nil {
			err = fmt.Errorf("unlock after save: %w", uerr)
		}
	}()

	err = atomicfile.Write(h.path, 0o600, func(f *os.File) error {
		if encErr := gokeepasslib.NewEncoder(f).Encode(h.db); encErr != nil {
			return fmt.Errorf("encode: %w", encErr)
		}
		// Verify before the rename. atomicfile has not moved anything yet, so a
		// failure here leaves the original file exactly as it was.
		return h.verify(f.Name(), before)
	})
	if err != nil {
		return err
	}

	fp, err := fingerprintOf(h.path)
	if err != nil {
		return err
	}
	h.fp = fp
	return nil
}

// verify decodes the just-written file with the same credentials and checks that
// it still holds what we meant to write. Costs one Argon2 derivation, which is
// the right price on an explicit save.
func (h *Handle) verify(path string, before map[gokeepasslib.UUID][]string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	defer func() { _ = f.Close() }()

	db := gokeepasslib.NewDatabase()
	db.Credentials = h.creds
	if err := gokeepasslib.NewDecoder(f).Decode(db); err != nil {
		return fmt.Errorf("verify: written file does not decode: %w", err)
	}
	if err := db.UnlockProtectedEntries(); err != nil {
		return fmt.Errorf("verify: written file does not unlock: %w", err)
	}

	if got, want := countEntries(db), countEntries(h.db); got != want {
		return fmt.Errorf("verify: wrote %d entries, read back %d", want, got)
	}
	if err := censusEqual(before, attachmentCensus(db)); err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	return nil
}

// backupOnce copies the original file aside the first time a session saves it.
// Once per session, not per save: a backup per keystroke-batch would litter the
// user's vault directory. It is an encrypted kdbx, so it is no more sensitive
// than the file it copies, and it lives beside it rather than in a temp dir.
// harmos never deletes one.
func (h *Handle) backupOnce() error {
	if h.backed {
		return nil
	}
	dst := fmt.Sprintf("%s.harmos-backup-%s.kdbx", h.path, time.Now().UTC().Format("20060102T150405Z"))

	src, err := os.Open(h.path)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	if err := atomicfile.Write(dst, 0o600, func(f *os.File) error {
		_, cerr := io.Copy(f, src)
		return cerr
	}); err != nil {
		return err
	}
	h.backed = true
	return nil
}

// BackupPath is the file backupOnce would create next, so the save confirmation
// can name it before the user commits. It returns "" once a backup exists.
func (h *Handle) BackupPath() string {
	if h.backed {
		return ""
	}
	return fmt.Sprintf("%s.harmos-backup-%s.kdbx", h.path, time.Now().UTC().Format("20060102T150405Z"))
}

// attachmentCensus maps each entry UUID to its attachment names, sorted.
//
// This is the second line of defence against gokeepasslib's binary collector,
// which only walks Root.Groups[0]: anything it considers unused is dropped from
// the file. The root-group gate in refuseWriteBecause should make that
// impossible, but losing a user's attachments is not a failure worth trusting a
// single check with.
func attachmentCensus(db *gokeepasslib.Database) map[gokeepasslib.UUID][]string {
	out := map[gokeepasslib.UUID][]string{}
	if db.Content == nil || db.Content.Root == nil {
		return out
	}
	var walk func(g gokeepasslib.Group)
	walk = func(g gokeepasslib.Group) {
		for i := range g.Entries {
			e := &g.Entries[i]
			if len(e.Binaries) == 0 {
				continue
			}
			names := make([]string, 0, len(e.Binaries))
			for _, b := range e.Binaries {
				names = append(names, b.Name)
			}
			sort.Strings(names)
			out[e.UUID] = names
		}
		for _, sub := range g.Groups {
			walk(sub)
		}
	}
	for _, g := range db.Content.Root.Groups {
		walk(g)
	}
	return out
}

func censusEqual(before, after map[gokeepasslib.UUID][]string) error {
	for uuid, want := range before {
		got, ok := after[uuid]
		if !ok {
			return fmt.Errorf("entry %x lost all %d attachment(s)", uuid, len(want))
		}
		if len(got) != len(want) {
			return fmt.Errorf("entry %x had %d attachment(s), now %d", uuid, len(want), len(got))
		}
		for i := range want {
			if got[i] != want[i] {
				return fmt.Errorf("entry %x attachment %q became %q", uuid, want[i], got[i])
			}
		}
	}
	return nil
}

func countEntries(db *gokeepasslib.Database) int {
	if db.Content == nil || db.Content.Root == nil {
		return 0
	}
	n := 0
	var walk func(g gokeepasslib.Group)
	walk = func(g gokeepasslib.Group) {
		n += len(g.Entries)
		for _, sub := range g.Groups {
			walk(sub)
		}
	}
	for _, g := range db.Content.Root.Groups {
		walk(g)
	}
	return n
}
