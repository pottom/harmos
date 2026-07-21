// Package secret holds credential-carrying values that never render themselves.
//
// A [Secret] prints as "[redacted]" through every fmt verb, Stringer, GoStringer,
// and text marshaling, so a stray %v, a log line, or an accidental
// serialization can never leak a password, token, or master key. The plaintext
// escapes only through an explicit [Secret.Reveal] or [Secret.Bytes] call —
// both easy to grep for in an audit.
//
// Zeroing is best-effort only: Go's garbage collector may copy the bytes and
// strings are immutable, so [Secret.Wipe] is a mitigation, not a guarantee. The
// README says so plainly; this package does not pretend otherwise.
package secret

import "fmt"

const redacted = "[redacted]"

// Secret wraps sensitive bytes (a password, OAuth token, or master key) so they
// never leak through formatting or logging. The zero value is a valid, empty
// secret.
type Secret struct {
	b []byte
}

// New wraps a string as a Secret. The caller's original string still lives in
// memory and cannot be wiped; prefer [FromBytes] when you own the buffer.
func New(s string) Secret {
	return Secret{b: []byte(s)}
}

// FromBytes wraps b as a Secret and takes ownership of it. The caller must not
// read or reuse b afterwards — [Secret.Wipe] will zero this same backing array.
func FromBytes(b []byte) Secret {
	return Secret{b: b}
}

// Reveal returns the plaintext value. This is the one place the secret escapes;
// call it only where the value is genuinely needed (an HTTP request body, the
// kdbx writer) and never pass the result to anything that logs.
func (s Secret) Reveal() string {
	return string(s.b)
}

// Bytes returns the underlying bytes without copying. Same caution as
// [Secret.Reveal]: do not log or format the result.
func (s Secret) Bytes() []byte {
	return s.b
}

// IsZero reports whether the secret carries no bytes.
func (s Secret) IsZero() bool {
	return len(s.b) == 0
}

// Wipe best-effort zeroes the underlying bytes. Go's GC may already have copied
// them elsewhere, so this reduces exposure but does not guarantee erasure.
func (s Secret) Wipe() {
	for i := range s.b {
		s.b[i] = 0
	}
}

// String implements [fmt.Stringer]; always redacted.
func (s Secret) String() string { return redacted }

// GoString implements [fmt.GoStringer] so %#v is redacted too.
func (s Secret) GoString() string { return redacted }

// Format catches every fmt verb (%v, %s, %q, %x, ...) and redacts, so no
// formatting path can print the plaintext.
func (s Secret) Format(f fmt.State, verb rune) {
	_, _ = f.Write([]byte(redacted))
}

// MarshalText redacts, so accidentally encoding a Secret into JSON, TOML, or any
// text format writes "[redacted]" rather than the value.
func (s Secret) MarshalText() ([]byte, error) {
	return []byte(redacted), nil
}
