// Package session opens every configured source into one flattened set of
// entries: Pleasant caches unlocked by the single harmos master password, and
// external kdbx files by their own credentials (spec §2a — one unlock, all
// sources). Partial failure is the normal case: a source that can't be opened
// is reported as excluded, never fatal.
package session

import (
	"fmt"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/source/localkdbx"
	"github.com/pottom/harmos/internal/vault"
)

// Excluded records a source that could not be opened, and why (so search can
// report it as excluded rather than silently missing — spec §2a).
type Excluded struct {
	Source string
	Reason string
}

// Result is the combined, read-only view of every source that opened.
type Result struct {
	Entries  []vault.Entry
	Excluded []Excluded
}

// AskFunc supplies a source's password. retry is true when the previous password
// was rejected, so the caller can re-prompt (and say so). interactive reports
// whether the password came from a live prompt: if it did, a wrong password is
// worth re-prompting; if it came from env/keyring/non-TTY, retrying can't help.
// The returned Secret may be zero for keyfile-only or unprotected files.
type AskFunc func(p config.Profile, retry bool) (pw secret.Secret, interactive bool, err error)

// maxUnlockAttempts bounds the wrong-password re-prompts per source.
const maxUnlockAttempts = 3

// Open opens every profile, resolving each source's password through ask. It
// never fails the whole run for one bad source — a source that can't be opened
// is recorded as excluded.
func Open(cfg *config.Config, ask AskFunc) *Result {
	var res Result
	for _, p := range cfg.Profiles {
		v, err := openOne(p, ask)
		if err != nil {
			res.Excluded = append(res.Excluded, Excluded{p.Name, err.Error()})
			continue
		}
		res.Entries = append(res.Entries, v.Entries...)
	}
	return &res
}

// openOne resolves a source's password and opens it, re-prompting on a wrong
// password (up to maxUnlockAttempts) so a bad password is caught immediately —
// not only surfaced as an excluded source at the end. It stops early when ask
// returns the same secret again (non-interactive, keyring, or env), since a
// retry then cannot help.
func openOne(p config.Profile, ask AskFunc) (*vault.Vault, error) {
	if ask == nil {
		return nil, fmt.Errorf("no way to obtain a password for %q", p.Name)
	}
	var last error
	for attempt := range maxUnlockAttempts {
		pw, interactive, err := ask(p, attempt > 0)
		if err != nil {
			return nil, err
		}
		v, oerr := openWith(p, pw)
		if oerr == nil {
			return v, nil
		}
		if !vault.IsBadCredential(oerr) {
			return nil, oerr // missing or corrupt file — a new password won't help
		}
		last = oerr
		if !interactive {
			break // env/keyring/non-TTY gave a fixed value — retrying can't help
		}
	}
	return nil, last
}

func openWith(p config.Profile, pw secret.Secret) (*vault.Vault, error) {
	switch p.Type {
	case config.Pleasant:
		return vault.Open(p.Cache, p.Name, vault.Credentials{Password: pw})
	case config.Kdbx:
		src := localkdbx.Source{Name: p.Name, Path: p.Path, Keyfile: p.Keyfile, Password: pw}
		return src.Open()
	default:
		return nil, errUnknownType{p.Type}
	}
}

type errUnknownType struct{ t config.Type }

func (e errUnknownType) Error() string { return "unknown source type: " + string(e.t) }
