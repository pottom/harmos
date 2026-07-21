package pleasant

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"

	"github.com/pottom/harmos/internal/secret"
)

func mapFixture(t *testing.T, meta Meta) *Result {
	t.Helper()
	zb := buildOfflineZip(t)
	zr, err := zip.NewReader(bytes.NewReader(zb), int64(len(zb)))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Map(zr, meta)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// writeAndReopen writes db to a temp kdbx, asserts 0600, and decrypts it back.
func writeAndReopen(t *testing.T, db *gokeepasslib.Database, master string) *gokeepasslib.Database {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cache.kdbx")
	if err := Write(db, path, secret.New(master)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("cache perms = %#o, want 0600", info.Mode().Perm())
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	reopened := gokeepasslib.NewDatabase()
	reopened.Credentials = gokeepasslib.NewPasswordCredentials(master)
	if err := gokeepasslib.NewDecoder(f).Decode(reopened); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := reopened.UnlockProtectedEntries(); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	return reopened
}

func allEntries(db *gokeepasslib.Database) []gokeepasslib.Entry {
	var out []gokeepasslib.Entry
	var walk func(g gokeepasslib.Group)
	walk = func(g gokeepasslib.Group) {
		out = append(out, g.Entries...)
		for _, sub := range g.Groups {
			walk(sub)
		}
	}
	for _, g := range db.Content.Root.Groups {
		walk(g)
	}
	return out
}

func TestMapCounts(t *testing.T) {
	res := mapFixture(t, Meta{SourceURL: "https://pps.example:10001", FetchedAt: time.Now()})
	if res.Folders != 3 { // Root + Infra + Restricted
		t.Errorf("folders = %d, want 3", res.Folders)
	}
	if res.Entries != 7 {
		t.Errorf("entries = %d, want 7", res.Entries)
	}
	if res.Attachments != 3 {
		t.Errorf("attachments = %d, want 3", res.Attachments)
	}
	if res.Expiry != "9999-12-31T23:59:59Z" {
		t.Errorf("expiry = %q", res.Expiry)
	}
}

func TestMapRoundTripEdgeCases(t *testing.T) {
	res := mapFixture(t, Meta{SourceURL: "https://pps.example:10001", FetchedAt: time.Now()})
	db := writeAndReopen(t, res.DB, "master-pw")
	entries := allEntries(db)

	if len(entries) != 7 {
		t.Fatalf("reopened entries = %d, want 7", len(entries))
	}

	titles := map[string]int{}
	byTitle := map[string]*gokeepasslib.Entry{}
	for i := range entries {
		e := &entries[i]
		titles[e.GetTitle()]++
		byTitle[e.GetTitle()] = e
	}

	// duplicate names in one folder survive as two entries
	if titles["db-prod"] != 2 {
		t.Errorf("db-prod count = %d, want 2 (duplicates preserved)", titles["db-prod"])
	}
	// unicode title intact
	if _, ok := byTitle["ékezetes-fiók-őÜű"]; !ok {
		t.Error("unicode title was mangled or missing")
	}
	// empty password preserved
	if got := byTitle["no-password"].GetPassword(); got != "" {
		t.Errorf("no-password should have empty password, got %q", got)
	}
	// server id stored verbatim
	if byTitle["no-password"].GetContent(fieldServerID) == "" {
		t.Error("pps.Id not stored")
	}
	// expired entry carries the Expires flag
	exp := byTitle["expired-cred"]
	if !exp.Times.Expires.Bool {
		t.Error("expired-cred should have Expires=true")
	}
	// TOTP mapped to an otpauth URI in the otp field
	otp := byTitle["has-totp"].GetContent("otp")
	if !strings.HasPrefix(otp, "otpauth://totp/") || !strings.Contains(otp, "secret=JBSWY3DPEHPK3PXP") {
		t.Errorf("otp field wrong: %q", otp)
	}
	if byTitle["has-totp"].GetContent(fieldTOTPSeed) != "JBSWY3DPEHPK3PXP" {
		t.Error("pps.TOTPSecret not stored")
	}

	// attachment content round-trips byte-for-byte
	wantCert := attachmentContent("00000000-0000-0000-0000-00000000a001")
	found := false
	for _, e := range entries {
		for _, ref := range e.Binaries {
			if ref.Name == "cert.pem" {
				bin := db.FindBinary(ref.Value.ID)
				if bin == nil {
					t.Fatal("cert.pem binary reference could not be resolved")
				}
				got, err := bin.GetContentBytes()
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, wantCert) {
					t.Errorf("cert.pem content mismatch: got %q want %q", got, wantCert)
				}
				found = true
			}
		}
	}
	if !found {
		t.Error("cert.pem attachment not found on any entry")
	}
}

func TestMetaCustomDataSurvives(t *testing.T) {
	res := mapFixture(t, Meta{SourceURL: "https://pps.example:10001", FetchedAt: time.Now()})
	db := writeAndReopen(t, res.DB, "master-pw")

	got := map[string]string{}
	for _, cd := range db.Content.Meta.CustomData {
		got[cd.Key] = cd.Value
	}
	if got[MetaSourceURL] != "https://pps.example:10001" {
		t.Errorf("source url meta = %q", got[MetaSourceURL])
	}
	if got[MetaExpiry] != "9999-12-31T23:59:59Z" {
		t.Errorf("expiry meta = %q", got[MetaExpiry])
	}
	if got[MetaFetchedAt] == "" {
		t.Error("fetched-at meta missing")
	}
}

func TestDeterministicUUIDv5(t *testing.T) {
	id := "00000000-0000-0000-0000-000000001006"
	a, b := detUUID(id), detUUID(id)
	if a != b {
		t.Fatal("UUID derivation is not deterministic")
	}
	u := detUUID(id)
	if v := u[6] >> 4; v != 5 {
		t.Errorf("UUID version nibble = %d, want 5", v)
	}
}

func TestPackageExpired(t *testing.T) {
	now := time.Now()
	cases := []struct {
		expiry string
		want   bool
	}{
		{"2020-01-01T00:00:00Z", true},
		{"2999-01-01T00:00:00Z", false},
		{"9999-12-31T23:59:59.9999999+00:00", false}, // real never-expires format
		{"", false},        // empty: not expired
		{"garbage", false}, // unparseable: don't block
	}
	for _, c := range cases {
		if got := PackageExpired(c.expiry, now); got != c.want {
			t.Errorf("PackageExpired(%q) = %v, want %v", c.expiry, got, c.want)
		}
	}
}
