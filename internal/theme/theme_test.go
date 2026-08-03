package theme

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestApplyAndBuiltins(t *testing.T) {
	Apply(Nord)
	if Accent.Dark != "#88c0d0" {
		t.Errorf("nord accent dark = %q, want #88c0d0", Accent.Dark)
	}
	Apply(Charm)
	if Accent.Dark != "#8b6dff" {
		t.Errorf("charm accent dark = %q, want #8b6dff", Accent.Dark)
	}
	if _, ok := Builtin("dracula"); !ok {
		t.Error("dracula should be a built-in")
	}
	if _, ok := Builtin("nope"); ok {
		t.Error("unknown theme should not be a built-in")
	}
	if len(Names()) < 5 {
		t.Errorf("want >=5 built-in themes, got %d", len(Names()))
	}
}

// adaptive fills a missing light/dark from the other.
func TestAdaptiveFallback(t *testing.T) {
	c := adaptive(token{Dark: "#123456"})
	if c.Light != "#123456" || c.Dark != "#123456" {
		t.Errorf("single-value token should repeat: %+v", c)
	}
}

// Every built-in must fill every token.
//
// Without this, adding a token to the struct and forgetting one theme yields an
// empty colour string — which lipgloss renders as "no colour" rather than as an
// error, so the theme quietly loses a distinction and nobody notices until a
// screenshot looks wrong. Reflection is used deliberately: a hand-written list
// of tokens would need remembering too, which is the thing that just failed.
func TestEveryBuiltinFillsEveryToken(t *testing.T) {
	tokenType := reflect.TypeOf(token{})

	for _, name := range Names() {
		th, ok := Builtin(name)
		if !ok {
			t.Fatalf("Names() lists %q but Builtin does not know it", name)
		}
		v := reflect.ValueOf(th)
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if f.Type != tokenType {
				continue // Name, and anything else that is not a colour
			}
			tok := v.Field(i).Interface().(token)
			if tok.Light == "" && tok.Dark == "" {
				t.Errorf("theme %q has no colour for %s", name, f.Name)
			}
		}
	}
}

// A token that resolves to an empty colour would render as the terminal default,
// silently undoing whatever distinction it was added to make.
func TestApplyLeavesNoEmptyToken(t *testing.T) {
	defer Apply(Charm)

	for _, name := range Names() {
		th, _ := Builtin(name)
		Apply(th)
		for label, c := range map[string]lipgloss.AdaptiveColor{
			"Accent": Accent, "AccentHi": AccentHi, "Steel": Steel,
			"Dim": Dim, "Faint": Faint, "OK": OK, "Warn": Warn,
			"Note": Note, "Writable": Writable, "SelBg": SelBg,
		} {
			if c.Light == "" || c.Dark == "" {
				t.Errorf("theme %q leaves %s empty after Apply: %+v", name, label, c)
			}
		}
	}
}

// A custom TOML theme written before a token existed has no value for it.
// lipgloss treats an empty colour as "the terminal default" rather than as an
// error, so without a fallback such a theme would silently lose the distinction.
func TestMissingTokenFallsBack(t *testing.T) {
	defer Apply(Charm)

	custom := Theme{
		Name:     "old-custom",
		Accent:   token{"#112233", "#445566"},
		AccentHi: token{"#112233", "#445566"},
		Steel:    token{"#112233", "#445566"},
		Dim:      token{"#112233", "#445566"},
		Faint:    token{"#112233", "#445566"},
		OK:       token{"#112233", "#445566"},
		Warn:     token{"#112233", "#445566"},
		Note:     token{"#abcdef", "#abcdef"},
		SelBg:    token{"#112233", "#445566"},
		// Writable deliberately unset.
	}
	Apply(custom)

	if Writable.Light == "" || Writable.Dark == "" {
		t.Fatalf("a missing token should fall back, got %+v", Writable)
	}
	if Writable.Light != "#abcdef" {
		t.Errorf("it should fall back to the nearest neighbour (Note), got %q", Writable.Light)
	}

	// And with neither, it still resolves rather than rendering as nothing.
	custom.Note = token{}
	Apply(custom)
	if Writable.Light == "" {
		t.Error("with no neighbour either, it should still resolve to something")
	}
}

// Two tokens that carry different meanings must not be the same colour.
//
// A theme is a set of promises: rust means removal, teal means new, amber means
// changed, the accent means focus, and the faint tokens mean "skip this". Where
// two of them collide the promise breaks — a revealed password rendering in the
// "modified" colour, an unlocked source's whole subtree reading as staged for
// deletion, or a faint marker vanishing into the selection it is drawn on.
func TestNoBuiltinCollidesTwoMeanings(t *testing.T) {
	// Pairs whose colours are load-bearing against each other. Deleted and
	// Purged deliberately share Warn — they are two shapes of one act, told
	// apart by glyph and weight — so that pair is not here.
	pairs := []struct{ a, b string }{
		{"AccentHi", "Note"}, // a revealed secret vs "modified"
		{"AccentHi", "Warn"}, // a revealed secret vs danger
		{"Writable", "Warn"}, // "you can edit here" vs "this is going"
		{"Writable", "OK"},   // vs "new"
		{"Note", "OK"},       // "changed" vs "new"
		{"Note", "Warn"},     // "changed" vs "going"
		{"Faint", "SelBg"},   // secondary text vs the row it sits on
		{"Accent", "Note"},   // focus vs "changed"
	}

	for _, name := range Names() {
		th, ok := Builtin(name)
		if !ok {
			t.Fatalf("Names() offers %q and Builtin does not have it", name)
		}
		by := map[string]token{
			"Accent": th.Accent, "AccentHi": th.AccentHi, "Steel": th.Steel,
			"Dim": th.Dim, "Faint": th.Faint, "OK": th.OK, "Warn": th.Warn,
			"Note": th.Note, "Writable": th.Writable, "SelBg": th.SelBg,
		}
		for _, p := range pairs {
			a, b := by[p.a], by[p.b]
			if a.Light == b.Light {
				t.Errorf("%s light: %s and %s are both %s", name, p.a, p.b, a.Light)
			}
			if a.Dark == b.Dark {
				t.Errorf("%s dark: %s and %s are both %s", name, p.a, p.b, a.Dark)
			}
		}
	}
}
