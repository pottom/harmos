package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/edit"
)

// intoFolder parks the tree cursor on a real folder — not a source root, which
// has no identity of its own to rename.
func intoFolder(t *testing.T, m Model) Model {
	t.Helper()
	m = m.expandAll(true)
	for i, tl := range m.visible() {
		if tl.node.id != "" {
			m.tsel = i
			return m
		}
	}
	t.Fatal("no folder in the tree")
	return m
}

// r on a folder edits its name where the name is, and stages a rename.
func TestInlineRenameFolder(t *testing.T) {
	m := intoFolder(t, editModel(t))
	id, was := m.currentFolderID()

	m = up(m, key2("r"))
	if m.edit != editInline {
		t.Fatalf("r should open the inline field, got mode %d", m.edit)
	}
	// The tree is still on screen behind the field — that is the whole point.
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "own") {
		t.Error("the vault should still be visible while renaming")
	}
	if !strings.Contains(view, "-- RENAME --") {
		t.Error("the footer should say which mode this is")
	}

	m.inlineInput.SetValue("")
	m = typeStr(m, "Renamed")
	m = up(m, key2("enter"))

	if m.edit != editNone {
		t.Error("↵ should close the field")
	}
	var found bool
	for _, op := range m.chg.Effective() {
		if op.Kind == edit.RenameGroup && op.Target == id && op.Name == "Renamed" {
			found = true
		}
	}
	if !found {
		t.Errorf("↵ should stage a rename of %q; ops: %v", was, m.chg.Effective())
	}
}

// r on an entry renames its title, and keeps everything else the entry had. A
// rename that blanked the password would be a rename that lost the password.
func TestInlineRenameEntryKeepsTheRest(t *testing.T) {
	m := intoTable(t, editModel(t))
	e := *m.selEntry()

	m = up(m, key2("r"))
	if m.edit != editInline {
		t.Fatalf("r should open the inline field, got mode %d", m.edit)
	}
	m.inlineInput.SetValue("")
	m = typeStr(m, "New title")
	m = up(m, key2("enter"))

	var op *edit.Op
	for _, o := range m.chg.Effective() {
		if o.Kind == edit.EditEntry && o.Target == e.ID {
			cp := o
			op = &cp
		}
	}
	if op == nil {
		t.Fatalf("↵ should stage an edit of %s; ops: %v", e.ID, m.chg.Effective())
	}
	if op.After.Title != "New title" {
		t.Errorf("title = %q, want %q", op.After.Title, "New title")
	}
	if op.After.Password.Reveal() == "" {
		t.Error("the rename dropped the password")
	}
	if op.After.Username != e.Username {
		t.Errorf("username = %q, want %q", op.After.Username, e.Username)
	}
}

// esc leaves the name alone and stages nothing.
func TestInlineRenameEscape(t *testing.T) {
	m := intoFolder(t, editModel(t))
	before := len(m.chg.Effective())

	m = up(m, key2("r"))
	m.inlineInput.SetValue("")
	m = typeStr(m, "Nope")
	m = up(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.edit != editNone {
		t.Error("esc should close the field")
	}
	if got := len(m.chg.Effective()); got != before {
		t.Errorf("esc staged something: %d ops, want %d", got, before)
	}
}

// An empty name is refused, and the field stays open so it can be fixed — losing
// what was typed to a modal that says "invalid" is worse than saying so in place.
func TestInlineRenameRefusesEmpty(t *testing.T) {
	m := intoFolder(t, editModel(t))

	m = up(m, key2("r"))
	m.inlineInput.SetValue("")
	m = up(m, key2("enter"))

	if m.edit != editInline {
		t.Error("an empty name should leave the field open")
	}
	if !strings.Contains(m.flash, "empty") {
		t.Errorf("it should say why: %q", m.flash)
	}
}

// The same name is not a change. Staging one would put a no-op in the review and
// rewrite the entry's modification time for nothing.
func TestInlineRenameUnchangedStagesNothing(t *testing.T) {
	m := intoFolder(t, editModel(t))
	before := len(m.chg.Effective())

	m = up(m, key2("r"))
	m = up(m, key2("enter"))

	if m.edit != editNone {
		t.Error("↵ should close the field")
	}
	if got := len(m.chg.Effective()); got != before {
		t.Errorf("an unchanged name staged something: %d ops, want %d", got, before)
	}
}

// While the field is open, letters are letters. q must not quit and / must not
// open the search — this is the same rule as the modal editor, and the reason it
// is a test is that a rename field lives on a surface where those keys work.
func TestInlineRenameSwallowsHotkeys(t *testing.T) {
	m := intoFolder(t, editModel(t))
	m = up(m, key2("r"))
	m.inlineInput.SetValue("")

	m = typeStr(m, "q/nd")
	if m.edit != editInline {
		t.Fatal("a hotkey escaped the rename field")
	}
	if m.searchMode {
		t.Error("/ opened the search from inside the field")
	}
	if got := m.inlineInput.Value(); got != "q/nd" {
		t.Errorf("the field got %q, want %q", got, "q/nd")
	}
}

// A row staged for deletion cannot be renamed: the two stagings describe
// different futures for the same thing, and the review could not draw both.
func TestInlineRenameRefusedOnADoomedRow(t *testing.T) {
	m := intoFolder(t, editModel(t))
	sel := m.tsel

	m = up(m, key2("d"))
	m.tsel = sel // d moves on; come back to the row it marked
	m = up(m, key2("r"))

	if m.edit == editInline {
		t.Error("a row staged for deletion should not open a rename field")
	}
	if !strings.Contains(m.flash, "deletion") {
		t.Errorf("it should say why: %q", m.flash)
	}
}

// One key, one act. e and r both used to rename a folder, which is two keys for
// one thing and a keymap nobody remembers correctly. e is the form now, and only
// an entry has one — on a folder it says which key does the rename.
func TestEditKeyDoesNotDuplicateRename(t *testing.T) {
	m := intoFolder(t, editModel(t))
	before := len(m.chg.Effective())

	m = up(m, key2("e"))
	if m.edit != editNone {
		t.Errorf("e on a folder should open nothing, got mode %d", m.edit)
	}
	if !strings.Contains(m.flash, "r renames") {
		t.Errorf("it should name the key that does: %q", m.flash)
	}
	if got := len(m.chg.Effective()); got != before {
		t.Errorf("e staged something on a folder: %d ops, want %d", got, before)
	}

	// And r still does it.
	m = up(m, key2("r"))
	if m.edit != editInline {
		t.Fatalf("r should open the inline field, got mode %d (%q)", m.edit, m.flash)
	}
}

// Renaming a folder and then renaming it back leaves nothing staged. The user's
// report: the review showed "Ibasa copy → Ibasa copy" and the tree kept its
// amber pencil, so the session looked dirty when nothing about the file would
// change.
//
// The inline field's own "unchanged" guard cannot catch this — it compares
// against the name on the row, which is the interim one. Only the reduction
// knows where the round trip started.
func TestRenamingBackLeavesNothingStaged(t *testing.T) {
	m := intoFolder(t, editModel(t))
	id, original := m.currentFolderID()

	rename := func(m Model, to string) Model {
		t.Helper()
		m = up(m, key2("r"))
		if m.edit != editInline {
			t.Fatalf("r should open the inline field, got mode %d (%q)", m.edit, m.flash)
		}
		m.inlineInput.SetValue("")
		m = typeStr(m, to)
		return up(m, key2("enter"))
	}

	m = rename(m, "something else")
	if m.dirtyCount() == 0 {
		t.Fatal("the first rename should stage something")
	}

	m = rename(m, original)
	if n := m.dirtyCount(); n != 0 {
		t.Errorf("renaming back to %q left %d change(s) staged: %v", original, n, m.chg.Effective())
	}
	if st := m.chg.StateOf(id); st != edit.Unchanged {
		t.Errorf("the row still reads as %v", st)
	}
	if out := ansi.Strip(m.switchTab(tabChanges).View()); !strings.Contains(out, "nothing pending") {
		t.Errorf("the review should be empty:\n%s", out)
	}
}

// And the same for a move: back where it came from is not a move.
func TestMovingBackLeavesNothingStaged(t *testing.T) {
	// The richer fixture: editModel has one folder with entries in it, so there
	// is nowhere to move to and nothing to prove.
	m, _ := walkModel(t)
	m = onEntry(t, m.expandAll(true), "db", "db-prod")
	e := m.selEntry()
	if e == nil {
		t.Fatal("no entry under the cursor")
	}
	home := e.GroupID

	m = up(m, key2("m"))
	if m.edit != editMove || len(m.moveDests) == 0 {
		t.Fatalf("m should open the picker with somewhere to go, got mode %d (%q)", m.edit, m.flash)
	}
	m = pickDestination(t, m, "Net")
	m = up(m, key2("enter"))
	if m.dirtyCount() == 0 {
		t.Fatal("the first move should stage something")
	}

	// Back again: the original folder is a destination now, because it is no
	// longer where the projection says the entry lives.
	m = up(m, key2("m"))
	var back int
	for i, d := range m.moveDests {
		if d.id == home {
			back = i
		}
	}
	m.moveSel = back
	m = up(m, key2("enter"))

	if n := m.dirtyCount(); n != 0 {
		t.Errorf("moving back to where it started left %d change(s): %v", n, m.chg.Effective())
	}
}
