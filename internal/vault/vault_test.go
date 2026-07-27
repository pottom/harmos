package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"

	"github.com/pottom/harmos/internal/secret"
)

func val(k, v string) gokeepasslib.ValueData {
	return gokeepasslib.ValueData{Key: k, Value: gokeepasslib.V{Content: v}}
}
func pval(k, v string) gokeepasslib.ValueData {
	return gokeepasslib.ValueData{Key: k, Value: gokeepasslib.V{Content: v, Protected: w.NewBoolWrapper(true)}}
}

// makeKDBX writes a small KDBX4 file (Root/Infra/db-prod) with the given creds.
func makeKDBX(t *testing.T, path string, creds *gokeepasslib.DBCredentials) {
	t.Helper()
	db := gokeepasslib.NewDatabase(gokeepasslib.WithDatabaseKDBXVersion4())
	db.Credentials = creds
	e := gokeepasslib.NewEntry()
	e.Values = append(e.Values,
		val("Title", "db-prod"),
		val("UserName", "svc"),
		pval("Password", "secret-pw"),
		val("URL", "https://db"),
		pval("otp", "otpauth://totp/db-prod?secret=GEZDGNBVGY3TQOJQ&digits=6&period=30"),
		val("Notes", "primary box\nrotate quarterly"),
		val("pps.cuf.Environment", "prod"), // custom field → "Environment"
		pval("Recovery", "r3c0v3ry"),       // protected custom field
		val("pps.Id", "srv-123"),           // internal — must be hidden
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
	db.Content.Root = &gokeepasslib.RootData{Groups: []gokeepasslib.Group{root}}
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
}

func makeKeyfile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "key.keyx")
	if err := os.WriteFile(p, []byte("synthetic-keyfile-content-0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// keyData reads a keyfile's bytes. Use the *DataCredentials constructors in
// tests too: gokeepasslib's path-based constructors leak the file handle, which
// blocks temp-dir cleanup on Windows.
func keyData(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func assertOneEntry(t *testing.T, v *Vault) {
	t.Helper()
	if len(v.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(v.Entries))
	}
	e := v.Entries[0]
	if e.Title != "db-prod" || e.Username != "svc" || e.URL != "https://db" {
		t.Errorf("fields wrong: %+v", e)
	}
	if e.Path != "Infra" {
		t.Errorf("path = %q, want Infra (root group name dropped)", e.Path)
	}
	if e.Password.Reveal() != "secret-pw" {
		t.Error("password did not round-trip")
	}
	if !strings.HasPrefix(e.TOTP, "otpauth://totp/") {
		t.Errorf("otp field not read into TOTP: %q", e.TOTP)
	}
	if !strings.Contains(e.Notes, "rotate quarterly") {
		t.Errorf("Notes field not read: %q", e.Notes)
	}
	// custom fields: pps.cuf.Environment surfaces as "Environment"; the protected
	// Recovery field keeps its flag; pps.Id and the standard fields are hidden.
	var envOK, recOK bool
	for _, f := range e.Custom {
		switch f.Name {
		case "pps.Id", "Title", "Password", "otp":
			t.Errorf("internal/standard field %q leaked into Custom", f.Name)
		case "Environment":
			envOK = f.Value == "prod"
		case "Recovery":
			recOK = f.Protected && f.Value == "r3c0v3ry"
		}
	}
	if !envOK {
		t.Error("pps.cuf.Environment should surface as a custom field named Environment")
	}
	if !recOK {
		t.Error("protected Recovery field should be a protected custom field")
	}
	if e.Modified.IsZero() || e.Modified.Year() != 2026 {
		t.Errorf("modified time not read: %v", e.Modified)
	}
	if len(e.Files) != 1 || e.Files[0].Name != "notes.txt" || string(e.Files[0].Data) != "attachment-bytes" {
		t.Errorf("attachment not read: %+v", e.Files)
	}
	if len(e.Tags) != 2 {
		t.Errorf("tags = %v, want 2", e.Tags)
	}
}

func TestOpenPasswordOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pw.kdbx")
	makeKDBX(t, p, gokeepasslib.NewPasswordCredentials("pw"))

	v, err := Open(p, "work", Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	if v.Source != "work" || v.Entries[0].Source != "work" {
		t.Error("source not tagged")
	}
	assertOneEntry(t, v)
}

func TestOpenKeyfileOnly(t *testing.T) {
	key := makeKeyfile(t)
	creds, err := gokeepasslib.NewKeyDataCredentials(keyData(t, key))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "key.kdbx")
	makeKDBX(t, p, creds)

	v, err := Open(p, "k", Credentials{Keyfile: key})
	if err != nil {
		t.Fatal(err)
	}
	assertOneEntry(t, v)
}

func TestOpenPasswordAndKeyfile(t *testing.T) {
	key := makeKeyfile(t)
	creds, err := gokeepasslib.NewPasswordAndKeyDataCredentials("pw", keyData(t, key))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "both.kdbx")
	makeKDBX(t, p, creds)

	// opens with BOTH
	v, err := Open(p, "both", Credentials{Password: secret.New("pw"), Keyfile: key})
	if err != nil {
		t.Fatal(err)
	}
	assertOneEntry(t, v)

	// must NOT open with the password alone
	if _, err := Open(p, "both", Credentials{Password: secret.New("pw")}); err == nil {
		t.Error("a composite-key file must not open with the password alone")
	}
}

// The core M3 invariant: reading an external kdbx never modifies it.
func TestReadOnlyInvariant(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ro.kdbx")
	makeKDBX(t, p, gokeepasslib.NewPasswordCredentials("pw"))

	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, _ := os.Stat(p)

	v, err := Open(p, "s", Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range v.Entries { // touch every field, including secrets
		_ = e.Password.Reveal()
	}

	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, _ := os.Stat(p)

	if !bytes.Equal(before, after) {
		t.Error("file bytes changed after a read session")
	}
	if !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Errorf("mtime changed: %v -> %v", beforeInfo.ModTime(), afterInfo.ModTime())
	}
	// no KeePass lock file created beside it
	if _, err := os.Stat(p + ".lock"); !os.IsNotExist(err) {
		t.Error("a .lock file was created next to the source")
	}
}

// A folder name (or title) with an embedded newline must be flattened, or it
// breaks the single-line display in the TUI.
func TestOnelineSanitizes(t *testing.T) {
	db := gokeepasslib.NewDatabase(gokeepasslib.WithDatabaseKDBXVersion4())
	db.Credentials = gokeepasslib.NewPasswordCredentials("pw")
	e := gokeepasslib.NewEntry()
	e.Values = append(e.Values, val("Title", "line1\nline2"), val("UserName", "u\tv"))
	sub := gokeepasslib.NewGroup()
	sub.Name = "Naplo\nNEBIH" // a Pleasant folder name with a newline
	sub.Entries = []gokeepasslib.Entry{e}
	root := gokeepasslib.NewGroup()
	root.Name = "Root"
	root.Groups = []gokeepasslib.Group{sub}
	db.Content.Root = &gokeepasslib.RootData{Groups: []gokeepasslib.Group{root}}
	if err := db.LockProtectedEntries(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "nl.kdbx")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := gokeepasslib.NewEncoder(f).Encode(db); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	v, err := Open(p, "s", Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Entries) != 1 {
		t.Fatalf("entries = %d", len(v.Entries))
	}
	e0 := v.Entries[0]
	for name, s := range map[string]string{"path": e0.Path, "title": e0.Title, "user": e0.Username} {
		if strings.ContainsAny(s, "\n\r\t") {
			t.Errorf("%s must be single-line, got %q", name, s)
		}
	}
}

func TestOpenErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "e.kdbx")
	makeKDBX(t, p, gokeepasslib.NewPasswordCredentials("pw"))

	if _, err := Open(p, "s", Credentials{}); err == nil {
		t.Error("no credentials should error")
	}
	if _, err := Open(p, "s", Credentials{Password: secret.New("wrong")}); err == nil {
		t.Error("wrong password should error")
	}
	if _, err := Open(filepath.Join(t.TempDir(), "missing.kdbx"), "s", Credentials{Password: secret.New("x")}); err == nil {
		t.Error("missing file should error")
	}
}
