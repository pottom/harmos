// Package keyring stores kdbx source passwords in the OS-native secret store
// (macOS Keychain, Windows Credential Manager, Linux Secret Service) so harmos
// can unlock a local kdbx without prompting every time (spec §9 — secrets never
// touch the config file). Storing a password is always opt-in.
package keyring

import (
	"errors"

	gokeyring "github.com/zalando/go-keyring"

	"github.com/pottom/harmos/internal/secret"
)

// service is the keyring "service" every harmos entry is filed under; the
// profile name is the "account".
const service = "harmos"

// Store saves a profile's password in the OS keyring, replacing any existing one.
func Store(profile string, pw secret.Secret) error {
	return gokeyring.Set(service, profile, pw.Reveal())
}

// Fetch returns a profile's stored password. ok is false (with a nil error) when
// nothing is stored for that profile.
func Fetch(profile string) (pw secret.Secret, ok bool, err error) {
	v, err := gokeyring.Get(service, profile)
	if errors.Is(err, gokeyring.ErrNotFound) {
		return secret.Secret{}, false, nil
	}
	if err != nil {
		return secret.Secret{}, false, err
	}
	return secret.New(v), true, nil
}

// Forget deletes a profile's stored password. It is not an error if there was
// nothing to delete.
func Forget(profile string) error {
	err := gokeyring.Delete(service, profile)
	if errors.Is(err, gokeyring.ErrNotFound) {
		return nil
	}
	return err
}
