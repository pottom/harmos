// Package vault is the single kdbx reader (spec §2). Everything downstream —
// search, CLI, TUI — reads a vault and knows nothing about where the kdbx came
// from. Both producers feed it: a Pleasant cache (internal/source/pleasant) and
// an external kdbx file (internal/source/localkdbx).
//
// It is strictly read-only: it opens files O_RDONLY and never encodes or writes.
// Opening a user's own kdbx must leave its bytes and mtime unchanged.
package vault

import (
	"fmt"
	"os"
	"strings"
	"time"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"

	"github.com/pottom/harmos/internal/secret"
)

// Credentials open a kdbx. A vault may need a password, a keyfile, or both
// (a KDBX composite key). At least one is required.
type Credentials struct {
	Password secret.Secret // zero if the file has no password
	Keyfile  string        // "" if the file has no keyfile
}

// Entry is one credential, flattened out of the tree and tagged with its source
// (provenance is structural — §2a). The password is a Secret so it never leaks
// through logging; reveal it only when copying.
type Entry struct {
	Source   string // source name this entry came from
	Path     string // folder path within the source, e.g. "Infra/db-prod"
	Title    string
	Username string
	URL      string
	Tags     []string
	Password secret.Secret
	TOTP     string  // the otpauth:// URI from the "otp" field, if any
	Notes    string  // the free-form Notes field, if any
	Custom   []Field // extra string fields beyond the standard ones

	Created  time.Time    // zero if unset
	Modified time.Time    // zero if unset
	Expiry   time.Time    // zero unless the entry has an expiry set
	Files    []Attachment // file attachments, if any
}

// FullPath is the entry's unambiguous "source/folder/title" selector — the same
// trail shown as a breadcrumb, joined with "/". Used by `harmos get --path`.
func (e Entry) FullPath() string {
	if e.Path == "" {
		return e.Source + "/" + e.Title
	}
	return e.Source + "/" + e.Path + "/" + e.Title
}

// Attachment is a file attached to an entry, decoded from the kdbx binary pool.
type Attachment struct {
	Name string
	Data []byte
}

// Size is the attachment's byte length.
func (a Attachment) Size() int { return len(a.Data) }

// Field is one custom string field on an entry (a KeePass key/value pair that is
// not one of the standard fields). Protected fields (e.g. secondary secrets) are
// masked in the UI.
type Field struct {
	Name      string
	Value     string
	Protected bool
}

// stdField reports whether a KeePass field key is a standard/internal one that
// harmos surfaces through a dedicated Entry field (or hides), rather than as a
// generic custom field. pps.Id and pps.TOTPSecret are harmos-internal.
func stdField(key string) bool {
	switch key {
	case "Title", "UserName", "Password", "URL", "Notes", "otp",
		"pps.Id", "pps.TOTPSecret":
		return true
	}
	return false
}

// customLabel drops the harmos/Pleasant prefixes so a custom field reads by its
// bare name (pps.cuf.Environment → Environment).
func customLabel(key string) string {
	for _, p := range []string{"pps.cuf.", "pps.caf."} {
		if rest, ok := strings.CutPrefix(key, p); ok {
			return rest
		}
	}
	return key
}

// Vault is a read-only view of one opened kdbx.
type Vault struct {
	Source  string
	Entries []Entry
}

// Open reads the kdbx at path, tagging entries with source. It opens the file
// read-only and never writes to it. creds must carry a password and/or keyfile.
func Open(path, source string, creds Credentials) (*Vault, error) {
	dbCreds, err := buildCredentials(creds)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path) // O_RDONLY — never written
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

	v := &Vault{Source: source}
	// The top-level group is the database root container; its own name is not
	// part of entry paths (the source name provides the top-level identity).
	for _, g := range db.Content.Root.Groups {
		v.walk(db, g, "")
	}
	return v, nil
}

// IsBadCredential reports whether err from Open looks like a wrong password (or
// wrong keyfile) rather than a missing or corrupt file. gokeepasslib's credential
// errors are unexported, so this matches on their messages — enough to decide
// whether re-prompting for the password is worth it.
func IsBadCredential(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, marker := range []string{
		"Wrong password?",        // KDBX4 HMAC / integrity
		"failed to verify HMAC",  // KDBX4 block HMAC
		"Sha256 of header",       // KDBX3.1 header hash
		"HMAC-SHA256 of header",  // KDBX4 header HMAC
		"integrity check failed", // KDBX4 database integrity
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

func (v *Vault) walk(db *gokeepasslib.Database, g gokeepasslib.Group, prefix string) {
	for i := range g.Entries {
		e := &g.Entries[i]
		v.Entries = append(v.Entries, Entry{
			Source:   v.Source,
			Path:     prefix,
			Title:    oneline(e.GetTitle()),
			Username: oneline(e.GetContent("UserName")),
			URL:      oneline(e.GetContent("URL")),
			Tags:     splitTags(e.Tags),
			Password: secret.New(e.GetPassword()),
			TOTP:     e.GetContent("otp"),
			Notes:    e.GetContent("Notes"),
			Custom:   customFields(e),
			Created:  timeOf(e.Times.CreationTime),
			Modified: timeOf(e.Times.LastModificationTime),
			Expiry:   expiryOf(e.Times),
			Files:    attachments(db, e),
		})
	}
	for _, sub := range g.Groups {
		child := oneline(sub.Name)
		if prefix != "" {
			child = prefix + "/" + oneline(sub.Name)
		}
		v.walk(db, sub, child)
	}
}

// oneline flattens a display string to a single line: control characters
// (newlines/tabs, e.g. a Pleasant folder name that embeds a newline) become
// spaces, so a value can never break the TUI layout.
func oneline(s string) string {
	return strings.Map(func(r rune) rune {
		if r < ' ' {
			return ' '
		}
		return r
	}, s)
}

func timeOf(tw *w.TimeWrapper) time.Time {
	if tw == nil {
		return time.Time{}
	}
	return tw.Time
}

// expiryOf returns the entry's expiry only when the Expires flag is set.
func expiryOf(td gokeepasslib.TimeData) time.Time {
	if !td.Expires.Bool || td.ExpiryTime == nil {
		return time.Time{}
	}
	return td.ExpiryTime.Time
}

// attachments decodes an entry's file attachments from the database binary pool.
func attachments(db *gokeepasslib.Database, e *gokeepasslib.Entry) []Attachment {
	var out []Attachment
	for i := range e.Binaries {
		ref := &e.Binaries[i]
		bin := db.FindBinary(ref.Value.ID)
		if bin == nil {
			continue
		}
		data, err := bin.GetContentBytes()
		if err != nil {
			continue
		}
		out = append(out, Attachment{Name: ref.Name, Data: data})
	}
	return out
}

// customFields collects an entry's non-standard string fields, in file order,
// with their protected flag (content is already decrypted by this point).
func customFields(e *gokeepasslib.Entry) []Field {
	var out []Field
	for _, vd := range e.Values {
		if stdField(vd.Key) {
			continue
		}
		out = append(out, Field{
			Name:      oneline(customLabel(vd.Key)),
			Value:     oneline(vd.Value.Content),
			Protected: vd.Value.Protected.Bool,
		})
	}
	return out
}

func splitTags(s string) []string {
	if s == "" {
		return nil
	}
	// KeePass stores tags separated by ";" (harmos) or ","; accept both.
	f := func(r rune) bool { return r == ';' || r == ',' }
	return strings.FieldsFunc(s, f)
}

func buildCredentials(c Credentials) (*gokeepasslib.DBCredentials, error) {
	hasPw := !c.Password.IsZero()
	hasKey := c.Keyfile != ""
	switch {
	case hasPw && hasKey:
		data, err := readKeyfile(c.Keyfile)
		if err != nil {
			return nil, err
		}
		return gokeepasslib.NewPasswordAndKeyDataCredentials(c.Password.Reveal(), data)
	case hasKey:
		data, err := readKeyfile(c.Keyfile)
		if err != nil {
			return nil, err
		}
		return gokeepasslib.NewKeyDataCredentials(data)
	case hasPw:
		return gokeepasslib.NewPasswordCredentials(c.Password.Reveal()), nil
	default:
		return nil, fmt.Errorf("no credentials: a password and/or keyfile is required")
	}
}

// readKeyfile reads the keyfile ourselves (closing it promptly) rather than
// letting gokeepasslib's path-based helpers open it — those leak the handle,
// which on Windows blocks deletion of the file.
func readKeyfile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read keyfile: %w", err)
	}
	return data, nil
}
