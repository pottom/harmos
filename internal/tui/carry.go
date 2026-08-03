package tui

import (
	"strings"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/theme"
)

// Moving, in the tree.
//
// m used to open a list of destinations in a window over the vault — the same
// objection the rename and the new folder already answered: a screen that
// covers the tree in order to ask a question about the tree. Where something
// should go is a question the tree itself is the answer to.
//
// So m picks the row up and the tree stays live. Navigate to the folder you
// want with the keys you browse with, and ↵ puts it there; esc puts it back.
// It is a drag, done with the keyboard, and the carried row and the folder
// under the cursor both say what they are throughout.

// grabForMove picks up the row under the cursor.
//
// It comes back to the tree first. The key works from a search result and from
// the entry detail, where the tree is either not the live surface or not on
// screen at all — and a move you steer with the tree has to start with the
// thing visible in it.
func (m Model) grabForMove(target string, isFolder bool) Model {
	// No way-out guard here, unlike e and r. Editing or renaming something that
	// is about to be deleted describes two futures for one thing; moving it out
	// of the folder that is going is the one change that is *about* the deletion
	// — it is how you keep one thing from a folder you are removing.
	if m.handles[m.editSource] == nil {
		m.flash = "this source is not open for writing"
		return m
	}

	m.detail = false
	if m.showResults() {
		m.input.SetValue("")
		m.results, m.sel, m.searchMode = nil, 0, false
	}
	m = m.revealTarget(target, isFolder)
	// After the tree cursor, not before: revealing an entry leaves the focus in
	// its folder's table, and the tree is what steers a move.
	m.focus = 0

	m.edit = editCarry
	m.editTarget = target
	m.editFolderTarget = isFolder
	m.carryFrom = m.homeOf(target)
	m.flash = "moving " + m.nameOfTarget(target, isFolder) + " — pick a folder, ↵ drops it there"
	return m
}

// carryKeys are the keys that still mean what they mean while something is
// held: everything you browse the tree with, so choosing a destination is
// choosing it the way you would find it any other day.
var carryKeys = map[string]bool{
	"up": true, "down": true, "left": true, "right": true,
	"ctrl+p": true, "ctrl+n": true, "pgup": true, "pgdown": true,
	"home": true, "end": true, "z": true, "Z": true,
	"shift+left": true, "shift+right": true, "ctrl+b": true,
}

// updateCarry answers the two keys the mode owns and reports whether the rest
// should fall through to the ordinary tree handling.
func (m Model) updateCarry(key string) (Model, bool) {
	switch key {
	case "esc":
		m.edit = editNone
		m.flash = "put back — nothing moved"
		return m, false
	case "enter":
		return m.dropHere(), false
	}
	if carryKeys[key] {
		return m, true // the tree is still the tree
	}
	// Anything else would act on a row while another one is in hand, which is
	// two things at once and no way to tell which happened.
	m.flash = "still moving " + m.nameOfTarget(m.editTarget, m.editFolderTarget) +
		" — ↵ drops it here, esc puts it back"
	return m, false
}

// dropHere stages the move to whatever the cursor is on, or says why it cannot.
func (m Model) dropHere() Model {
	dest, ok := m.destUnderCursor()
	if !ok {
		m.flash = "that row is not a folder — move the cursor onto one"
		return m
	}
	if why, ok := m.canDropInto(dest); !ok {
		m.flash = why
		return m
	}

	kind := edit.MoveEntry
	if m.editFolderTarget {
		kind = edit.MoveGroup
	}
	name := m.nameOfTarget(m.editTarget, m.editFolderTarget)
	m.chg, _ = m.chg.Add(edit.Op{
		Kind: kind, Source: m.editSource, Target: m.editTarget,
		Name: name, Was: m.carryFrom, Parent: dest,
	})
	m.edit = editNone
	m.lastMoveTo = dest
	m.flash = "staged: " + name + " → " + m.folderName(m.editSource, dest) +
		" · nothing is written until you save"
	return m.restage()
}

// destUnderCursor is the folder the tree cursor is on, as a destination.
//
// A source row has no identity of its own — the tree draws the source there —
// but it stands for the root group, which is a folder like any other and the
// one place nothing could be moved to before.
func (m Model) destUnderCursor() (string, bool) {
	flat := m.visible()
	if m.focus != 0 || m.tsel >= len(flat) {
		return "", false
	}
	n := flat[m.tsel].node
	if n.source != m.editSource {
		return "", false
	}
	if n.id != "" {
		return n.id, true
	}
	if h := m.handles[n.source]; h != nil {
		if root := h.RootGroupID(); root != "" {
			return root, true
		}
	}
	return "", false
}

// canDropInto is whether the carried thing may go into dest, and why not.
//
// The same four rules the destination list applied, asked one folder at a time
// so the answer can be shown as the cursor passes over it rather than by
// leaving the folder off a list with no explanation.
func (m Model) canDropInto(dest string) (string, bool) {
	what := m.nameOfTarget(m.editTarget, m.editFolderTarget)
	switch dest {
	case m.editTarget:
		return "a folder cannot go inside itself", false
	case m.carryFrom:
		return what + " is already here", false
	}
	if m.editFolderTarget {
		if f, ok := m.folderByID(m.editTarget); ok {
			if d, ok := m.folderByID(dest); ok && strings.HasPrefix(d.Path, f.Path+"/") {
				return "that is inside " + what + " — a folder cannot go into its own contents", false
			}
		}
	}
	if folder, doomed := m.doomedParent(dest); doomed {
		return "that folder is going with " + folder + " — undo the deletion first", false
	}
	if _, doomed := m.stagedDeletion(dest); doomed {
		return "that folder is staged for deletion", false
	}
	return "", true
}

// carriedRow reports whether this row is the one in hand, so the tree can say so.
func (m Model) carriedRow(id string) bool {
	return m.edit == editCarry && id != "" && id == m.editTarget
}

// carryHint is the footer while something is held: what is in hand, where it
// would land, and — when it would not — why.
func (m Model) carryHint() string {
	what := m.nameOfTarget(m.editTarget, m.editFolderTarget)
	mode := theme.Noted.Render("-- MOVING --")

	dest, ok := m.destUnderCursor()
	switch {
	case !ok:
		return mode + "  " + theme.Strong.Render(what) +
			theme.Faded.Render("  ·  move onto a folder  ·  esc puts it back")
	case func() bool { _, can := m.canDropInto(dest); return can }():
		return mode + "  " + theme.Strong.Render(what) + theme.Noted.Render(" → ") +
			theme.Strong.Render(m.folderName(m.editSource, dest)) +
			theme.Faded.Render("  ·  ↵ drop  ·  esc puts it back")
	default:
		why, _ := m.canDropInto(dest)
		return mode + "  " + theme.Strong.Render(what) + "  " +
			theme.Bad.Render(why) + theme.Faded.Render("  ·  esc puts it back")
	}
}

// carryTag is the name of what is being carried, for the row under the cursor:
// the answer to "where would this go" put where it would go.
//
// Its colour is the other answer. Amber is a folder that will take it; rust is
// one that will not, which the footer spells out — at the cursor there is only
// room to say yes or no, and a glyph and a colour say it without being read.
func (m Model) carryTag(w int) (plain, styled string) {
	what := m.nameOfTarget(m.editTarget, m.editFolderTarget)
	if what == "" {
		return "", ""
	}
	st := theme.Noted
	if dest, ok := m.destUnderCursor(); !ok {
		st = theme.Bad
	} else if _, can := m.canDropInto(dest); !can {
		st = theme.Bad
	}

	// Half the row at most: the folder being landed on has to stay readable, or
	// the tag has answered a question the reader can no longer ask.
	tag := "  " + ic().moved + " " + trunc(what, max(3, w/2))
	return tag, st.Render(tag)
}
