package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"

	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault/vaulttest"
)

func pw(s string) Credentials { return Credentials{Password: secret.New(s)} }

func openFixture(t *testing.T, opts ...vaulttest.Option) (*Handle, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "v.kdbx")
	vaulttest.Write(t, p, opts...)
	h, err := OpenHandle(p, "s", pw("pw"))
	if err != nil {
		t.Fatal(err)
	}
	return h, p
}

// The reason Rekey exists. gokeepasslib writes the decoded file's MasterSeed,
// EncryptionIV and KDF salt straight back out, so without Rekey two saves of the
// same vault would encrypt different plaintexts under the same key and nonce —
// with ChaCha20 that is keystream reuse, and it is recoverable by anyone holding
// both files. This test fails loudly if Rekey is ever dropped or reordered.
func TestSaveRegeneratesNonces(t *testing.T) {
	h, p := openFixture(t)

	headers := func() (seed, iv, salt, irs []byte) {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		db := gokeepasslib.NewDatabase()
		db.Credentials = gokeepasslib.NewPasswordCredentials("pw")
		if err := gokeepasslib.NewDecoder(f).Decode(db); err != nil {
			t.Fatal(err)
		}
		fh := db.Header.FileHeaders
		salt = append(salt, fh.KdfParameters.Salt[:]...)
		return fh.MasterSeed, fh.EncryptionIV, salt, db.Content.InnerHeader.InnerRandomStreamKey
	}

	seed0, iv0, salt0, irs0 := headers()
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	seed1, iv1, salt1, irs1 := headers()
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	seed2, iv2, salt2, irs2 := headers()

	for _, c := range []struct {
		name    string
		a, b, c []byte
	}{
		{"master seed", seed0, seed1, seed2},
		{"encryption IV", iv0, iv1, iv2},
		{"kdf salt", salt0, salt1, salt2},
		{"inner random stream key", irs0, irs1, irs2},
	} {
		if bytes.Equal(c.a, c.b) || bytes.Equal(c.b, c.c) || bytes.Equal(c.a, c.c) {
			t.Errorf("%s repeated across saves — keystream reuse", c.name)
		}
		if len(c.b) == 0 {
			t.Errorf("%s is empty after a save", c.name)
		}
	}
	// Length is part of the format: ChaCha20 wants a 12-byte IV, and handing it a
	// 32-byte one would produce a file nothing can open.
	if len(iv0) != len(iv1) || len(iv1) != len(iv2) {
		t.Errorf("IV length changed: %d, %d, %d", len(iv0), len(iv1), len(iv2))
	}
}

// Saving without touching anything must preserve every field the reader projects
// — this is the floor the mutation PRs build on.
func TestSaveWithNoChangesRoundTrips(t *testing.T) {
	h, p := openFixture(t)
	before := h.Snapshot()

	raw0, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	raw1, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(raw0, raw1) {
		t.Error("bytes identical after save — the nonces cannot have been regenerated")
	}

	after, err := Open(p, "s", pw("pw"))
	if err != nil {
		t.Fatalf("reopen after save: %v", err)
	}
	if len(after.Entries) != len(before.Entries) {
		t.Fatalf("entries %d -> %d", len(before.Entries), len(after.Entries))
	}
	a, b := before.Entries[0], after.Entries[0]
	if a.Title != b.Title || a.Username != b.Username || a.URL != b.URL || a.Path != b.Path {
		t.Errorf("standard fields changed:\n%+v\n%+v", a, b)
	}
	if a.Password.Reveal() != b.Password.Reveal() {
		t.Error("password did not survive the save")
	}
	if a.TOTP != b.TOTP {
		t.Errorf("totp changed: %q -> %q", a.TOTP, b.TOTP)
	}
	if a.Notes != b.Notes {
		t.Errorf("notes changed: %q -> %q", a.Notes, b.Notes)
	}
	if len(a.Custom) != len(b.Custom) {
		t.Errorf("custom fields %d -> %d", len(a.Custom), len(b.Custom))
	}
	for i := range a.Custom {
		if a.Custom[i] != b.Custom[i] {
			t.Errorf("custom field changed: %+v -> %+v", a.Custom[i], b.Custom[i])
		}
	}
	if len(b.Files) != 1 || b.Files[0].Name != "notes.txt" || string(b.Files[0].Data) != "attachment-bytes" {
		t.Errorf("attachment did not survive: %+v", b.Files)
	}
	if !a.Modified.Equal(b.Modified) {
		t.Errorf("modification time rewritten by a no-op save: %v -> %v", a.Modified, b.Modified)
	}
	if len(a.Tags) != len(b.Tags) {
		t.Errorf("tags %v -> %v", a.Tags, b.Tags)
	}
}

// The KDF cost is the file owner's choice. The Pleasant cache deliberately
// overrides it (that cache is ours); a user's vault must come back untouched.
func TestKdfCostPreserved(t *testing.T) {
	h, p := openFixture(t)
	kdf := h.db.Header.FileHeaders.KdfParameters
	kdf.Memory, kdf.Iterations, kdf.Parallelism = 64*1024*1024, 5, 4

	if err := h.Save(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials("pw")
	if err := gokeepasslib.NewDecoder(f).Decode(db); err != nil {
		t.Fatal(err)
	}
	got := db.Header.FileHeaders.KdfParameters
	if got.Memory != 64*1024*1024 || got.Iterations != 5 || got.Parallelism != 4 {
		t.Errorf("kdf cost rewritten: memory=%d iterations=%d parallelism=%d",
			got.Memory, got.Iterations, got.Parallelism)
	}
}

// Each refusal is a file harmos would silently damage. Writing nothing is the
// correct behaviour, and the reason has to reach the user.
func TestRefusesFilesItWouldDamage(t *testing.T) {
	cases := []struct {
		name string
		opts []vaulttest.Option
		want string
	}{
		{"kdbx 3.1", []vaulttest.Option{vaulttest.KDBX31()}, "KDBX 3.1"},
		{"several root groups", []vaulttest.Option{vaulttest.ExtraRootGroup()}, "root groups"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, p := openFixture(t, c.opts...)

			ok, why := h.Writable()
			if ok {
				t.Fatal("should not be writable")
			}
			if !strings.Contains(why, c.want) {
				t.Errorf("reason %q should mention %q", why, c.want)
			}

			before, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			err = h.Save()
			if !errors.Is(err, ErrNotWritable) {
				t.Fatalf("Save error = %v, want ErrNotWritable", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("Save error %q should carry the reason", err)
			}
			after, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Error("a refused save still wrote to the file")
			}
		})
	}
}

// If something else rewrote the file while we had it open, saving would throw
// their work away without either of us noticing. Refuse, and keep the caller's
// staged changes so they can still be reviewed or re-applied.
func TestSaveRefusesWhenFileChangedUnderneath(t *testing.T) {
	h, p := openFixture(t)

	// Another writer replaces the file. A different password proves we compare
	// contents rather than trusting the handle's own view.
	vaulttest.Write(t, p, vaulttest.WithPassword("other"), vaulttest.WithTitle("theirs"))
	theirs, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}

	if err := h.Save(); !errors.Is(err, ErrChangedUnderneath) {
		t.Fatalf("Save error = %v, want ErrChangedUnderneath", err)
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(theirs, after) {
		t.Error("the other writer's file was overwritten")
	}
}

// The backup is insurance taken once per session, not per keystroke — a backup
// per save would litter the user's vault directory.
func TestBackupIsWrittenOnce(t *testing.T) {
	h, p := openFixture(t)
	original, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}

	announced := h.BackupPath()
	if !strings.Contains(announced, ".harmos-backup-") {
		t.Errorf("BackupPath = %q, should be announceable before saving", announced)
	}

	for range 3 {
		if err := h.Save(); err != nil {
			t.Fatal(err)
		}
	}

	backups, err := filepath.Glob(p + ".harmos-backup-*.kdbx")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("got %d backups after three saves, want 1: %v", len(backups), backups)
	}
	got, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, got) {
		t.Error("the backup is not a copy of the file as it was before the first save")
	}
	if h.BackupPath() != "" {
		t.Error("BackupPath should be empty once a backup exists")
	}
	st, err := os.Stat(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no POSIX mode bits (see internal/atomicfile).
	if perm := st.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o600 {
		t.Errorf("backup mode = %o, want 600 — it holds the same secrets", perm)
	}
}

// Verify is what stops a corrupt encode from becoming someone's vault. It runs
// before the rename, so a failure leaves the original in place.
func TestVerifyRejectsAnUnreadableFile(t *testing.T) {
	h, _ := openFixture(t)

	corrupt := filepath.Join(t.TempDir(), "corrupt.kdbx")
	if err := os.WriteFile(corrupt, []byte("not a kdbx at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.verify(corrupt, nil); err == nil {
		t.Fatal("verify accepted a file that does not decode")
	}

	// A file that decodes but holds the wrong thing must also be caught.
	other := filepath.Join(t.TempDir(), "other.kdbx")
	vaulttest.Write(t, other, vaulttest.Shape(func(*gokeepasslib.Database) []gokeepasslib.Group {
		g := gokeepasslib.NewGroup()
		g.Name = "Root"
		return []gokeepasslib.Group{g}
	}))
	if err := h.verify(other, nil); err == nil {
		t.Fatal("verify accepted a file with the wrong entry count")
	}
}

// Losing an attachment is silent and permanent, so the census is checked on
// every save even though the root-group gate should already prevent it.
func TestVerifyCatchesLostAttachments(t *testing.T) {
	h, _ := openFixture(t)

	before := attachmentCensus(h.db)
	if len(before) != 1 {
		t.Fatalf("fixture should have one entry with attachments, got %d", len(before))
	}
	if err := censusEqual(before, map[gokeepasslib.UUID][]string{}); err == nil {
		t.Error("a census that lost every attachment should not compare equal")
	}
	partial := map[gokeepasslib.UUID][]string{}
	for k := range before {
		partial[k] = []string{"renamed.txt"}
	}
	if err := censusEqual(before, partial); err == nil {
		t.Error("a renamed attachment should not compare equal")
	}
	if err := censusEqual(before, before); err != nil {
		t.Errorf("a census should equal itself: %v", err)
	}
}

// A save must land in one step: a reader either sees the old file or the new one.
func TestSaveLeavesNoTempFiles(t *testing.T) {
	h, p := openFixture(t)
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".harmos-") && strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

// A handle stays usable after saving: Encode leaves the database locked, and
// forgetting to unlock turns every protected value into ciphertext in the UI.
func TestHandleStaysReadableAfterSave(t *testing.T) {
	h, _ := openFixture(t)
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	got := h.Snapshot()
	if len(got.Entries) != 1 {
		t.Fatalf("entries = %d", len(got.Entries))
	}
	if got.Entries[0].Password.Reveal() != "secret-pw" {
		t.Error("protected values unreadable after a save — the lock/unlock pairing is wrong")
	}
	// And again, to catch a discipline that only survives one round trip.
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	if h.Snapshot().Entries[0].Password.Reveal() != "secret-pw" {
		t.Error("protected values unreadable after a second save")
	}
}

// A Handle holds the key material for a whole vault; it must never print.
func TestHandleIsRedacted(t *testing.T) {
	h, _ := openFixture(t)
	for _, s := range []string{h.String(), h.GoString()} {
		if strings.Contains(s, "pw") || strings.Contains(s, "secret") {
			t.Errorf("handle rendering leaks: %q", s)
		}
	}
}

func TestWritableReportsPathAndSource(t *testing.T) {
	h, p := openFixture(t)
	if h.Path() != p {
		t.Errorf("Path = %q, want %q", h.Path(), p)
	}
	if h.Source() != "s" {
		t.Errorf("Source = %q, want s", h.Source())
	}
}

// Rekey runs while values are plaintext and must not disturb them.
func TestRekeyNeedsHeaders(t *testing.T) {
	if err := Rekey(gokeepasslib.NewDatabase()); err == nil {
		_ = err // a fresh database does have headers; the guard is for decoded ones
	}
	db := &gokeepasslib.Database{}
	if err := Rekey(db); err == nil {
		t.Error("Rekey should refuse a database with no headers rather than write a broken file")
	}
}

// A read-only file on disk cannot be saved, and the reason says so rather than
// failing later with a bare permission error.
func TestRefusesReadOnlyFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: file modes are not enforced")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "ro.kdbx")
	vaulttest.Write(t, p)
	if err := os.Chmod(p, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o600) })

	h, err := OpenHandle(p, "s", pw("pw"))
	if err != nil {
		t.Fatal(err)
	}
	ok, why := h.Writable()
	if ok {
		t.Fatal("a read-only file should not be writable")
	}
	if !strings.Contains(why, "not writable") {
		t.Errorf("reason = %q", why)
	}
}

// The fingerprint compares contents, so a touch alone is not a conflict.
func TestFingerprintIsContentBased(t *testing.T) {
	h, p := openFixture(t)
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	if err := h.Save(); err != nil {
		t.Fatalf("an mtime-only change is not a conflict: %v", err)
	}
}

// A 4.1 label is not a reason to refuse. This is the case the version-based gate
// got wrong: real vaults are 4.1, and one that uses no element we cannot keep is
// perfectly safe to write. The gate asks what the file contains instead.
func TestWritesKdbx41WhenNothingWouldBeLost(t *testing.T) {
	h, p := openFixture(t, vaulttest.MinorVersion(1))

	if ok, why := h.Writable(); !ok {
		t.Fatalf("a 4.1 file that loses nothing should be writable, refused with: %s", why)
	}
	if err := h.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	after, err := Open(p, "s", pw("pw"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if len(after.Entries) != 1 || after.Entries[0].Password.Reveal() != "secret-pw" {
		t.Errorf("contents did not survive: %+v", after.Entries)
	}
}

// The gate's decision function, on its own. An element that survives in fewer
// copies counts as lost just as much as one that vanishes: dropping one of three
// attachments is not a partial success.
func TestCensusDetectsLoss(t *testing.T) {
	before := census{"Group>Tags": 2, "Entry>String": 5, "Group>UUID": 1}

	if lost := before.lostSince(census{"Group>Tags": 2, "Entry>String": 5, "Group>UUID": 1}); len(lost) != 0 {
		t.Errorf("an identical census should report no loss, got %v", lost)
	}
	lost := before.lostSince(census{"Entry>String": 5, "Group>UUID": 1})
	if len(lost) != 1 || !strings.Contains(lost[0], "Group>Tags") {
		t.Errorf("a vanished element should be reported, got %v", lost)
	}
	lost = before.lostSince(census{"Group>Tags": 2, "Entry>String": 4, "Group>UUID": 1})
	if len(lost) != 1 || !strings.Contains(lost[0], "Entry>String") {
		t.Errorf("a thinned element should be reported, got %v", lost)
	}
	if lost := before.lostSince(census{"Group>Tags": 9, "Entry>String": 5, "Group>UUID": 1}); len(lost) != 0 {
		t.Errorf("gaining elements is not loss, got %v", lost)
	}
}

// The census is derived from the decrypted XML, so it must see what is actually
// in the file rather than what the structs happen to model.
func TestCensusSeesTheRealDocument(t *testing.T) {
	h, _ := openFixture(t)

	c, err := censusOf(h.db)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"KeePassFile>Meta", "KeePassFile>Root", "Group>Entry", "Entry>String"} {
		if c[want] == 0 {
			t.Errorf("census is missing %s: %v", want, c)
		}
	}
	if c["Entry>String"] < 5 {
		t.Errorf("the fixture entry has several fields, census says %d", c["Entry>String"])
	}
}
