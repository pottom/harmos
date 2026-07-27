package pwgen

import (
	"strings"
	"testing"
)

func TestGenerateLengthAndPool(t *testing.T) {
	o := Options{Length: 20, Lower: true, Upper: true, Digit: true, Symbol: true}
	p, err := Generate(o)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(p)) != 20 {
		t.Errorf("length = %d, want 20", len([]rune(p)))
	}
	all := Lower + Upper + Digit + Symbol
	for _, r := range p {
		if !strings.ContainsRune(all, r) {
			t.Errorf("char %q outside the enabled pool", r)
		}
	}
}

func TestGenerateRespectsDisabledClasses(t *testing.T) {
	o := Options{Length: 40, Lower: true} // only lowercase
	p, err := Generate(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range p {
		if !strings.ContainsRune(Lower, r) {
			t.Errorf("char %q should be lowercase-only", r)
		}
	}
}

func TestExclude(t *testing.T) {
	o := Options{Length: 40, Lower: true, Upper: true, Digit: true, Symbol: true, Exclude: "aeiou0O!"}
	for range 20 {
		p, err := Generate(o)
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(p, "aeiou0O!") {
			t.Fatalf("excluded character leaked into %q", p)
		}
	}
}

func TestAvoidAmbiguous(t *testing.T) {
	o := Options{Length: 60, Lower: true, Upper: true, Digit: true, AvoidAmbig: true}
	for range 20 { // sample a few, the pool is small
		p, err := Generate(o)
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(p, ambiguous) {
			t.Fatalf("ambiguous glyph leaked into %q", p)
		}
	}
}

func TestOneEach(t *testing.T) {
	o := Options{Length: 12, Lower: true, Upper: true, Digit: true, Symbol: true, OneEach: true}
	for range 50 {
		p, err := Generate(o)
		if err != nil {
			t.Fatal(err)
		}
		for _, class := range []string{Lower, Upper, Digit, Symbol} {
			if !strings.ContainsAny(p, class) {
				t.Fatalf("OneEach: %q missing a class %q", p, class)
			}
		}
	}
}

func TestErrors(t *testing.T) {
	if _, err := Generate(Options{Length: 20}); err != ErrNoClasses {
		t.Errorf("no classes should error, got %v", err)
	}
	if _, err := Generate(Options{Length: 4, Lower: true}); err != ErrLength {
		t.Errorf("short length should error, got %v", err)
	}
	if _, err := Generate(Options{Length: 999, Lower: true}); err != ErrLength {
		t.Errorf("long length should error, got %v", err)
	}
}

func TestMany(t *testing.T) {
	ps, err := Many(Default(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 50 {
		t.Fatalf("want 50, got %d", len(ps))
	}
	// crypto/rand output should not repeat across 50 20-char samples.
	seen := map[string]bool{}
	for _, p := range ps {
		if seen[p] {
			t.Errorf("duplicate password %q — randomness broken", p)
		}
		seen[p] = true
	}
}

// A password must never contain a space or a non-printable/control character —
// those are invisible or get mangled by shells, forms and CSV.
func TestOnlyVisibleChars(t *testing.T) {
	o := Options{Length: 64, Lower: true, Upper: true, Digit: true, Symbol: true}
	for range 50 {
		p, err := Generate(o)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range p {
			if r <= ' ' || r == 0x7f {
				t.Fatalf("password %q contains a space or control char %q (%#x)", p, r, r)
			}
		}
	}
}

func TestEntropyAndPool(t *testing.T) {
	o := Default()
	if got := o.PoolSize(); got != len(Lower)+len(Upper)+len(Digit)+len(Symbol) {
		t.Errorf("pool size = %d", got)
	}
	if bits := o.EntropyBits(); bits < 100 { // 20 chars over a ~73-char pool ≈ 123 bits
		t.Errorf("entropy suspiciously low: %.1f", bits)
	}
}
