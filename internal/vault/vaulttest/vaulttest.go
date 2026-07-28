// Package vaulttest builds kdbx files for tests.
//
// It exists because three packages had grown their own near-identical fixture
// builder (vault, session, source/localkdbx), and the write path needs several
// more shapes — a KDBX 3.1 file, a 4.1 file, one with several root groups — that
// nobody wants to hand-roll a fourth time.
//
// It deliberately does not import internal/vault: the vault package's own tests
// live in package vault and would not be able to use it otherwise.
package vaulttest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

// Val is an ordinary (unprotected) entry field.
func Val(k, v string) gokeepasslib.ValueData {
	return gokeepasslib.ValueData{Key: k, Value: gokeepasslib.V{Content: v}}
}

// PVal is a protected entry field — the ones that are encrypted in the file and
// masked in the UI.
func PVal(k, v string) gokeepasslib.ValueData {
	return gokeepasslib.ValueData{Key: k, Value: gokeepasslib.V{Content: v, Protected: w.NewBoolWrapper(true)}}
}

// Option customises a fixture.
type Option func(*builder)

type builder struct {
	creds     *gokeepasslib.DBCredentials
	kdbx31    bool
	minor     uint16
	extraRoot bool
	title     string
	shape     func(db *gokeepasslib.Database) []gokeepasslib.Group
}

// WithCredentials sets the composite key. Defaults to the password "pw".
func WithCredentials(c *gokeepasslib.DBCredentials) Option {
	return func(b *builder) { b.creds = c }
}

// WithPassword is the common case of WithCredentials.
func WithPassword(pw string) Option {
	return func(b *builder) { b.creds = gokeepasslib.NewPasswordCredentials(pw) }
}

// WithTitle renames the single entry in the simple fixture.
func WithTitle(title string) Option {
	return func(b *builder) { b.title = title }
}

// KDBX31 writes a KDBX 3.1 file instead of 4.0 — harmos refuses to write these,
// and the refusal needs something to refuse.
func KDBX31() Option {
	return func(b *builder) { b.kdbx31 = true }
}

// MinorVersion stamps the header's minor version. Use MinorVersion(1) to make a
// 4.1 file: the field round-trips verbatim through gokeepasslib even though it
// models none of the 4.1-only elements, which is exactly why harmos refuses it.
func MinorVersion(v uint16) Option {
	return func(b *builder) { b.minor = v }
}

// ExtraRootGroup adds a second top-level group. gokeepasslib's binary collector
// only walks the first one, so attachments outside it are dropped on save —
// harmos refuses these files.
func ExtraRootGroup() Option {
	return func(b *builder) { b.extraRoot = true }
}

// Shape replaces the fixture's content entirely, for tests that need a specific
// tree. It is handed the database so it can call AddBinary.
func Shape(fn func(db *gokeepasslib.Database) []gokeepasslib.Group) Option {
	return func(b *builder) { b.shape = fn }
}

// Write builds a kdbx at path and returns it.
func Write(t testing.TB, path string, opts ...Option) string {
	t.Helper()

	b := &builder{creds: gokeepasslib.NewPasswordCredentials("pw"), title: "db-prod"}
	for _, o := range opts {
		o(b)
	}

	version := gokeepasslib.WithDatabaseKDBXVersion4()
	if b.kdbx31 {
		version = gokeepasslib.WithDatabaseKDBXVersion3()
	}
	db := gokeepasslib.NewDatabase(version)
	db.Credentials = b.creds

	groups := b.simple(db)
	if b.shape != nil {
		groups = b.shape(db)
	}
	if b.extraRoot {
		second := gokeepasslib.NewGroup()
		second.Name = "Second Root"
		groups = append(groups, second)
	}
	db.Content.Root = &gokeepasslib.RootData{Groups: groups}

	if b.minor != 0 && db.Header != nil && db.Header.Signature != nil {
		// gokeepasslib points every new header at the *same* package-level
		// signature variable (header.go: Signature: &DefaultKDBX4Sig), so writing
		// through the pointer would stamp this minor version on every database
		// built afterwards in the process — including the ones other tests expect
		// to be 4.0. Swap in a copy.
		sig := *db.Header.Signature
		sig.MinorVersion = b.minor
		db.Header.Signature = &sig
	}

	if err := db.LockProtectedEntries(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := gokeepasslib.NewEncoder(f).Encode(db); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// simple is the standard fixture: Root/Infra/<title>, exercising every field
// kind the reader projects — protected and unprotected, a TOTP URI, a prefixed
// custom field, an internal field that must stay hidden, tags, and an
// attachment.
func (b *builder) simple(db *gokeepasslib.Database) []gokeepasslib.Group {
	e := gokeepasslib.NewEntry()
	e.Values = append(e.Values,
		Val("Title", b.title),
		Val("UserName", "svc"),
		PVal("Password", "secret-pw"),
		Val("URL", "https://db"),
		PVal("otp", "otpauth://totp/db-prod?secret=GEZDGNBVGY3TQOJQ&digits=6&period=30"),
		Val("Notes", "primary box\nrotate quarterly"),
		Val("pps.cuf.Environment", "prod"), // custom field → "Environment"
		PVal("Recovery", "r3c0v3ry"),       // protected custom field
		Val("pps.Id", "srv-123"),           // internal — must be hidden
	)
	e.Tags = "infra;prod"
	e.Times = gokeepasslib.NewTimeData()
	e.Times.LastModificationTime = &w.TimeWrapper{Time: time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)}
	bin := db.AddBinary([]byte("attachment-bytes"))
	e.Binaries = append(e.Binaries, bin.CreateReference("notes.txt"))

	infra := gokeepasslib.NewGroup()
	infra.Name = "Infra"
	infra.Entries = []gokeepasslib.Entry{e}

	root := gokeepasslib.NewGroup()
	root.Name = "Root"
	root.Groups = []gokeepasslib.Group{infra}
	return []gokeepasslib.Group{root}
}

// Keyfile writes a synthetic keyfile in a temp dir and returns its path.
func Keyfile(t testing.TB) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "key.keyx")
	if err := os.WriteFile(p, []byte("synthetic-keyfile-content-0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// KeyData reads a keyfile's bytes.
//
// Use the *DataCredentials constructors in tests rather than gokeepasslib's
// path-based ones: those leak the file handle, which blocks temp-dir cleanup on
// Windows.
func KeyData(t testing.TB, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
