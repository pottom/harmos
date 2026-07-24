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
// profile name is the "account". The shared harmos master password (which
// unlocks every Pleasant cache — spec §2a) lives under a reserved account.
const (
	service       = "harmos"
	masterAccount = "__harmos_master__"
)

// Store saves a profile's password in the OS keyring, replacing any existing one.
func Store(profile string, pw secret.Secret) error { return set(profile, pw) }

// Fetch returns a profile's stored password. ok is false (with a nil error) when
// nothing is stored for that profile.
func Fetch(profile string) (pw secret.Secret, ok bool, err error) { return get(profile) }

// Forget deletes a profile's stored password. It is not an error if there was
// nothing to delete.
func Forget(profile string) error { return del(profile) }

// StoreMaster saves the shared harmos master password.
func StoreMaster(pw secret.Secret) error { return set(masterAccount, pw) }

// FetchMaster returns the shared harmos master password, if one is stored.
func FetchMaster() (secret.Secret, bool, error) { return get(masterAccount) }

// ForgetMaster deletes the shared harmos master password.
func ForgetMaster() error { return del(masterAccount) }

// serverAccount namespaces a Pleasant source's server login password so it never
// collides with a kdbx source's per-file password of the same profile name.
func serverAccount(profile string) string { return "server:" + profile }

// StoreServer saves a Pleasant source's server login password.
func StoreServer(profile string, pw secret.Secret) error { return set(serverAccount(profile), pw) }

// FetchServer returns a Pleasant source's server login password, if stored.
func FetchServer(profile string) (secret.Secret, bool, error) { return get(serverAccount(profile)) }

// ForgetServer deletes a Pleasant source's server login password.
func ForgetServer(profile string) error { return del(serverAccount(profile)) }

func set(account string, pw secret.Secret) error {
	return gokeyring.Set(service, account, pw.Reveal())
}

func get(account string) (secret.Secret, bool, error) {
	v, err := gokeyring.Get(service, account)
	if errors.Is(err, gokeyring.ErrNotFound) {
		return secret.Secret{}, false, nil
	}
	if err != nil {
		return secret.Secret{}, false, err
	}
	return secret.New(v), true, nil
}

func del(account string) error {
	err := gokeyring.Delete(service, account)
	if errors.Is(err, gokeyring.ErrNotFound) {
		return nil
	}
	return err
}
