package vault

import (
	"crypto/rand"
	"fmt"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
)

// Rekey replaces every per-file random value that the encryption depends on.
//
// It exists because gokeepasslib writes the *decoded* file's MasterSeed,
// EncryptionIV and KDF salt straight back out (see header.go writeTo4). Saving
// twice with the same password would therefore encrypt two different plaintexts
// under the same key and the same nonce — with ChaCha20 that is keystream reuse,
// and XORing the two ciphertexts together cancels the keystream entirely.
// KeePassXC regenerates these on every save; so do we.
//
// Rekey must run while the protected values are UNLOCKED (plaintext). It
// replaces the inner random stream key, and the values are re-encrypted from
// plaintext under that new key by the LockProtectedEntries call that follows.
// Rekeying a locked database would leave the header advertising a key that does
// not match the ciphertext.
//
// The KDF cost parameters (memory, iterations, parallelism, rounds, version) are
// deliberately left alone: they are the file owner's choice, not ours.
func Rekey(db *gokeepasslib.Database) error {
	if db.Header == nil || db.Header.FileHeaders == nil {
		return fmt.Errorf("rekey: database has no file headers")
	}
	fh := db.Header.FileHeaders

	if err := fill(fh.MasterSeed, "master seed"); err != nil {
		return err
	}
	// The IV length is cipher-specific — 12 bytes for ChaCha20, 16 for AES and
	// TwoFish. Refill in place at whatever length the file already uses rather
	// than guessing from the cipher ID.
	if err := fill(fh.EncryptionIV, "encryption IV"); err != nil {
		return err
	}
	if fh.KdfParameters != nil {
		// Salt is a [32]byte value, and header.updateRawData rebuilds the KDF
		// variant dictionary from these struct fields, so assigning it is enough.
		if _, err := rand.Read(fh.KdfParameters.Salt[:]); err != nil {
			return fmt.Errorf("rekey kdf salt: %w", err)
		}
	}
	if db.Content != nil && db.Content.InnerHeader != nil {
		if err := fill(db.Content.InnerHeader.InnerRandomStreamKey, "inner random stream key"); err != nil {
			return err
		}
	}
	return nil
}

// fill refills a header slice in place, keeping its existing length — that
// length is part of the file's format and is not ours to change.
func fill(b []byte, what string) error {
	if len(b) == 0 {
		return fmt.Errorf("rekey: %s is missing or empty", what)
	}
	if _, err := rand.Read(b); err != nil {
		return fmt.Errorf("rekey %s: %w", what, err)
	}
	return nil
}
