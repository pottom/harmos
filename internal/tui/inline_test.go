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

// e on a folder edits its name where the name is, and stages a rename. There is
// only one editing key: an entry has fields and gets the form, a folder is its
// name and gets the row.
func TestInlineRenameFolder(t *testing.T) {
	m := intoFolder(t, editModel(t))
	id, was := m.currentFolderID()

	m = up(m, key2("e"))
	if m.edit != editInline {
		t.Fatalf("e on a folder should open the inline field, got mode %d", m.edit)
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

// esc leaves the name alone and stages nothing.
func TestInlineRenameEscape(t *testing.T) {
	m := intoFolder(t, editModel(t))
	before := len(m.chg.Effective())

	m = up(m, key2("e"))
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

	m = up(m, key2("e"))
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

	m = up(m, key2("e"))
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
	m = up(m, key2("e"))
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
	m = up(m, key2("e"))

	if m.edit == editInline {
		t.Error("a row staged for deletion should not open a rename field")
	}
	if !strings.Contains(m.flash, "deletion") {
		t.Errorf("it should say why: %q", m.flash)
	}
}
