package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/edit"
)

// The delete flow, walked the way somebody uses it: every surface the key can
// be pressed on, every state it can be pressed in, and what the screen says
// about it afterwards. Nothing here saves — the point is the interface, and the
// write has its own tests.
//
// Written after a hand audit found four things: the entry detail and the search
// results both let you stage a deletion and then showed no sign of it, the
// source row swallowed the key in silence, and deleting something created in
// the same session announced a trip to a recycle bin that was never going to
// happen.

func deleteModel(t *testing.T) Model {
	t.Helper()
	m, _ := walkModel(t)
	return m.expandAll(true)
}

// Wherever a thing can be selected, d can delete it.
func TestDeleteWorksOnEverySurface(t *testing.T) {
	t.Run("entry in the table", func(t *testing.T) {
		m := up(onEntry(t, deleteModel(t), "db", "db-prod"), key2("d"))
		if m.dirtyCount() != 1 {
			t.Errorf("nothing staged (%q)", m.flash)
		}
	})
	t.Run("folder in the tree", func(t *testing.T) {
		m := up(onRow(t, deleteModel(t), "Net"), key2("d"))
		if m.dirtyCount() != 1 {
			t.Errorf("nothing staged (%q)", m.flash)
		}
	})
	t.Run("search result", func(t *testing.T) {
		m := searching(t, deleteModel(t), "router")
		m = up(m, key2("d"))
		if m.dirtyCount() != 1 {
			t.Errorf("nothing staged (%q)", m.flash)
		}
	})
	t.Run("entry detail", func(t *testing.T) {
		m := up(onEntry(t, deleteModel(t), "db", "db-prod"), tea.KeyMsg{Type: tea.KeyRight})
		if !m.detail {
			t.Fatal("→ should open the detail")
		}
		m = up(m, key2("d"))
		if m.dirtyCount() != 1 {
			t.Errorf("nothing staged (%q)", m.flash)
		}
		if !m.detail {
			t.Error("staging should not throw the reader out of the entry")
		}
	})
}

// searching runs a query and leaves the results list up.
func searching(t *testing.T, m Model, q string) Model {
	t.Helper()
	m = up(m, key2("/"))
	m = typeStr(m, q)
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.showResults() {
		t.Fatalf("no results for %q", q)
	}
	return m
}

// Every surface that can stage a deletion has to be able to show one. The tree
// showing it is not enough: from the detail pane and from the results list, the
// tree is either behind the reader's attention or not on screen at all.
func TestEveryDeleteIsVisibleWhereItWasStaged(t *testing.T) {
	_, softMark := changeStyle(edit.Deleted)
	soft := strings.TrimSpace(softMark)

	t.Run("entry table", func(t *testing.T) {
		m := up(onEntry(t, deleteModel(t), "db", "db-prod"), key2("d"))
		if out := ansi.Strip(m.View()); !strings.Contains(out, soft+" db-prod") {
			t.Errorf("the row should carry the marker:\n%s", out)
		}
	})
	t.Run("entry detail", func(t *testing.T) {
		m := up(onEntry(t, deleteModel(t), "db", "db-prod"), tea.KeyMsg{Type: tea.KeyRight})
		m = up(m, key2("d"))
		out := ansi.Strip(m.View())
		if !strings.Contains(out, soft+" "+edit.Deleted.String()) {
			t.Errorf("the pane the reader is looking at should say what is staged:\n%s", out)
		}
	})
	t.Run("search results", func(t *testing.T) {
		m := up(searching(t, deleteModel(t), "router"), key2("d"))
		if out := ansi.Strip(m.View()); !strings.Contains(out, soft+" router") {
			t.Errorf("the result row should carry the marker:\n%s", out)
		}
	})
	t.Run("changes tab", func(t *testing.T) {
		m := up(onEntry(t, deleteModel(t), "db", "db-prod"), key2("d"))
		if out := ansi.Strip(m.switchTab(tabChanges).View()); !strings.Contains(out, "recycle bin") {
			t.Errorf("the review should say where it goes:\n%s", out)
		}
	})
}

// An entry going because the folder above it is going says so too, on every
// surface — faded, because it is a consequence rather than a decision.
func TestAnInheritedDeletionIsVisibleToo(t *testing.T) {
	m := up(onRow(t, deleteModel(t), "db"), key2("d"))

	m = onEntry(t, m, "db", "db-prod")
	_, mark := changeStyle(edit.Deleted)
	if out := ansi.Strip(m.View()); !strings.Contains(out, strings.TrimSpace(mark)+" db-prod") {
		t.Errorf("an entry inside a doomed folder should read as going:\n%s", out)
	}

	m = up(m, tea.KeyMsg{Type: tea.KeyRight})
	if out := ansi.Strip(m.View()); !strings.Contains(out, edit.Deleted.String()) {
		t.Errorf("and its detail pane should say so:\n%s", out)
	}
}

// The key is a toggle, and D is an upgrade rather than a second toggle: making
// somebody press D twice, with the row briefly unmarked in between, reads as
// the key having failed.
func TestDeleteKeyToggleMatrix(t *testing.T) {
	steps := []struct {
		press string
		want  edit.State
		why   string
	}{
		{"d", edit.Deleted, "d stages a trip to the bin"},
		{"D", edit.Purged, "D upgrades it rather than toggling it off"},
		{"d", edit.Deleted, "and d downgrades it back"},
		{"d", edit.Unchanged, "the same key again takes it off"},
		{"D", edit.Purged, "D stages a permanent one from nothing"},
		{"D", edit.Unchanged, "and D takes its own off"},
	}

	m := onEntry(t, deleteModel(t), "db", "db-prod")
	id := m.selEntry().ID
	for _, s := range steps {
		m.esel, m.focus = 0, 1 // the key advances; come back to the row it marked
		m = up(m, key2(s.press))
		if got := m.chg.StateOf(id); got != s.want {
			t.Fatalf("%s: after %q the state is %v, want %v (%q)", s.why, s.press, got, s.want, m.flash)
		}
	}
}

// Working down a list costs one key per row. It used to stall on any row that
// was already marked, which is exactly the row you did not need to stop on.
func TestDeleteAlwaysAdvances(t *testing.T) {
	m := onEntry(t, deleteModel(t), "db", "db-prod")
	if m.esel != 0 {
		t.Fatalf("expected to start at the top, got %d", m.esel)
	}
	m = up(m, key2("d"))
	if m.esel != 1 {
		t.Errorf("staging should move on, esel=%d", m.esel)
	}
	m.esel = 0
	m = up(m, key2("d")) // un-stage
	if m.esel != 1 {
		t.Errorf("un-staging should move on too, esel=%d", m.esel)
	}

	// At the end of the list it stays, and the hint says so rather than naming
	// a row above that the reader would have to count back to.
	m.esel = 1
	m = up(m, key2("d"))
	if m.esel != 1 {
		t.Errorf("the last row has nowhere to advance to, esel=%d", m.esel)
	}
	if !strings.Contains(m.flash, "again undoes it") {
		t.Errorf("the hint should name the key as it is, got %q", m.flash)
	}
}

// Staging changes what the vault will contain, never how the reader is looking
// at it.
func TestDeleteLeavesTheTreeAlone(t *testing.T) {
	m := deleteModel(t)
	for _, tl := range m.visible() {
		if tl.node.name == "db" {
			tl.node.expanded = false
		}
	}
	shape := treeShape(m)

	m = onRow(t, m, "Net")
	m = up(m, key2("d"))
	if got := treeShape(m); strings.Join(got, " ") != strings.Join(shape, " ") {
		t.Errorf("the tree changed shape:\n%v\nwas:\n%v", got, shape)
	}
}

// Two deletions of one thing is not a thing. The child is already going with
// its folder, and staging it separately produced a set that pulled it out of
// the folder it was deleted with.
func TestDeleteRefusesWhatIsAlreadyGoing(t *testing.T) {
	m := up(onRow(t, deleteModel(t), "db"), key2("d"))

	for _, key := range []string{"d", "D"} {
		mm := up(onEntry(t, m, "db", "db-prod"), key2(key))
		if mm.dirtyCount() != 1 {
			t.Errorf("%q staged a second deletion for the same thing: %d", key, mm.dirtyCount())
		}
		if !strings.Contains(mm.flash, "already going with") {
			t.Errorf("%q should say why: %q", key, mm.flash)
		}
	}

	// And a folder underneath one, which is the same rule a level up.
	mm := up(onRow(t, up(onRow(t, deleteModel(t), "Infra"), key2("d")), "db"), key2("D"))
	if !strings.Contains(mm.flash, "already going with") {
		t.Errorf("a folder inside a doomed folder should say why: %q", mm.flash)
	}
}

// With no recycle bin, d is a permanent delete — and says so, because silence
// there would be a lie about the one act that cannot be undone.
func TestDeleteWithoutARecycleBinSaysSo(t *testing.T) {
	m, _ := walkModel(t)
	m.handles["own"].DisableRecycleBinForTest()
	m = onEntry(t, m.expandAll(true), "db", "db-prod")

	id := m.selEntry().ID
	m = up(m, key2("d"))
	if got := m.chg.StateOf(id); got != edit.Purged {
		t.Errorf("without a bin, d is permanent; state is %v", got)
	}
	if !strings.Contains(m.flash, "no recycle bin") {
		t.Errorf("and it has to say so: %q", m.flash)
	}
}

// Deleting something created in the same session cancels the creation. There is
// no deletion, no recycle bin, and nothing to undo — the row is not in the tree
// any more, so a hint naming a key would name one that cannot be pressed.
func TestDeletingSomethingJustCreatedSaysWhatReallyHappened(t *testing.T) {
	for _, c := range []struct{ key, make, word string }{
		{"n", "brand-new", "entry"},
		{"N", "Fresh", "folder"},
	} {
		m := up(onRow(t, deleteModel(t), "Net"), key2(c.key))
		m = typeStr(m, c.make)
		m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
		if m.dirtyCount() != 1 {
			t.Fatalf("%s: the creation should be staged", c.word)
		}
		if c.key == "n" {
			m = onEntry(t, m, "Net", c.make)
		} else {
			m = onRow(t, m, c.make)
		}

		m = up(m, key2("d"))
		if m.dirtyCount() != 0 {
			t.Errorf("%s: create then delete is nothing at all, %d staged", c.word, m.dirtyCount())
		}
		if strings.Contains(m.flash, "recycle bin") {
			t.Errorf("%s: it is not going to a bin, it was never written: %q", c.word, m.flash)
		}
		if !strings.Contains(m.flash, "never written") {
			t.Errorf("%s: the message should say what happened: %q", c.word, m.flash)
		}
		if strings.Contains(m.flash, "undoes it") {
			t.Errorf("%s: there is no row left to press a key on: %q", c.word, m.flash)
		}
	}
}

// A source row stands for the root group. n and N create inside it; the rest
// have nothing to act on, and said nothing at all — a key that looks broken.
func TestDeleteOnASourceRowExplainsItself(t *testing.T) {
	for _, key := range []string{"d", "D", "e", "r", "m"} {
		m := up(onRow(t, deleteModel(t), "own"), key2(key))
		if m.dirtyCount() != 0 {
			t.Errorf("%q staged something against a source row", key)
		}
		if m.flash == "" {
			t.Errorf("%q on a source row says nothing at all", key)
		}
	}
	// But creating there still works: that is the root group, not a mistake.
	m := up(onRow(t, deleteModel(t), "own"), key2("N"))
	if m.edit != editFolder {
		t.Errorf("N on a source row should still open the folder editor, got %d", m.edit)
	}
}

// On a source nobody has unlocked, the key says how to unlock rather than
// staging anything or looking dead.
func TestDeleteOnALockedSourceSaysHowToUnlock(t *testing.T) {
	m, _ := walkModel(t)
	m.writeOK = map[string]bool{}
	m = onEntry(t, m.expandAll(true), "db", "db-prod")

	m = up(m, key2("d"))
	if m.dirtyCount() != 0 {
		t.Error("a locked source must not stage anything")
	}
	if !strings.Contains(m.flash, "^w") {
		t.Errorf("it should name the key that unlocks: %q", m.flash)
	}
}

// Staging writes nothing. The file is byte-identical after the whole matrix.
func TestNoDeleteTouchesTheFile(t *testing.T) {
	m, path := walkModel(t)
	m = m.expandAll(true)
	before := fileBytes(t, path)

	m = up(onEntry(t, m, "db", "db-prod"), key2("d"))
	m = up(onEntry(t, m, "db", "db-stage"), key2("D"))
	m = up(onRow(t, m, "Net"), key2("d"))
	m = up(onRow(t, m, "Infra"), key2("D"))
	_ = m.switchTab(tabChanges).View()

	if after := fileBytes(t, path); after != before {
		t.Error("staging a deletion wrote to the file")
	}
}

// fileBytes is the file's contents, for proving a surface did not write.
func fileBytes(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
