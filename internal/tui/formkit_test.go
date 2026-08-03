package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func typeForm(f form, s string) form {
	for _, r := range s {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		f, _, _ = f.Update(string(r), msg)
	}
	return f
}

// press sends a key, with the message type the component would really receive —
// a textarea decides what to do from the message, not from our label for it.
func press(f form, key string) (form, bool) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		msg = tea.KeyMsg{Type: tea.KeyShiftTab}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+r":
		msg = tea.KeyMsg{Type: tea.KeyCtrlR}
	}
	f, _, submitted := f.Update(key, msg)
	return f, submitted
}

func TestFormMovesAndSubmits(t *testing.T) {
	f := newForm("Save", 60,
		textField("a", "A", "", ""),
		textField("b", "B", "", ""),
	)

	f = typeForm(f, "first")
	f, _ = press(f, "tab")
	f = typeForm(f, "second")

	if got := f.Value("a"); got != "first" {
		t.Errorf("a = %q", got)
	}
	if got := f.Value("b"); got != "second" {
		t.Errorf("b = %q", got)
	}

	// tab from the last field lands on the button, and wraps back round.
	f, _ = press(f, "tab")
	if !f.OnButton() {
		t.Error("tab from the last field should land on the button")
	}
	f, submitted := press(f, "enter")
	if !submitted {
		t.Error("enter should submit")
	}
	f, _ = press(f, "tab")
	if f.OnButton() {
		t.Error("tab from the button should wrap round to the first field")
	}
}

// A secret must not be legible on screen just because it is being typed.
func TestMaskedFieldIsNotLegible(t *testing.T) {
	f := newForm("Save", 60, maskedField("pw", "Password", ""))
	f = typeForm(f, "hunter2")

	rendered := ansi.Strip(strings.Join(viewLines(f), "\n"))
	if strings.Contains(rendered, "hunter2") {
		t.Errorf("the masked value is legible on screen:\n%s", rendered)
	}
	if !strings.Contains(rendered, "•") {
		t.Errorf("a masked field should show bullets:\n%s", rendered)
	}
	// The value is still readable by the program, or the form would be useless.
	if f.Value("pw") != "hunter2" {
		t.Errorf("value = %q", f.Value("pw"))
	}

	// Reveal, for checking what you just typed.
	f, _ = press(f, "ctrl+r")
	if !strings.Contains(ansi.Strip(strings.Join(viewLines(f), "\n")), "hunter2") {
		t.Error("ctrl+r should reveal")
	}
	f, _ = press(f, "ctrl+r")
	if strings.Contains(ansi.Strip(strings.Join(viewLines(f), "\n")), "hunter2") {
		t.Error("ctrl+r again should hide it")
	}
}

// A notes field has to be able to contain a second line, so enter belongs to the
// field rather than to the form.
func TestMultiLineFieldKeepsEnter(t *testing.T) {
	f := newForm("Save", 60, multiField("notes", "Notes", "", 3))

	f = typeForm(f, "one")
	f, submitted := press(f, "enter")
	if submitted {
		t.Fatal("enter inside a multi-line field must not submit the form")
	}
	f = typeForm(f, "two")

	if got := f.Raw("notes"); !strings.Contains(got, "\n") {
		t.Errorf("notes = %q, want two lines", got)
	}
	// tab is how you leave it.
	f, _ = press(f, "tab")
	if !f.OnButton() {
		t.Error("tab should leave a multi-line field")
	}
	if f.Hint() == "" {
		t.Error("the hint should say how to leave the field")
	}
}

func TestToggleField(t *testing.T) {
	f := newForm("Save", 60, toggleField("type", "Type", []string{"kdbx", "pps"}, 0))

	if f.Value("type") != "kdbx" {
		t.Errorf("initial = %q", f.Value("type"))
	}
	f, _ = press(f, "right")
	if f.Value("type") != "pps" || f.Choice("type") != 1 {
		t.Errorf("after right = %q (%d)", f.Value("type"), f.Choice("type"))
	}
	f, _ = press(f, "left")
	if f.Value("type") != "kdbx" {
		t.Errorf("after left = %q", f.Value("type"))
	}
	// and it wraps
	f, _ = press(f, "left")
	if f.Value("type") != "pps" {
		t.Errorf("left from the first option should wrap, got %q", f.Value("type"))
	}
}

// Validation belongs to the field, and the cursor moves to whatever failed —
// otherwise the message names one field while the cursor sits on another.
func TestValidationMovesTheCursorToTheProblem(t *testing.T) {
	f := newForm("Save", 60,
		textField("a", "A", "", "filled"),
		textField("b", "B", "", "").withValidation(func(v string) error {
			if v == "" {
				return errors.New("b is required")
			}
			return nil
		}),
	)

	f, ok := f.Validate()
	if ok {
		t.Fatal("validation should have failed")
	}
	if f.focus != 1 {
		t.Errorf("cursor is on field %d, want the one that failed (1)", f.focus)
	}
	if !strings.Contains(ansi.Strip(f.Hint()), "b is required") {
		t.Errorf("hint = %q", ansi.Strip(f.Hint()))
	}

	f = typeForm(f, "now filled")
	if _, ok := f.Validate(); !ok {
		t.Error("validation should pass once the field is filled")
	}
}

// Inputs size to the pane. The package rule is that nothing hardcodes a column,
// and a form in a narrow terminal should shrink rather than overflow.
func TestFormResizesToThePane(t *testing.T) {
	wide := newForm("Save", 100, textField("a", "A", "", ""))
	narrow := wide.resize(40)

	if narrow.fields[0].input.Width >= wide.fields[0].input.Width {
		t.Errorf("input width %d did not shrink from %d",
			narrow.fields[0].input.Width, wide.fields[0].input.Width)
	}
	tiny := wide.resize(10)
	if tiny.fields[0].input.Width < 1 {
		t.Errorf("a tiny pane should still leave a usable input, got %d", tiny.fields[0].input.Width)
	}
}

// The Settings source form is built on the component; switching the type must
// still rebuild the field list and carry the name across.
func TestSourceFormTypeToggleKeepsTheName(t *testing.T) {
	f := buildSourceForm(false, false, nil, 60)

	f, _ = press(f, "tab") // onto Name
	f = typeForm(f, "work")

	f.focus = 0 // back to the toggle
	f = f.refocus()
	f, _ = press(f, "right")

	if f.Value("name") != "work" {
		t.Errorf("the name should survive a type change, got %q", f.Value("name"))
	}
	if f.Value("url") == "" && f.Choice("type") == 1 {
		// the pps branch has url/user/cache/ca rather than path/keyfile
		var keys []string
		for _, fl := range f.fields {
			keys = append(keys, fl.key)
		}
		if !contains(keys, "url") {
			t.Errorf("switching to pps should offer the pps fields, got %v", keys)
		}
	}
}

func contains(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}

// viewLines is form.View's lines alone, for tests that do not care where the
// focus sits.
func viewLines(f form) []string {
	lines, _ := f.View()
	return lines
}
