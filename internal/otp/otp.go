// Package otp generates TOTP codes (RFC 6238) from the otpauth:// URI stored in
// a kdbx entry's "otp" field — the same field KeePassXC reads. It is a small,
// dependency-free implementation so harmos can show and copy a live code.
package otp

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"hash"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Key is a parsed TOTP configuration.
type Key struct {
	Secret []byte
	Digits int
	Period int
	Alg    func() hash.Hash
}

// Parse reads an otpauth://totp/…?secret=…&digits=…&period=…&algorithm=… URI.
// Only TOTP is supported (HOTP has no time window to display).
func Parse(uri string) (Key, error) {
	u, err := url.Parse(strings.TrimSpace(uri))
	if err != nil {
		return Key{}, err
	}
	if !strings.EqualFold(u.Scheme, "otpauth") {
		return Key{}, fmt.Errorf("not an otpauth URI")
	}
	if !strings.EqualFold(u.Host, "totp") {
		return Key{}, fmt.Errorf("unsupported OTP type %q (only totp)", u.Host)
	}
	q := u.Query()

	sec := strings.ToUpper(q.Get("secret"))
	sec = strings.NewReplacer(" ", "", "-", "").Replace(sec)
	sec = strings.TrimRight(sec, "=")
	if sec == "" {
		return Key{}, fmt.Errorf("otpauth URI has no secret")
	}
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(sec)
	if err != nil {
		return Key{}, fmt.Errorf("bad base32 secret: %w", err)
	}

	k := Key{Secret: raw, Digits: 6, Period: 30, Alg: sha1.New}
	if n, err := strconv.Atoi(q.Get("digits")); err == nil && n > 0 {
		k.Digits = n
	}
	if n, err := strconv.Atoi(q.Get("period")); err == nil && n > 0 {
		k.Period = n
	}
	switch strings.ToUpper(q.Get("algorithm")) {
	case "", "SHA1":
		k.Alg = sha1.New
	case "SHA256":
		k.Alg = sha256.New
	case "SHA512":
		k.Alg = sha512.New
	default:
		return Key{}, fmt.Errorf("unsupported algorithm %q", q.Get("algorithm"))
	}
	return k, nil
}

// Code is the TOTP code for time t, zero-padded to the configured digit count.
func (k Key) Code(t time.Time) string {
	counter := uint64(t.Unix()) / uint64(k.Period)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(k.Alg, k.Secret)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	off := sum[len(sum)-1] & 0x0f
	val := (uint32(sum[off]&0x7f) << 24) |
		(uint32(sum[off+1]) << 16) |
		(uint32(sum[off+2]) << 8) |
		uint32(sum[off+3])

	mod := uint32(1)
	for range k.Digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", k.Digits, val%mod)
}

// Remaining is the seconds left before the current code rolls over.
func (k Key) Remaining(t time.Time) int {
	return k.Period - int(t.Unix()%int64(k.Period))
}
