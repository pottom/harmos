// Package localkdbx reads an external .kdbx file as a source (spec §2a). The
// file IS the source: there is no cache and no sync.
//
// It may be the user's primary vault, so browsing never writes: no rewrite, no
// lock file, no timestamp touched. Editing exists (M6) but is deliberately out
// of reach by default — it needs a Handle, which the caller must ask for, and
// which will only save when explicitly told to.
package localkdbx

import (
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault"
)

// Source is an external kdbx source. Password and/or Keyfile may be set
// (a KDBX composite key needs both).
type Source struct {
	Name     string
	Path     string
	Keyfile  string
	Password secret.Secret
}

// Open reads the file into a read-only vault, tagged with the source name.
func (s Source) Open() (*vault.Vault, error) {
	return vault.Open(s.Path, s.Name, s.credentials())
}

// OpenHandle reads the file and keeps what a save would need. It writes nothing
// by itself — the handle's Save is still the only writer, and it refuses files
// harmos cannot round-trip.
func (s Source) OpenHandle() (*vault.Handle, error) {
	return vault.OpenHandle(s.Path, s.Name, s.credentials())
}

func (s Source) credentials() vault.Credentials {
	return vault.Credentials{Password: s.Password, Keyfile: s.Keyfile}
}
