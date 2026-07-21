// Package localkdbx reads an external .kdbx file as a source (spec §2a). The
// file IS the source: there is no cache and no sync, and it is opened strictly
// read-only — never written, locked, or timestamp-touched. It may be the user's
// primary vault.
package localkdbx

import (
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault"
)

// Source is an external kdbx profile. Password and/or Keyfile may be set
// (a KDBX composite key needs both).
type Source struct {
	Name     string
	Path     string
	Keyfile  string
	Password secret.Secret
}

// Open reads the file into a read-only vault, tagged with the source name.
func (s Source) Open() (*vault.Vault, error) {
	return vault.Open(s.Path, s.Name, vault.Credentials{
		Password: s.Password,
		Keyfile:  s.Keyfile,
	})
}
