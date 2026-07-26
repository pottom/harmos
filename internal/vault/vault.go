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

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"

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
	Source   string // profile name this entry came from
	Path     string // folder path within the source, e.g. "Infra/db-prod"
	Title    string
	Username string
	URL      string
	Tags     []string
	Password secret.Secret
	TOTP     string  // the otpauth:// URI from the "otp" field, if any
	Notes    string  // the free-form Notes field, if any
	Custom   []Field // extra string fields beyond the standard ones
}

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
		v.walk(g, "")
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

func (v *Vault) walk(g gokeepasslib.Group, prefix string) {
	for i := range g.Entries {
		e := &g.Entries[i]
		v.Entries = append(v.Entries, Entry{
			Source:   v.Source,
			Path:     prefix,
			Title:    e.GetTitle(),
			Username: e.GetContent("UserName"),
			URL:      e.GetContent("URL"),
			Tags:     splitTags(e.Tags),
			Password: secret.New(e.GetPassword()),
			TOTP:     e.GetContent("otp"),
			Notes:    e.GetContent("Notes"),
			Custom:   customFields(e),
		})
	}
	for _, sub := range g.Groups {
		child := sub.Name
		if prefix != "" {
			child = prefix + "/" + sub.Name
		}
		v.walk(sub, child)
	}
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
			Name:      customLabel(vd.Key),
			Value:     vd.Value.Content,
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
