package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/theme"
)

// A reusable labelled form.
//
// It used to be five fields on Model wired directly to the Settings tab's mode
// enum, which meant a second form — for editing an entry — would have been a
// second copy of the same focus arithmetic and rendering. This is a value type,
// like the model that holds it, so a caller keeps the result of Update the same
// way Bubble Tea already works.
//
// What it deliberately does not have yet: dynamically added rows, for an entry's
// custom fields. That lands with the editor that needs it rather than being
// built speculatively here.

type fieldKind int

const (
	fText   fieldKind = iota // a single-line value
	fMasked                  // a single-line secret: echoed as bullets
	fMulti                   // a multi-line value, e.g. notes
	fToggle                  // a choice between fixed options
)

type field struct {
	key   string
	label string
	kind  fieldKind

	input textinput.Model // fText, fMasked
	area  textarea.Model  // fMulti

	options []string // fToggle
	choice  int

	// validate rejects a value when the field is left or the form submitted.
	// Per field rather than in one submit function, so a form for a different
	// thing does not have to grow another branch of somebody else's rules.
	validate func(string) error

	// onToggle is called when a toggle changes, for the case where the choice
	// changes which fields exist at all.
	onToggle func(f form, choice int) form
}

type form struct {
	fields []field
	focus  int    // index into fields; len(fields) is the submit button
	status string // validation error or last result, shown in the footer
	button string // submit button label
	width  int    // input width, derived from the pane
	reveal bool   // temporarily show masked values
}

// newForm builds a form. The width is the pane's, not a constant: the package
// contract is that nothing hardcodes a column, and a form squeezed into a narrow
// terminal should shrink rather than overflow.
func newForm(button string, width int, fields ...field) form {
	f := form{fields: fields, button: button}
	return f.resize(width)
}

// clone copies the field slice.
//
// form is a value type so that Bubble Tea's copy-on-update model works, but a
// slice header copies by reference — without this, mutating one copy's fields
// would reach into every other copy, including the "before" state a caller is
// holding on to. Every method that changes a field goes through here first.
func (f form) clone() form {
	fields := make([]field, len(f.fields))
	copy(fields, f.fields)
	f.fields = fields
	return f
}

// resize adapts every input to the available width.
func (f form) resize(width int) form {
	f = f.clone()
	f.width = width
	inner := max(12, width-labelWidth-6)
	for i := range f.fields {
		switch f.fields[i].kind {
		case fText, fMasked:
			f.fields[i].input.Width = inner
		case fMulti:
			f.fields[i].area.SetWidth(inner)
		}
	}
	return f.refocus()
}

const labelWidth = 10

// textField is a plain single-line field.
func textField(key, label, placeholder, value string) field {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.SetValue(value)
	return field{key: key, label: label, kind: fText, input: ti}
}

// maskedField is a single-line secret. The value is echoed as bullets, so a
// password is not left legible on a screen somebody else can see.
func maskedField(key, label, value string) field {
	ti := textinput.New()
	ti.Prompt = ""
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.SetValue(value)
	return field{key: key, label: label, kind: fMasked, input: ti}
}

// multiField is a multi-line value.
func multiField(key, label, value string, height int) field {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.SetHeight(height)
	ta.SetValue(value)
	return field{key: key, label: label, kind: fMulti, area: ta}
}

// toggleField chooses between fixed options.
func toggleField(key, label string, options []string, choice int) field {
	return field{key: key, label: label, kind: fToggle, options: options, choice: choice}
}

func (f field) withValidation(v func(string) error) field { f.validate = v; return f }

func (f field) withToggleHandler(h func(form, int) form) field { f.onToggle = h; return f }

// value is the field's current text. A toggle's value is its chosen option, so
// callers can read every field the same way.
func (f field) value() string {
	switch f.kind {
	case fMulti:
		return f.area.Value()
	case fToggle:
		if f.choice < len(f.options) {
			return f.options[f.choice]
		}
		return ""
	}
	return f.input.Value()
}

// Value is the trimmed value of a field by key.
func (f form) Value(key string) string {
	for _, fl := range f.fields {
		if fl.key == key {
			return strings.TrimSpace(fl.value())
		}
	}
	return ""
}

// Raw is Value without trimming, for fields where surrounding whitespace is
// content rather than noise.
func (f form) Raw(key string) string {
	for _, fl := range f.fields {
		if fl.key == key {
			return fl.value()
		}
	}
	return ""
}

// Choice is the selected index of a toggle.
func (f form) Choice(key string) int {
	for _, fl := range f.fields {
		if fl.key == key {
			return fl.choice
		}
	}
	return 0
}

// OnButton reports whether the cursor is on the submit button.
func (f form) OnButton() bool { return f.focus == len(f.fields) }

// Validate runs every field's rule, stopping at the first failure and moving the
// cursor there — so the message and the cursor agree about which field is wrong.
func (f form) Validate() (form, bool) {
	for i, fl := range f.fields {
		if fl.validate == nil {
			continue
		}
		if err := fl.validate(strings.TrimSpace(fl.value())); err != nil {
			f.status = err.Error()
			f.focus = i
			return f.refocus(), false
		}
	}
	f.status = ""
	return f, true
}

// refocus gives the keyboard to the focused field and takes it from the rest.
func (f form) refocus() form {
	f = f.clone()
	for i := range f.fields {
		focused := i == f.focus
		switch f.fields[i].kind {
		case fText, fMasked:
			if focused {
				f.fields[i].input.Focus()
			} else {
				f.fields[i].input.Blur()
			}
		case fMulti:
			if focused {
				f.fields[i].area.Focus()
			} else {
				f.fields[i].area.Blur()
			}
		}
	}
	return f
}

// Update handles one key. submitted reports that the user asked to submit; the
// caller decides what that means.
func (f form) Update(key string, msg tea.KeyMsg) (out form, cmd tea.Cmd, submitted bool) {
	stops := len(f.fields) + 1 // the fields, plus the button

	f = f.clone()

	// A multi-line field keeps enter for itself — otherwise notes could not
	// contain a second line — so it is left with tab to move on.
	inMulti := f.focus < len(f.fields) && f.fields[f.focus].kind == fMulti

	switch key {
	case "tab":
		f.focus = (f.focus + 1) % stops
		return f.refocus(), nil, false
	case "shift+tab":
		f.focus = (f.focus - 1 + stops) % stops
		return f.refocus(), nil, false
	case "down":
		if !inMulti {
			f.focus = (f.focus + 1) % stops
			return f.refocus(), nil, false
		}
	case "up":
		if !inMulti {
			f.focus = (f.focus - 1 + stops) % stops
			return f.refocus(), nil, false
		}
	case "enter":
		if !inMulti {
			return f, nil, true
		}
	case "ctrl+r":
		// Reveal, for checking a password you have just typed. Deliberately a
		// hold-to-look toggle rather than a persistent setting.
		f.reveal = !f.reveal
		for i := range f.fields {
			if f.fields[i].kind != fMasked {
				continue
			}
			if f.reveal {
				f.fields[i].input.EchoMode = textinput.EchoNormal
			} else {
				f.fields[i].input.EchoMode = textinput.EchoPassword
			}
		}
		return f, nil, false
	}

	if f.focus >= len(f.fields) {
		return f, nil, false // on the button; nothing to type into
	}

	fl := &f.fields[f.focus]
	if fl.kind == fToggle {
		switch key {
		case "left", "right", " ":
			if len(fl.options) > 0 {
				step := 1
				if key == "left" {
					step = len(fl.options) - 1
				}
				fl.choice = (fl.choice + step) % len(fl.options)
				if fl.onToggle != nil {
					return fl.onToggle(f, fl.choice), nil, false
				}
			}
		}
		return f, nil, false
	}

	switch fl.kind {
	case fMulti:
		fl.area, cmd = fl.area.Update(msg)
	default:
		fl.input, cmd = fl.input.Update(msg)
	}
	return f, cmd, false
}

// View renders the form's rows. The caller frames them.
func (f form) View() []string {
	var lines []string
	for i, fl := range f.fields {
		selected := i == f.focus

		label := theme.Dimmed.Render(pad(fl.label, labelWidth))
		if selected {
			label = theme.Hi.Render(pad(fl.label, labelWidth))
		}
		cursor := "  "
		if selected {
			cursor = theme.Acc.Render("▸ ")
		}

		switch fl.kind {
		case fToggle:
			var parts []string
			for j, opt := range fl.options {
				if j == fl.choice {
					parts = append(parts, theme.Acc.Render("["+opt+"]"))
				} else {
					parts = append(parts, theme.Faded.Render(opt))
				}
			}
			lines = append(lines, cursor+label+"  "+strings.Join(parts, "  ")+
				theme.Faded.Render("   ←/→ to change"))
		case fMulti:
			lines = append(lines, cursor+label)
			for line := range strings.SplitSeq(fl.area.View(), "\n") {
				lines = append(lines, "    "+line)
			}
		default:
			lines = append(lines, cursor+label+"  "+fl.input.View())
		}
	}
	lines = append(lines, "", "  "+button(f.button, false, f.OnButton()))
	return lines
}

// Hint is the footer text: the validation message when there is one, else the
// keys that apply here.
func (f form) Hint() string {
	if f.status != "" {
		return theme.Bad.Render(f.status)
	}
	keys := "↑↓/tab move · ↵ save · esc cancel"
	if f.focus < len(f.fields) {
		switch f.fields[f.focus].kind {
		case fMasked:
			keys = "^r reveal · ↑↓/tab move · ↵ save · esc cancel"
		case fMulti:
			keys = "tab leaves the field · ↵ new line · esc cancel"
		}
	}
	return theme.Faded.Render(keys)
}

// withStatus sets the message shown instead of the key hints.
func (f form) withStatus(s string) form { f.status = s; return f }
