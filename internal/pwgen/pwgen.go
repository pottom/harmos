// Package pwgen generates random passwords from a chosen set of character
// classes. It uses crypto/rand exclusively — a password generator that leaks
// bits through a predictable PRNG is worse than useless, so there is no
// math/rand fallback anywhere in this package.
package pwgen

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
)

// Character classes. The symbol set is the "shell-friendly" subset: no brackets,
// quotes or punctuation that a CSV, shell or downstream system tends to mangle.
const (
	Lower  = "abcdefghijklmnopqrstuvwxyz"
	Upper  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	Digit  = "0123456789"
	Symbol = "!@#$%^&*-_=+"

	// ambiguous glyphs that are easy to misread; dropped when Options.AvoidAmbig.
	ambiguous = "0O1lI"
)

// Bounds the UI relies on.
const (
	MinLength = 8
	MaxLength = 64
	MinCount  = 10
	MaxCount  = 100
)

// Options controls one password's shape.
type Options struct {
	Length                      int
	Lower, Upper, Digit, Symbol bool
	AvoidAmbig                  bool   // drop 0 O 1 l I
	OneEach                     bool   // guarantee at least one char from each enabled class
	Exclude                     string // extra characters to keep out of the pool
}

// Default is the starting configuration for the generator tab.
func Default() Options {
	return Options{Length: 20, Lower: true, Upper: true, Digit: true, Symbol: true}
}

var (
	// ErrNoClasses means every character class is disabled.
	ErrNoClasses = errors.New("pwgen: no character classes enabled")
	// ErrLength means the requested length is out of range; the message states it.
	ErrLength = fmt.Errorf("pwgen: length must be between %d and %d", MinLength, MaxLength)
)

// classes returns the enabled character pools, ambiguous glyphs removed when
// requested. A class that becomes empty after removal is dropped.
func (o Options) classes() []string {
	var cs []string
	add := func(on bool, set string) {
		if !on {
			return
		}
		if o.AvoidAmbig {
			set = strip(set, ambiguous)
		}
		if o.Exclude != "" {
			set = strip(set, o.Exclude)
		}
		if set != "" {
			cs = append(cs, set)
		}
	}
	add(o.Lower, Lower)
	add(o.Upper, Upper)
	add(o.Digit, Digit)
	add(o.Symbol, Symbol)
	return cs
}

// Generate returns one random password satisfying o.
func Generate(o Options) (string, error) {
	if o.Length < MinLength || o.Length > MaxLength {
		return "", ErrLength
	}
	classes := o.classes()
	if len(classes) == 0 {
		return "", ErrNoClasses
	}
	pool := strings.Join(classes, "")

	out := make([]byte, 0, o.Length)
	// OneEach seeds one char from each class first (only when they all fit).
	if o.OneEach && len(classes) <= o.Length {
		for _, c := range classes {
			ch, err := pick(c)
			if err != nil {
				return "", err
			}
			out = append(out, ch)
		}
	}
	for len(out) < o.Length {
		ch, err := pick(pool)
		if err != nil {
			return "", err
		}
		out = append(out, ch)
	}
	if err := shuffle(out); err != nil { // so the seeded chars aren't always at the front
		return "", err
	}
	return string(out), nil
}

// Many returns n passwords generated from o.
func Many(o Options, n int) ([]string, error) {
	out := make([]string, 0, n)
	for range n {
		p, err := Generate(o)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// PoolSize is the number of distinct characters o draws from.
func (o Options) PoolSize() int {
	n := 0
	for _, c := range o.classes() {
		n += len(c)
	}
	return n
}

// EntropyBits is the password's entropy in bits: length · log2(poolSize). It is a
// deliberate lower bound — it ignores the small boost from OneEach — so the UI
// never overstates strength.
func (o Options) EntropyBits() float64 {
	n := o.PoolSize()
	if n <= 1 || o.Length <= 0 {
		return 0
	}
	return float64(o.Length) * math.Log2(float64(n))
}

// pick returns a uniformly random byte from s using crypto/rand.
func pick(s string) (byte, error) {
	i, err := rand.Int(rand.Reader, big.NewInt(int64(len(s))))
	if err != nil {
		return 0, err
	}
	return s[i.Int64()], nil
}

// shuffle is a Fisher–Yates shuffle backed by crypto/rand.
func shuffle(b []byte) error {
	for i := len(b) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		b[i], b[j.Int64()] = b[j.Int64()], b[i]
	}
	return nil
}

// strip removes every rune of cutset from s.
func strip(s, cutset string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(cutset, r) {
			return -1
		}
		return r
	}, s)
}
