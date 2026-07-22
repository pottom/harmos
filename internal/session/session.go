// Package session opens every configured source into one flattened set of
// entries: Pleasant caches unlocked by the single harmos master password, and
// external kdbx files by their own credentials (spec §2a — one unlock, all
// sources). Partial failure is the normal case: a source that can't be opened
// is reported as excluded, never fatal.
package session

import (
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

// AskFunc supplies a kdbx source's own password when it needs one. It may return
// a zero Secret for keyfile-only or unprotected files. nil means "don't prompt"
// — such a source is excluded if it needs a password.
type AskFunc func(p config.Profile) (secret.Secret, error)

// Open opens every profile. Pleasant caches use master; kdbx sources use their
// own password (via ask) plus any keyfile. It never fails the whole run for one
// bad source.
func Open(cfg *config.Config, master secret.Secret, ask AskFunc) *Result {
	var res Result
	for _, p := range cfg.Profiles {
		v, err := openOne(p, master, ask)
		if err != nil {
			res.Excluded = append(res.Excluded, Excluded{p.Name, err.Error()})
			continue
		}
		res.Entries = append(res.Entries, v.Entries...)
	}
	return &res
}

func openOne(p config.Profile, master secret.Secret, ask AskFunc) (*vault.Vault, error) {
	switch p.Type {
	case config.Pleasant:
		return vault.Open(p.Cache, p.Name, vault.Credentials{Password: master})
	case config.Kdbx:
		var pw secret.Secret
		if ask != nil {
			got, err := ask(p)
			if err != nil {
				return nil, err
			}
			pw = got
		}
		src := localkdbx.Source{Name: p.Name, Path: p.Path, Keyfile: p.Keyfile, Password: pw}
		return src.Open()
	default:
		return nil, errUnknownType{p.Type}
	}
}

type errUnknownType struct{ t config.Type }

func (e errUnknownType) Error() string { return "unknown source type: " + string(e.t) }
