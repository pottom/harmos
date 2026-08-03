package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/edit"
)

// The rename flow, walked the way somebody uses it.
//
// Written after a hand audit found that r opened the field on three surfaces
// and drew it on one: from the entry detail the pane rendered unchanged and
// even the mode line was missing, so every keystroke went into a field nobody
// could see and ↵ staged whatever had been typed blind.

func renameModel(t *testing.T) Model {
	t.Helper()
	m, _ := walkModel(t)
	return m.expandAll(true)
}

// openRename presses r and checks the field opened.
func openRename(t *testing.T, m Model, to string) Model {
	t.Helper()
	m = up(m, key2("r"))
	if m.edit != editInline {
		t.Fatalf("r should open the field, got mode %d (%q)", m.edit, m.flash)
	}
	m.inlineInput.SetValue(to)
	return m
}

// Wherever a thing can be selected, r can rename it — and the field is on
// screen while it does.
func TestRenameIsVisibleOnEverySurface(t *testing.T) {
	const typed = "RENAMED-HERE"

	t.Run("folder in the tree", func(t *testing.T) {
		m := openRename(t, onRow(t, renameModel(t), "Net"), typed)
		assertFieldOnScreen(t, m, typed)
	})
	t.Run("entry in the table", func(t *testing.T) {
		m := openRename(t, onEntry(t, renameModel(t), "db", "db-prod"), typed)
		assertFieldOnScreen(t, m, typed)
	})
	t.Run("entry detail", func(t *testing.T) {
		m := up(onEntry(t, renameModel(t), "db", "db-prod"), tea.KeyMsg{Type: tea.KeyRight})
		if !m.detail {
			t.Fatal("→ should open the detail")
		}
		m = openRename(t, m, typed)
		assertFieldOnScreen(t, m, typed)
		if !m.detail {
			t.Error("renaming should not throw the reader out of the entry")
		}
	})
	t.Run("search result", func(t *testing.T) {
		m := openRename(t, searching(t, renameModel(t), "router"), "RENAMED")
		assertFieldOnScreen(t, m, "RENAMED")
	})
}

// assertFieldOnScreen checks the value is rendered and the mode is announced.
func assertFieldOnScreen(t *testing.T, m Model, typed string) {
	t.Helper()
	out := ansi.Strip(m.View())
	if !strings.Contains(out, typed) {
		t.Errorf("the field is not on screen:\n%s", out)
	}
	if !strings.Contains(out, "-- RENAME --") {
		t.Errorf("nothing says which mode this is:\n%s", out)
	}
	if !strings.Contains(out, "esc cancel") {
		t.Errorf("nothing says how to get out:\n%s", out)
	}
}

// While the field is open, letters are letters. This is the rule the modal
// editor has, and it matters more here: the field sits on a surface where q
// quits, / searches and the digits switch tabs.
func TestRenameFieldOwnsTheKeyboard(t *testing.T) {
	for _, start := range []string{"folder", "entry"} {
		m := renameModel(t)
		if start == "folder" {
			m = onRow(t, m, "Net")
		} else {
			m = onEntry(t, m, "db", "db-prod")
		}
		m = up(m, key2("r"))
		m.inlineInput.SetValue("")

		m = typeStr(m, "q/dD1?nN")
		if m.edit != editInline {
			t.Fatalf("%s: a hotkey escaped the field", start)
		}
		if m.searchMode || m.help || m.tab != tabVault {
			t.Errorf("%s: a key reached the surface behind: search=%v help=%v tab=%d",
				start, m.searchMode, m.help, m.tab)
		}
		if got := m.inlineInput.Value(); got != "q/dD1?nN" {
			t.Errorf("%s: the field got %q", start, got)
		}

		// And the keys that would leave with unsaved work in hand.
		for _, k := range []tea.KeyMsg{{Type: tea.KeyCtrlC}, {Type: tea.KeyCtrlS}} {
			mm := up(m, k)
			if mm.saveConfirm || mm.quitGuard {
				t.Errorf("%s: %v reached past the field", start, k)
			}
		}
	}
}

// esc leaves the name alone; ↵ stages it. Neither moves the cursor, because
// staging changes what the vault will contain, not how it is being read.
func TestRenameCommitAndCancel(t *testing.T) {
	m := onRow(t, renameModel(t), "Net")
	at, focus := m.tsel, m.focus

	cancelled := up(openRename(t, m, "Nope"), tea.KeyMsg{Type: tea.KeyEsc})
	if cancelled.edit != editNone {
		t.Error("esc should close the field")
	}
	if cancelled.dirtyCount() != 0 {
		t.Error("esc staged something")
	}
	if cancelled.tsel != at || cancelled.focus != focus {
		t.Error("esc moved the reader")
	}

	staged := up(openRename(t, m, "Networks"), key2("enter"))
	if staged.edit != editNone {
		t.Error("↵ should close the field")
	}
	if staged.dirtyCount() != 1 {
		t.Errorf("↵ should stage the rename, %d staged (%q)", staged.dirtyCount(), staged.flash)
	}
	if staged.tsel != at || staged.focus != focus {
		t.Error("↵ moved the reader")
	}
}

// What the field accepts and what it refuses.
func TestRenameNameRules(t *testing.T) {
	cases := []struct {
		name    string
		to      string
		staged  int
		stays   bool // the field stays open, so it can be corrected
		flashIs string
	}{
		{"empty", "", 0, true, "empty"},
		{"whitespace only", "   ", 0, true, "empty"},
		{"unchanged", "Net", 0, false, "unchanged"},
		{"a slash in the name", "a/b", 1, false, "staged"},
		{"non-ASCII", "Ärger İstanbul ẞ", 1, false, "staged"},
		{"longer than the row", strings.Repeat("x", 80), 1, false, "staged"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := up(openRename(t, onRow(t, renameModel(t), "Net"), c.to), key2("enter"))
			if got := m.dirtyCount(); got != c.staged {
				t.Errorf("staged %d, want %d (%q)", got, c.staged, m.flash)
			}
			if open := m.edit == editInline; open != c.stays {
				t.Errorf("field open = %v, want %v", open, c.stays)
			}
			if !strings.Contains(m.flash, c.flashIs) {
				t.Errorf("flash %q should mention %q", m.flash, c.flashIs)
			}
		})
	}
}

// A row already staged for deletion cannot be renamed: the two describe
// different futures for one thing, and the review could not draw both.
func TestRenameRefusedOnSomethingGoing(t *testing.T) {
	t.Run("staged itself", func(t *testing.T) {
		m := onRow(t, renameModel(t), "Net")
		at := m.tsel
		m = up(m, key2("d"))
		m.tsel = at
		m = up(m, key2("r"))
		if m.edit == editInline {
			t.Error("a row staged for deletion should not open a field")
		}
		if !strings.Contains(m.flash, "deletion") {
			t.Errorf("it should say why: %q", m.flash)
		}
	})
	t.Run("going with a folder above", func(t *testing.T) {
		m := up(onRow(t, renameModel(t), "Infra"), key2("d"))
		m = up(onRow(t, m, "db"), key2("r"))
		if m.edit == editInline {
			t.Error("something inside a doomed folder should not open a field")
		}
		if !strings.Contains(m.flash, "going with") {
			t.Errorf("it should say what is taking it: %q", m.flash)
		}
	})
}

// Renaming an entry changes its title and keeps everything else. A rename that
// blanked the password would be a rename that lost the password.
func TestRenameAnEntryKeepsTheRest(t *testing.T) {
	m := onEntry(t, renameModel(t), "db", "db-prod")
	was := *m.selEntry()

	m = up(openRename(t, m, "new title"), key2("enter"))

	var op *edit.Op
	for _, o := range m.chg.Effective() {
		if o.Kind == edit.EditEntry && o.Target == was.ID {
			cp := o
			op = &cp
		}
	}
	if op == nil {
		t.Fatalf("no edit staged for %s: %v", was.ID, m.chg.Effective())
	}
	if op.After.Title != "new title" {
		t.Errorf("title = %q", op.After.Title)
	}
	if op.After.Password.Reveal() == "" {
		t.Error("the rename dropped the password")
	}
	if op.After.Username != was.Username || op.After.URL != was.URL {
		t.Errorf("the rename changed other fields: %+v", op.After)
	}
}

// On a locked source the key says how to unlock, and stages nothing.
func TestRenameOnALockedSourceSaysHowToUnlock(t *testing.T) {
	m, _ := walkModel(t)
	m.writeOK = map[string]bool{}
	m = up(onRow(t, m.expandAll(true), "Net"), key2("r"))

	if m.edit == editInline {
		t.Error("a locked source should not open the field")
	}
	if !strings.Contains(m.flash, "^w") {
		t.Errorf("it should name the key that unlocks: %q", m.flash)
	}
}

// Staging a rename writes nothing.
func TestNoRenameTouchesTheFile(t *testing.T) {
	m, path := walkModel(t)
	m = m.expandAll(true)
	before := fileBytes(t, path)

	m = up(openRename(t, onRow(t, m, "Net"), "Networks"), key2("enter"))
	m = up(openRename(t, onEntry(t, m, "db", "db-prod"), "renamed"), key2("enter"))
	_ = m.switchTab(tabChanges).View()

	if after := fileBytes(t, path); after != before {
		t.Error("staging a rename wrote to the file")
	}
}
