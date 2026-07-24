package theme

import "testing"

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
