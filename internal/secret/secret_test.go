package secret

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const plaintext = "hunter2-super-secret"

// A Secret must never render its plaintext through any formatting path.
func TestSecretNeverLeaks(t *testing.T) {
	s := New(plaintext)

	cases := map[string]string{
		"%v":       fmt.Sprintf("%v", s),
		"%s":       fmt.Sprintf("%s", s),
		"%q":       fmt.Sprintf("%q", s),
		"%x":       fmt.Sprintf("%x", s),
		"%#v":      fmt.Sprintf("%#v", s),
		"%+v":      fmt.Sprintf("%+v", s),
		"String()": s.String(),
		"pointer":  fmt.Sprintf("%v", &s),
	}
	for name, got := range cases {
		if strings.Contains(got, plaintext) {
			t.Errorf("%s leaked the plaintext: %q", name, got)
		}
		if !strings.Contains(got, redacted) {
			t.Errorf("%s did not redact, got %q", name, got)
		}
	}
}

// A Secret embedded in a struct must not leak through JSON either.
func TestSecretJSONRedacts(t *testing.T) {
	type payload struct {
		User string
		Pass Secret
	}
	b, err := json.Marshal(payload{User: "alice", Pass: New(plaintext)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), plaintext) {
		t.Fatalf("JSON leaked the plaintext: %s", b)
	}
	if !strings.Contains(string(b), redacted) {
		t.Fatalf("JSON did not redact: %s", b)
	}
}

// Reveal is the sanctioned escape hatch and must return the real value.
func TestReveal(t *testing.T) {
	s := New(plaintext)
	if got := s.Reveal(); got != plaintext {
		t.Fatalf("Reveal() = %q, want the plaintext", got)
	}
}

func TestWipe(t *testing.T) {
	b := []byte(plaintext)
	s := FromBytes(b)
	s.Wipe()
	for i, c := range s.Bytes() {
		if c != 0 {
			t.Fatalf("byte %d not zeroed after Wipe", i)
		}
	}
	if s.Reveal() != strings.Repeat("\x00", len(plaintext)) {
		t.Fatal("Reveal after Wipe should be all zero bytes")
	}
}

func TestZeroValueAndIsZero(t *testing.T) {
	var s Secret // zero value
	if !s.IsZero() {
		t.Fatal("zero-value Secret should report IsZero")
	}
	if s.String() != redacted {
		t.Fatal("zero-value Secret should still redact")
	}
	if !New("").IsZero() {
		t.Fatal(`New("") should be zero`)
	}
	if New("x").IsZero() {
		t.Fatal(`New("x") should not be zero`)
	}
}
