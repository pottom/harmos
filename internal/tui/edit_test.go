package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The entry editor, walked the way somebody uses it.
//
// Written after a hand audit found two things: tab never reached the custom
// field list, so a field could be given a name and never a value — and with it
// the two keys that act on the focused row, which the form advertises. And e
// opened happily on a row r refused to rename, so the two keys disagreed about
// whether something on its way out could still be changed.

func editorModel(t *testing.T) Model {
	t.Helper()
	m, _ := walkModel(t)
	return m.expandAll(true)
}

func openEditor(t *testing.T, m Model) Model {
	t.Helper()
	m = up(m, key2("e"))
	if m.edit != editEntry {
		t.Fatalf("e should open the editor, got mode %d (%q)", m.edit, m.flash)
	}
	return m
}

// e opens on every surface an entry can be selected on, and never on a folder —
// a folder is its name, which r edits on the row.
func TestEditorOpensFromEverySurface(t *testing.T) {
	t.Run("entry in the table", func(t *testing.T) {
		openEditor(t, onEntry(t, editorModel(t), "db", "db-prod"))
	})
	t.Run("a search result", func(t *testing.T) {
		openEditor(t, searching(t, editorModel(t), "router"))
	})
	t.Run("the entry detail", func(t *testing.T) {
		openEditor(t, up(onEntry(t, editorModel(t), "db", "db-prod"), tea.KeyMsg{Type: tea.KeyRight}))
	})
	t.Run("a folder points at r", func(t *testing.T) {
		m := up(onRow(t, editorModel(t), "db"), key2("e"))
		if m.edit != editNone {
			t.Errorf("e on a folder should not open a form, mode %d", m.edit)
		}
		if !strings.Contains(m.flash, "r renames") {
			t.Errorf("it should name the key that does: %q", m.flash)
		}
	})
}

// The form says which entry, and where it lives.
func TestEditorNamesWhatItIsEditing(t *testing.T) {
	m := openEditor(t, onEntry(t, editorModel(t), "db", "db-prod"))
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "Edit entry") {
		t.Errorf("the frame should say what this is:\n%s", strings.Split(out, "\n")[0])
	}
	if !strings.Contains(out, "own › Infra › db") {
		t.Errorf("and where the entry lives:\n%s", strings.Split(out, "\n")[0])
	}
	if !strings.Contains(out, "db-prod") {
		t.Errorf("and carry its values:\n%s", out)
	}
}

// A form with free text in it cannot share a keyboard with single-letter
// commands. This is the rule the whole editor exists above, and the one a
// refactor is most likely to undo.
func TestEditorOwnsTheKeyboard(t *testing.T) {
	m := openEditor(t, onEntry(t, editorModel(t), "db", "db-prod"))

	for _, k := range []string{"q", "/", "d", "D", "1", "?", "n", "N", "m", "r"} {
		mm := up(m, key2(k))
		if mm.edit != editEntry {
			t.Errorf("%q left the editor (mode %d)", k, mm.edit)
		}
		if mm.searchMode || mm.help || mm.tab != tabVault {
			t.Errorf("%q reached the surface behind", k)
		}
		if !strings.HasSuffix(mm.editForm.Value("title"), k) {
			t.Errorf("%q did not land in the field: %q", k, mm.editForm.Value("title"))
		}
	}
	for _, k := range []tea.KeyMsg{{Type: tea.KeyCtrlC}, {Type: tea.KeyCtrlS}} {
		mm := up(m, k)
		if mm.quitGuard || mm.saveConfirm {
			t.Errorf("%v reached past the editor", k)
		}
	}
}

// A custom field can be given a name and a value. tab used to be taken by the
// form before the row list saw it, so the list's own column handling was
// unreachable — and with it ^p and ^d, which act on the focused row.
func TestEditorCustomFields(t *testing.T) {
	m := onFieldsRow(t, openEditor(t, onEntry(t, editorModel(t), "db", "db-prod")))

	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlN})
	if n := len(m.editForm.Rows("fields")); n != 1 {
		t.Fatalf("^n should add a row, got %d", n)
	}
	m = typeStr(m, "Env")
	m = up(m, tea.KeyMsg{Type: tea.KeyTab}) // across to the value
	m = typeStr(m, "prod")

	keys, values, protected := m.editForm.RowValues("fields")
	if len(keys) != 1 || keys[0] != "Env" {
		t.Fatalf("keys = %v", keys)
	}
	if values[0] != "prod" {
		t.Errorf("value = %q — tab never reached the second column", values[0])
	}
	if protected[0] {
		t.Error("a new field starts unprotected")
	}

	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlP})
	if _, _, p := m.editForm.RowValues("fields"); !p[0] {
		t.Error("^p should protect the row it is on")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlD})
	if k, _, _ := m.editForm.RowValues("fields"); len(k) != 0 {
		t.Errorf("^d should remove it, %v left", k)
	}
}

// onFieldsRow tabs down to the custom field list.
func onFieldsRow(t *testing.T, m Model) Model {
	t.Helper()
	for range 12 {
		if m.editForm.fields[m.editForm.focus].key == "fields" {
			return m
		}
		m = up(m, tea.KeyMsg{Type: tea.KeyTab})
	}
	t.Fatal("never reached the custom fields")
	return m
}

// The generator and the reveal, which are why a password never has to be typed
// or read back from another window.
func TestEditorGeneratorAndReveal(t *testing.T) {
	m := openEditor(t, onEntry(t, editorModel(t), "db", "db-prod"))

	was := m.editForm.Raw("password")
	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlG})
	if got := m.editForm.Raw("password"); got == was || got == "" {
		t.Errorf("^g should roll a new password, got %q", got)
	}
	if !strings.Contains(m.flash, "generated") {
		t.Errorf("and say so: %q", m.flash)
	}
	// Never on screen unrevealed.
	if strings.Contains(ansi.Strip(m.View()), m.editForm.Raw("password")) {
		t.Error("the generated password is legible without ^r")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if !m.editForm.reveal {
		t.Error("^r should reveal")
	}
}

// A title is required, and the form says so rather than staging a nameless row.
func TestEditorRefusesAnEmptyTitle(t *testing.T) {
	m := openEditor(t, onEntry(t, editorModel(t), "db", "db-prod"))
	m.editForm = m.editForm.setValue("title", "")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.edit != editEntry {
		t.Error("it should stay open to be corrected")
	}
	if m.dirtyCount() != 0 {
		t.Error("and stage nothing")
	}
	if !strings.Contains(m.editForm.status, "title") {
		t.Errorf("and say why: %q", m.editForm.status)
	}
}

// esc drops the whole form; ↵ stages it.
func TestEditorCommitAndCancel(t *testing.T) {
	m := openEditor(t, onEntry(t, editorModel(t), "db", "db-prod"))

	cancelled := up(m, tea.KeyMsg{Type: tea.KeyEsc})
	if cancelled.edit != editNone || cancelled.dirtyCount() != 0 {
		t.Errorf("esc should close and stage nothing: mode=%d staged=%d",
			cancelled.edit, cancelled.dirtyCount())
	}

	staged := m
	staged.editForm = staged.editForm.setValue("username", "changed")
	staged = up(staged, tea.KeyMsg{Type: tea.KeyEnter})
	if staged.edit != editNone {
		t.Error("↵ should close the form")
	}
	if staged.dirtyCount() != 1 {
		t.Errorf("and stage the edit, %d staged", staged.dirtyCount())
	}
	if !strings.Contains(staged.flash, "nothing is written") {
		t.Errorf("and say nothing was written: %q", staged.flash)
	}
}

// Something on its way out cannot be edited, exactly as it cannot be renamed.
// The two keys used to disagree: r refused, e opened.
func TestEditorRefusesSomethingGoing(t *testing.T) {
	t.Run("staged itself", func(t *testing.T) {
		m := onEntry(t, editorModel(t), "db", "db-prod")
		at := m.esel
		m = up(m, key2("d"))
		m.esel = at

		e := up(m, key2("e"))
		r := up(m, key2("r"))
		if e.edit != editNone {
			t.Errorf("e opened on a row staged for deletion (mode %d)", e.edit)
		}
		if e.flash != r.flash {
			t.Errorf("e and r should say the same thing:\n  e: %q\n  r: %q", e.flash, r.flash)
		}
	})
	t.Run("going with a folder above", func(t *testing.T) {
		m := up(onRow(t, editorModel(t), "db"), key2("d"))
		e := up(onEntry(t, m, "db", "db-prod"), key2("e"))
		r := up(onEntry(t, m, "db", "db-prod"), key2("r"))
		if e.edit != editNone {
			t.Errorf("e opened inside a doomed folder (mode %d)", e.edit)
		}
		if e.flash != r.flash {
			t.Errorf("e and r should say the same thing:\n  e: %q\n  r: %q", e.flash, r.flash)
		}
	})
}

// On a locked source the key says how to unlock.
func TestEditorOnALockedSourceSaysHowToUnlock(t *testing.T) {
	m, _ := walkModel(t)
	m.writeOK = map[string]bool{}
	m = up(onEntry(t, m.expandAll(true), "db", "db-prod"), key2("e"))

	if m.edit != editNone {
		t.Error("a locked source should not open the editor")
	}
	if !strings.Contains(m.flash, "^w") {
		t.Errorf("it should name the key that unlocks: %q", m.flash)
	}
}

// Editing writes nothing.
func TestNoEditTouchesTheFile(t *testing.T) {
	m, path := walkModel(t)
	m = m.expandAll(true)
	before := fileBytes(t, path)

	m = openEditor(t, onEntry(t, m, "db", "db-prod"))
	m.editForm = m.editForm.setValue("username", "changed")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	_ = m.switchTab(tabChanges).View()

	if after := fileBytes(t, path); after != before {
		t.Error("staging an edit wrote to the file")
	}
}
