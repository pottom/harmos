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

// saveChoices: writing is what the user asked for and a backup is taken first,
// so Write leads — unless the set contains a permanent delete, the one thing in
// harmos no backup inside the file can undo. Then the cursor starts on Cancel
// and the caller says why, because that is the case where an enter pressed out
// of habit costs something real.
func saveChoices(permanent bool) ([]confirmChoice, int) {
	choices := []confirmChoice{{label: "Write", danger: permanent}, {label: "Cancel"}}
	if permanent {
		return choices, 1
	}
	return choices, 0
}

// quitChoices: the staged work is the thing worth protecting, so saving leads
// and discarding is marked as the destructive answer it is.
func quitChoices() []confirmChoice {
	return []confirmChoice{{label: "Save and quit"}, {label: "Discard and quit", danger: true}, {label: "Stay"}}
}

// conflictChoices: reloading keeps the staged changes and shows what the file
// now says, which is the only answer that loses nothing.
func conflictChoices() []confirmChoice {
	return []confirmChoice{{label: "Reload"}, {label: "Leave it"}}
}
