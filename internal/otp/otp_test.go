package otp

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"testing"
	"time"
)

// RFC 6238 Appendix B test vectors. The seed is the ASCII string
// "12345678901234567890" (repeated for the wider hashes), 8 digits, 30s.
func TestRFC6238Vectors(t *testing.T) {
	sha1Key := Key{Secret: []byte("12345678901234567890"), Digits: 8, Period: 30, Alg: sha1.New}
	sha256Key := Key{Secret: []byte("12345678901234567890123456789012"), Digits: 8, Period: 30, Alg: sha256.New}
	sha512Key := Key{Secret: []byte("1234567890123456789012345678901234567890123456789012345678901234"), Digits: 8, Period: 30, Alg: sha512.New}

	cases := []struct {
		unix int64
		key  Key
		want string
	}{
		{59, sha1Key, "94287082"},
		{1111111109, sha1Key, "07081804"},
		{1234567890, sha1Key, "89005924"},
		{2000000000, sha1Key, "69279037"},
		{59, sha256Key, "46119246"},
		{1111111109, sha256Key, "68084774"},
		{59, sha512Key, "90693936"},
		{1111111109, sha512Key, "25091201"},
	}
	for _, c := range cases {
		if got := c.key.Code(time.Unix(c.unix, 0)); got != c.want {
			t.Errorf("Code(%d) = %s, want %s", c.unix, got, c.want)
		}
	}
}

func TestParse(t *testing.T) {
	// secret "12345678901234567890" base32-encoded
	uri := "otpauth://totp/harmos:demo?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&digits=8&period=30&issuer=harmos"
	k, err := Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	if k.Digits != 8 || k.Period != 30 {
		t.Errorf("digits/period = %d/%d, want 8/30", k.Digits, k.Period)
	}
	if got := k.Code(time.Unix(59, 0)); got != "94287082" {
		t.Errorf("parsed key Code(59) = %s, want 94287082", got)
	}
}

func TestParseDefaults(t *testing.T) {
	k, err := Parse("otpauth://totp/x?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ")
	if err != nil {
		t.Fatal(err)
	}
	if k.Digits != 6 || k.Period != 30 {
		t.Errorf("defaults = %d/%d, want 6/30", k.Digits, k.Period)
	}
	if len(k.Code(time.Now())) != 6 {
		t.Error("default code should be 6 digits")
	}
}

func TestParseRejects(t *testing.T) {
	for _, uri := range []string{
		"otpauth://hotp/x?secret=GEZDGNBV",               // not totp
		"https://example.com",                            // not otpauth
		"otpauth://totp/x",                               // no secret
		"otpauth://totp/x?secret=GEZDGNBV&algorithm=MD5", // bad alg
		"otpauth://totp/x?secret=0189",                   // invalid base32
	} {
		if _, err := Parse(uri); err == nil {
			t.Errorf("Parse(%q) should have failed", uri)
		}
	}
}

func TestRemaining(t *testing.T) {
	k := Key{Period: 30, Alg: sha1.New, Digits: 6, Secret: []byte("x")}
	if r := k.Remaining(time.Unix(0, 0)); r != 30 {
		t.Errorf("Remaining at t=0 = %d, want 30", r)
	}
	if r := k.Remaining(time.Unix(25, 0)); r != 5 {
		t.Errorf("Remaining at t=25 = %d, want 5", r)
	}
}
