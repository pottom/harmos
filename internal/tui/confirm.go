package tui

import (
	"strings"

	"github.com/pottom/harmos/internal/theme"
)

// A confirmation's buttons.
//
// The keys were there before this — enter has always meant "go ahead" on these
// modals — but nothing on screen said so, and a shortcut you have to try in
// order to discover is not much of a shortcut. Buttons say which action is the
// default, so the common answer is one keypress and the reader knows it before
// pressing anything.
//
// The safe choice is the default everywhere except where the action is
// irreversible; there, being one careless enter away from it is worse than
// making somebody move the cursor. See confirmButtons for who gets which.

type confirmChoice struct {
	label  string
	danger bool
}

// confirmButtons renders a row of buttons with one of them focused, plus the
// key hints that still work.
func confirmButtons(choices []confirmChoice, sel int, keys string) []string {
	var rendered []string
	for i, c := range choices {
		rendered = append(rendered, button(c.label, c.danger, i == sel))
	}
	return []string{
		"  " + strings.Join(rendered, "  "),
		"",
		"  " + theme.Faded.Render(keys),
	}
}

// confirmNav moves between buttons. It reports whether it took the key, so the
// caller keeps its own shortcuts.
func confirmNav(key string, sel, n int) (int, bool) {
	switch key {
	case "left", "shift+tab":
		return (sel - 1 + n) % n, true
	case "right", "tab":
		return (sel + 1) % n, true
	}
	return sel, false
}

// unlockChoices: unlocking is reversible — ^w locks it again, and nothing is
// written until an explicit save — so the default is the action the user came
// for rather than the timid one.
func unlockChoices() []confirmChoice {
	return []confirmChoice{{label: "Unlock"}, {label: "Cancel"}}
}
