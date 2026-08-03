package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/edit"
)

// The create flow, walked the way somebody uses it.
//
// Written after a hand audit found two things: neither form said *where* the
// thing was about to be made — the frame carried the source name and nothing
// else, so N from a search result or from the entry detail landed in whatever
// folder the tree cursor had been left on, unseen — and the new-folder form was
// a full-screen modal with one field on it, covering the very tree that would
// have answered the question.

func createModel(t *testing.T) Model {
	t.Helper()
	m, _ := walkModel(t)
	return m.expandAll(true)
}

// n and N work from every surface a parent can be worked out from.
func TestCreateOpensFromEverySurface(t *testing.T) {
	surfaces := map[string]func(*testing.T) Model{
		"folder in the tree": func(t *testing.T) Model { return onRow(t, createModel(t), "db") },
		"entry in the table": func(t *testing.T) Model { return onEntry(t, createModel(t), "db", "db-prod") },
		"the source row":     func(t *testing.T) Model { return onRow(t, createModel(t), "own") },
		"a search result":    func(t *testing.T) Model { return searching(t, createModel(t), "router") },
		"the entry detail": func(t *testing.T) Model {
			return up(onEntry(t, createModel(t), "db", "db-prod"), tea.KeyMsg{Type: tea.KeyRight})
		},
	}
	for name, start := range surfaces {
		t.Run(name+"/n", func(t *testing.T) {
			m := up(start(t), key2("n"))
			if m.edit != editEntry || !m.editNew {
				t.Fatalf("n should open a new entry, got mode %d new=%v (%q)", m.edit, m.editNew, m.flash)
			}
		})
		t.Run(name+"/N", func(t *testing.T) {
			m := up(start(t), key2("N"))
			if m.edit != editInline {
				t.Fatalf("N should open a name field, got mode %d (%q)", m.edit, m.flash)
			}
		})
	}
}

// Both say where the thing is going. A form that covers the tree cannot be
// checked against it, and the tree cursor may be somewhere the reader has not
// looked at in a while.
func TestCreateSaysWhereItIsGoing(t *testing.T) {
	t.Run("a new entry names its folder", func(t *testing.T) {
		m := up(onRow(t, createModel(t), "db"), key2("n"))
		if out := ansi.Strip(m.View()); !strings.Contains(out, "own › Infra › db") {
			t.Errorf("the frame should name the destination:\n%s", strings.Split(out, "\n")[0])
		}
	})
	t.Run("from a search result, the result's folder", func(t *testing.T) {
		m := up(searching(t, createModel(t), "router"), key2("n"))
		if out := ansi.Strip(m.View()); !strings.Contains(out, "own › Net") {
			t.Errorf("it should name the result's folder:\n%s", strings.Split(out, "\n")[0])
		}
	})
	t.Run("on a source row, the top", func(t *testing.T) {
		m := up(onRow(t, createModel(t), "own"), key2("n"))
		if out := ansi.Strip(m.View()); !strings.Contains(out, "the top") {
			t.Errorf("the root group is 'the top', not a blank:\n%s", strings.Split(out, "\n")[0])
		}
	})
	t.Run("a new folder is where it will be", func(t *testing.T) {
		// N needs no wording: the row is in the place.
		m := up(onRow(t, createModel(t), "db"), key2("N"))
		rows := rowNames(m)
		var under bool
		for i, n := range rows {
			if n == "db" && i+1 < len(rows) && rows[i+1] == newFolderName {
				under = true
			}
		}
		if !under {
			t.Errorf("the new row should sit under the folder it belongs to: %v", rows)
		}
	})
}

// The new-folder field says which act it is. A row made to be typed on and a
// row being renamed look the same once the field is open.
func TestNewFolderAnnouncesItself(t *testing.T) {
	m := up(onRow(t, createModel(t), "db"), key2("N"))
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "-- NEW FOLDER --") {
		t.Errorf("the mode line should say what is being made:\n%s", out)
	}
	if strings.Contains(out, "-- RENAME --") {
		t.Error("a creation is not a rename")
	}

	// And renaming still says rename.
	r := up(onRow(t, createModel(t), "Net"), key2("r"))
	if out := ansi.Strip(r.View()); !strings.Contains(out, "-- RENAME --") {
		t.Errorf("a rename should still say rename:\n%s", out)
	}
}

// esc backs out of a folder never finished. The row only existed to be typed
// on, so leaving it behind under its stand-in name is not what esc means.
func TestEscapeUnmakesANewFolder(t *testing.T) {
	m := up(onRow(t, createModel(t), "db"), key2("N"))
	if m.dirtyCount() != 1 {
		t.Fatalf("the row has to exist to be typed on, %d staged", m.dirtyCount())
	}

	m = up(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.dirtyCount() != 0 {
		t.Errorf("esc left %d staged: %v", m.dirtyCount(), m.chg.Effective())
	}
	if slicesContains(rowNames(m), newFolderName) {
		t.Errorf("and left the row behind: %v", rowNames(m))
	}
	if !strings.Contains(m.flash, "not made") {
		t.Errorf("it should say so: %q", m.flash)
	}
}

// ↵ with nothing typed keeps the stand-in, the way every file manager does.
func TestNewFolderCommitsWithOrWithoutAName(t *testing.T) {
	t.Run("named", func(t *testing.T) {
		m := up(onRow(t, createModel(t), "db"), key2("N"))
		m = typeStr(m, "Fresh")
		m = up(m, key2("enter"))
		if !slicesContains(rowNames(m), "Fresh") {
			t.Errorf("the folder should carry the name typed: %v", rowNames(m))
		}
		// One creation, not a creation and a rename: the reduction folds them.
		if n := len(m.chg.Effective()); n != 1 {
			t.Errorf("expected one effective op, got %d: %v", n, m.chg.Effective())
		}
		if m.chg.Effective()[0].Kind != edit.CreateGroup {
			t.Errorf("and it should be a creation, got %v", m.chg.Effective()[0].Kind)
		}
	})
	t.Run("unnamed", func(t *testing.T) {
		m := up(onRow(t, createModel(t), "db"), key2("N"))
		m = up(m, key2("enter"))
		if !slicesContains(rowNames(m), newFolderName) {
			t.Errorf("it should keep the stand-in: %v", rowNames(m))
		}
		if strings.Contains(m.flash, "unchanged") {
			t.Errorf("a folder was made; that is not 'unchanged': %q", m.flash)
		}
	})
}

// Typing goes straight into the name — the stand-in is a placeholder, not a
// value to type after.
func TestNewFolderFieldStartsEmpty(t *testing.T) {
	m := up(onRow(t, createModel(t), "db"), key2("N"))
	if got := m.inlineInput.Value(); got != "" {
		t.Errorf("the field starts with %q; typing would land after it", got)
	}
	m = typeStr(m, "Fresh")
	if got := m.inlineInput.Value(); got != "Fresh" {
		t.Errorf("the field holds %q", got)
	}
}

// Nothing can be created inside a folder that is on its way out.
func TestCreateRefusedInsideADoomedFolder(t *testing.T) {
	m := up(onRow(t, createModel(t), "Net"), key2("d"))
	for _, key := range []string{"n", "N"} {
		mm := up(onRow(t, m, "Net"), key2(key))
		if mm.edit != editNone {
			t.Errorf("%q opened an editor inside a doomed folder (mode %d)", key, mm.edit)
		}
		if !strings.Contains(mm.flash, "going with") {
			t.Errorf("%q should say why: %q", key, mm.flash)
		}
	}
}

// On a locked source both keys say how to unlock.
func TestCreateOnALockedSourceSaysHowToUnlock(t *testing.T) {
	for _, key := range []string{"n", "N"} {
		m, _ := walkModel(t)
		m.writeOK = map[string]bool{}
		m = up(onRow(t, m.expandAll(true), "db"), key2(key))
		if m.edit != editNone {
			t.Errorf("%q opened an editor on a locked source", key)
		}
		if !strings.Contains(m.flash, "^w") {
			t.Errorf("%q should name the key that unlocks: %q", key, m.flash)
		}
	}
}

// Creating writes nothing.
func TestNoCreateTouchesTheFile(t *testing.T) {
	m, path := walkModel(t)
	m = m.expandAll(true)
	before := fileBytes(t, path)

	m = up(onRow(t, m, "db"), key2("N"))
	m = typeStr(m, "Fresh")
	m = up(m, key2("enter"))

	m = up(onRow(t, m, "Fresh"), key2("n"))
	m = typeStr(m, "brand-new")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	_ = m.switchTab(tabChanges).View()

	if after := fileBytes(t, path); after != before {
		t.Error("creating staged something to disk")
	}
}

func slicesContains(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}
