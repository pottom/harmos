package pleasant

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"

	"github.com/pottom/harmos/internal/secret"
)

// harmosNS is the fixed namespace for deterministic entry/group UUIDs (UUIDv5
// over the Pleasant server Id). Stable across runs; never derived from secrets.
// This sidesteps the .NET-GUID mixed-endian trap (spec §6): the raw Id is stored
// verbatim in a custom field, and the kdbx UUID is hashed from it.
var harmosNS = uuid.MustParse("2f5c8b6a-4e3d-4b1a-9c7e-1a2b3c4d5e6f")

// Field and metadata keys written into the kdbx.
const (
	fieldServerID = "pps.Id"
	fieldTOTPSeed = "pps.TOTPSecret"
	prefixCUF     = "pps.cuf."
	prefixCAF     = "pps.caf."

	MetaSourceURL = "harmos.SourceURL"
	MetaFetchedAt = "harmos.FetchedAt"
	MetaExpiry    = "harmos.Expiry"
)

// Cache KDF cost (spec §14). gokeepasslib's default is a weak 1 MiB / 2 rounds;
// this is the OWASP Argon2 baseline — 19 MiB, 2 iterations, 1 lane — far dearer
// to brute-force if the cache file leaks, still a sub-second unlock. The cache
// is a re-syncable derived artifact, so this is a deliberate, documented middle
// ground, not the library default by accident.
const (
	cacheKDFMemoryBytes uint64 = 19 * 1024 * 1024 // 19 MiB
	cacheKDFIterations  uint64 = 2
	cacheKDFParallelism uint32 = 1
)

// Meta is provenance written into the kdbx so status/TUI can report cache age
// and package expiry without a network call (spec §6).
type Meta struct {
	SourceURL string
	FetchedAt time.Time
}

// Result is what Map produced: the database to write, plus counts and the
// package Expiry for the sync report and expiry enforcement.
type Result struct {
	DB          *gokeepasslib.Database
	Expiry      string
	Folders     int
	Entries     int
	Attachments int
}

// Map turns an OfflinePackage zip (a JSON manifest plus attachment blobs named
// by AttachmentId) into an in-memory KDBX4 database. Attachment bytes come from
// the zip entries via db.AddBinary — never a base64 FileData field, which is
// always null (M1 FINDINGS), and never the deprecated inner-header Add.
func Map(zr *zip.Reader, meta Meta) (*Result, error) {
	files := make(map[string]*zip.File, len(zr.File))
	var manifest *zip.File
	for _, f := range zr.File {
		if manifest == nil && strings.HasSuffix(f.Name, ".json") {
			manifest = f
			continue
		}
		files[f.Name] = f
	}
	if manifest == nil {
		return nil, fmt.Errorf("offline package has no JSON manifest")
	}
	raw, err := readZip(manifest)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var pkg Package
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	db := gokeepasslib.NewDatabase(gokeepasslib.WithDatabaseKDBXVersion4())
	db.Content.Meta.DatabaseName = "harmos cache"
	db.Content.Meta.CustomData = append(db.Content.Meta.CustomData,
		gokeepasslib.CustomData{Key: MetaSourceURL, Value: meta.SourceURL},
		gokeepasslib.CustomData{Key: MetaFetchedAt, Value: meta.FetchedAt.UTC().Format(time.RFC3339)},
		gokeepasslib.CustomData{Key: MetaExpiry, Value: pkg.Expiry},
	)

	m := &mapper{db: db, files: files}
	root := m.folder(pkg.Root)
	db.Content.Root = &gokeepasslib.RootData{Groups: []gokeepasslib.Group{root}}
	if m.err != nil {
		return nil, m.err
	}
	return &Result{
		DB:          db,
		Expiry:      pkg.Expiry,
		Folders:     m.folders,
		Entries:     m.entries,
		Attachments: m.attachments,
	}, nil
}

// Write encrypts db with the master password and writes it to path as a 0600
// KDBX4 file.
func Write(db *gokeepasslib.Database, path string, master secret.Secret) (err error) {
	// Strengthen the Argon2 cost beyond gokeepasslib's weak default (§14).
	if kdf := db.Header.FileHeaders.KdfParameters; kdf != nil {
		kdf.Memory = cacheKDFMemoryBytes
		kdf.Iterations = cacheKDFIterations
		kdf.Parallelism = cacheKDFParallelism
	}

	db.Credentials = gokeepasslib.NewPasswordCredentials(master.Reveal())
	if err := db.LockProtectedEntries(); err != nil {
		return fmt.Errorf("lock entries: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	if err := gokeepasslib.NewEncoder(f).Encode(db); err != nil {
		return fmt.Errorf("encode kdbx: %w", err)
	}
	return nil
}

// PackageExpired reports whether a package Expiry is in the past. An empty or
// unparseable value is treated as not-expired (don't block on a value we can't
// read). Enforcement per spec §9.
func PackageExpired(expiry string, now time.Time) bool {
	if expiry == "" {
		return false
	}
	t, err := parseExpiry(expiry)
	if err != nil {
		return false
	}
	return now.After(t)
}

type mapper struct {
	db                          *gokeepasslib.Database
	files                       map[string]*zip.File
	folders, entries, attachments int
	err                         error
}

func (m *mapper) folder(f Folder) gokeepasslib.Group {
	m.folders++
	g := gokeepasslib.NewGroup()
	g.Name = f.Name
	g.Notes = f.Notes
	for _, child := range f.Children {
		g.Groups = append(g.Groups, m.folder(child))
	}
	for _, cr := range f.Credentials {
		g.Entries = append(g.Entries, m.entry(cr))
	}
	return g
}

func (m *mapper) entry(cr Credential) gokeepasslib.Entry {
	m.entries++
	e := gokeepasslib.NewEntry()
	e.UUID = detUUID(cr.Id)
	e.Values = append(e.Values,
		val("Title", cr.Name),
		val("UserName", cr.Username),
		pval("Password", cr.Password),
		val("URL", cr.Url),
		val("Notes", cr.Notes),
		val(fieldServerID, cr.Id),
	)
	for k, v := range cr.CustomUserFields {
		e.Values = append(e.Values, val(prefixCUF+k, fmt.Sprintf("%v", v)))
	}
	for k, v := range cr.CustomApplicationFields {
		e.Values = append(e.Values, val(prefixCAF+k, fmt.Sprintf("%v", v)))
	}
	if cr.TOTPSecret != "" {
		e.Values = append(e.Values, pval("otp", otpauthURI(cr)), pval(fieldTOTPSeed, cr.TOTPSecret))
	}
	if len(cr.Tags) > 0 {
		names := make([]string, len(cr.Tags))
		for i, t := range cr.Tags {
			names[i] = t.Name
		}
		e.Tags = strings.Join(names, ";")
	}

	e.Times = gokeepasslib.NewTimeData()
	if t := parseTime(cr.Created); t != nil {
		e.Times.CreationTime = t
	}
	if t := parseTime(cr.Modified); t != nil {
		e.Times.LastModificationTime = t
	}
	if cr.Expires != nil {
		if t := parseTime(*cr.Expires); t != nil {
			e.Times.ExpiryTime = t
			e.Times.Expires = w.NewBoolWrapper(true)
		}
	}

	for _, a := range cr.Attachments {
		f, ok := m.files[a.AttachmentId]
		if !ok {
			m.setErr(fmt.Errorf("attachment %s (%s) missing from package", a.AttachmentId, a.FileName))
			continue
		}
		data, err := readZip(f)
		if err != nil {
			m.setErr(fmt.Errorf("read attachment %s: %w", a.FileName, err))
			continue
		}
		bin := m.db.AddBinary(data)
		e.Binaries = append(e.Binaries, bin.CreateReference(a.FileName))
		m.attachments++
	}
	return e
}

func (m *mapper) setErr(e error) {
	if m.err == nil {
		m.err = e
	}
}

func detUUID(serverID string) gokeepasslib.UUID {
	u := uuid.NewSHA1(harmosNS, []byte(serverID))
	var ku gokeepasslib.UUID
	copy(ku[:], u[:])
	return ku
}

func otpauthURI(cr Credential) string {
	digits, period := cr.TOTPDigits, cr.TOTPPeriod
	if digits == 0 {
		digits = 6
	}
	if period == 0 {
		period = 30
	}
	q := url.Values{}
	q.Set("secret", cr.TOTPSecret)
	q.Set("digits", strconv.Itoa(digits))
	q.Set("period", strconv.Itoa(period))
	if cr.TOTPIssuer != "" {
		q.Set("issuer", cr.TOTPIssuer)
	}
	return "otpauth://totp/" + url.PathEscape(cr.Name) + "?" + q.Encode()
}

func parseTime(s string) *w.TimeWrapper {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &w.TimeWrapper{Time: t}
}

func parseExpiry(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

func readZip(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

func val(k, v string) gokeepasslib.ValueData {
	return gokeepasslib.ValueData{Key: k, Value: gokeepasslib.V{Content: v}}
}

func pval(k, v string) gokeepasslib.ValueData {
	return gokeepasslib.ValueData{Key: k, Value: gokeepasslib.V{Content: v, Protected: w.NewBoolWrapper(true)}}
}
