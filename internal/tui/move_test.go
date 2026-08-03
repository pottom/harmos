package tui

import (
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault"
)

// The move flow, walked the way somebody uses it.
//
// m picks a row up and the tree stays live: the destination is chosen the way
// anything in the tree is found. There used to be a list of destinations in a
// window over the vault, which is the objection the rename and the new folder
// already answered — a screen that covers the tree to ask a question about it.

func moveModel(t *testing.T) Model {
	t.Helper()
	m, _ := walkModel(t)
	return m.expandAll(true)
}

// m picks up from every surface a target can be selected on, and comes back to
// the tree — which is what the reader steers with from here.
func TestMoveOpensFromEverySurface(t *testing.T) {
	surfaces := map[string]func(*testing.T) Model{
		"folder in the tree": func(t *testing.T) Model { return onRow(t, moveModel(t), "db") },
		"entry in the table": func(t *testing.T) Model { return onEntry(t, moveModel(t), "db", "db-prod") },
		"a search result":    func(t *testing.T) Model { return searching(t, moveModel(t), "router") },
		"the entry detail": func(t *testing.T) Model {
			return up(onEntry(t, moveModel(t), "db", "db-prod"), tea.KeyMsg{Type: tea.KeyRight})
		},
	}
	for name, start := range surfaces {
		t.Run(name, func(t *testing.T) {
			m := up(start(t), key2("m"))
			if m.edit != editCarry {
				t.Fatalf("m should pick it up, got mode %d (%q)", m.edit, m.flash)
			}
			if m.focus != 0 || m.detail || m.showResults() {
				t.Errorf("it should land in the tree: focus=%d detail=%v results=%v",
					m.focus, m.detail, m.showResults())
			}
			if _, ok := m.destUnderCursor(); !ok {
				t.Error("and on a row that can be steered from")
			}
		})
	}
}

// The footer says what is in hand and where it would land, all the way through.
func TestCarryFooterSaysWhatAndWhere(t *testing.T) {
	m := up(onEntry(t, moveModel(t), "db", "db-prod"), key2("m"))

	m = pickDestination(t, m, "Net")
	out := ansi.Strip(m.View())
	for _, want := range []string{"-- MOVING --", "db-prod", "Net", "↵ drop", "esc"} {
		if !strings.Contains(out, want) {
			t.Errorf("the footer should say %q:\n%s", want, out)
		}
	}

	// And on a folder that cannot take it, why not — rather than a silent ↵.
	m = pickDestination(t, m, "db")
	if out := ansi.Strip(m.View()); !strings.Contains(out, "already here") {
		t.Errorf("it should say why this one is no good:\n%s", out)
	}
}

// The four rules, asked one folder at a time so the answer can be shown as the
// cursor passes over it rather than by leaving it off a list with no reason.
func TestCarryRefusals(t *testing.T) {
	t.Run("its own folder", func(t *testing.T) {
		m := up(onEntry(t, moveModel(t), "db", "db-prod"), key2("m"))
		m = up(pickDestination(t, m, "db"), key2("enter"))
		if m.dirtyCount() != 0 {
			t.Error("dropping something where it already is staged a move")
		}
		if !strings.Contains(m.flash, "already here") {
			t.Errorf("and it should say so: %q", m.flash)
		}
	})
	t.Run("itself", func(t *testing.T) {
		m := up(onRow(t, moveModel(t), "db"), key2("m"))
		m = up(pickDestination(t, m, "db"), key2("enter"))
		if m.dirtyCount() != 0 || !strings.Contains(m.flash, "inside itself") {
			t.Errorf("a folder dropped on itself: %d staged, %q", m.dirtyCount(), m.flash)
		}
	})
	t.Run("its own contents", func(t *testing.T) {
		m := up(onRow(t, moveModel(t), "Infra"), key2("m"))
		m = up(pickDestination(t, m, "db"), key2("enter"))
		if m.dirtyCount() != 0 || !strings.Contains(m.flash, "own contents") {
			t.Errorf("a folder dropped into its own subtree: %d staged, %q", m.dirtyCount(), m.flash)
		}
	})
	t.Run("a folder staged for deletion", func(t *testing.T) {
		m := up(onRow(t, moveModel(t), "Net"), key2("d"))
		m = up(onEntry(t, m, "db", "db-prod"), key2("m"))
		m = up(pickDestination(t, m, "Net"), key2("enter"))
		if m.dirtyCount() != 1 { // the deletion only
			t.Errorf("dropped into a folder about to stop existing: %d staged", m.dirtyCount())
		}
		if !strings.Contains(m.flash, "deletion") {
			t.Errorf("and it should say why: %q", m.flash)
		}
	})
	t.Run("the top of a source takes it", func(t *testing.T) {
		m := up(onEntry(t, moveModel(t), "db", "db-prod"), key2("m"))
		m = up(pickDestination(t, m, "own"), key2("enter"))
		if m.dirtyCount() != 1 {
			t.Errorf("the root group is a folder like any other: %d staged (%q)",
				m.dirtyCount(), m.flash)
		}
	})
}

// The tree stays live while something is held: every browsing key still browses,
// ↵ drops, esc puts it back, and anything else says the mode is still on.
func TestCarryKeepsTheTreeLive(t *testing.T) {
	m := up(onEntry(t, moveModel(t), "db", "db-prod"), key2("m"))
	at := m.tsel

	m = up(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.tsel == at {
		t.Error("↓ should still move the tree cursor")
	}
	if m.edit != editCarry {
		t.Fatal("and not put the row down")
	}

	for _, k := range []string{"d", "D", "n", "N", "r", "e", "/", "q"} {
		mm := up(m, key2(k))
		if mm.edit != editCarry {
			t.Errorf("%q ended the move (mode %d)", k, mm.edit)
		}
		if mm.dirtyCount() != 0 || mm.searchMode {
			t.Errorf("%q acted while another row was in hand", k)
		}
	}

	if put := up(m, tea.KeyMsg{Type: tea.KeyEsc}); put.edit != editNone || put.dirtyCount() != 0 {
		t.Errorf("esc should put it back: mode=%d staged=%d", put.edit, put.dirtyCount())
	}
}

// Moving something out of a folder that is going is how you keep one thing from
// a folder you are deleting. It has to survive the write, in that order.
func TestMoveOutOfADoomedFolderKeepsIt(t *testing.T) {
	m, path := walkModel(t)
	m = m.expandAll(true)

	m = up(onRow(t, m, "db"), key2("d")) // the folder goes to the bin
	m = onEntry(t, m, "db", "db-prod")
	m = up(m, key2("m"))
	m = up(pickDestination(t, m, "Net"), key2("enter")) // this one comes out first

	c := m.switchTab(tabChanges)
	c, _ = c.askToSave()
	c, cmd := c.updateSaveConfirm("y")
	if cmd == nil {
		t.Fatal("confirming should start the write")
	}
	c = c.onSaveDone(cmd().(saveDoneMsg))
	if c.flash != "saved" {
		t.Fatalf("save: %q", c.flash)
	}

	h, err := vault.OpenHandle(path, "own", vault.Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range h.Snapshot().Entries {
		got = append(got, e.Path+"/"+e.Title)
	}
	sort.Strings(got)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "Net/db-prod") {
		t.Errorf("the entry moved out should be in Net:\n%s", joined)
	}
	if !strings.Contains(joined, "Recycle Bin/db/db-stage") {
		t.Errorf("and the rest of the folder should be in the bin:\n%s", joined)
	}
}

// On a locked source the key says how to unlock, and opens nothing.
func TestMoveOnALockedSourceSaysHowToUnlock(t *testing.T) {
	m, _ := walkModel(t)
	m.writeOK = map[string]bool{}
	m = up(onEntry(t, m.expandAll(true), "db", "db-prod"), key2("m"))

	if m.edit != editNone {
		t.Error("a locked source should not pick anything up")
	}
	if !strings.Contains(m.flash, "^w") {
		t.Errorf("it should name the key that unlocks: %q", m.flash)
	}
}

// Staging a move writes nothing.
func TestNoMoveTouchesTheFile(t *testing.T) {
	m, path := walkModel(t)
	m = m.expandAll(true)
	before := fileBytes(t, path)

	m = up(onEntry(t, m, "db", "db-prod"), key2("m"))
	m = pickDestination(t, m, "Net")
	m = up(m, key2("enter"))
	_ = m.switchTab(tabChanges).View()

	if after := fileBytes(t, path); after != before {
		t.Error("staging a move wrote to the file")
	}
}

// A move to the top of a source is nameable. The snapshot leaves the root group
// out of Folders — nobody browses the container the tree draws as the source —
// so the review could resolve every destination but that one, and a move there
// said only "moved".
func TestReviewNamesAMoveToTheTop(t *testing.T) {
	m := onRow(t, moveModel(t), "db")
	m = up(m, key2("m"))
	m = up(pickDestination(t, m, "own"), key2("enter"))

	out := ansi.Strip(m.switchTab(tabChanges).View())
	if strings.Contains(out, " moved ") || !strings.Contains(out, "top level") {
		t.Errorf("the review should name where it went:\n%s", out)
	}
	if !strings.Contains(out, "Infra") {
		t.Errorf("and where it came from:\n%s", out)
	}
}

// What is in hand says so, wherever it is sitting — and that is not where the
// cursor is, which is the point: the two are different rows now.
func TestCarriedRowIsMarked(t *testing.T) {
	t.Setenv("HARMOS_NERDFONT", "0")

	t.Run("an entry, while its folder is on screen", func(t *testing.T) {
		m := up(onEntry(t, moveModel(t), "db", "db-prod"), key2("m"))
		// After the model is built: the glyph set is chosen when New reads the
		// config, so asking before that answers with whatever the last test left.
		moved := ic().moved
		if out := ansi.Strip(m.View()); !strings.Contains(out, moved+" db-prod") {
			t.Errorf("the carried entry should be marked:\n%s", out)
		}
		// Once the cursor walks off to choose a destination the table follows
		// it, so the entry is not on screen — which is why the footer names
		// what is in hand the whole way. TestCarryFooterSaysWhatAndWhere.
		m = pickDestination(t, m, "Net")
		if out := ansi.Strip(m.View()); !strings.Contains(out, "db-prod") {
			t.Errorf("the footer should still name it:\n%s", out)
		}
	})
	t.Run("a folder, in the tree", func(t *testing.T) {
		m := up(onRow(t, moveModel(t), "db"), key2("m"))
		moved := ic().moved
		m = pickDestination(t, m, "Net")
		if out := ansi.Strip(m.View()); !strings.Contains(out, "db 2 "+moved) {
			t.Errorf("the carried folder should be marked:\n%s", out)
		}
	})
}

// Picking something up does not change the vault, and putting it back does not
// either — the file is untouched through the whole thing.
func TestCarryingWritesNothing(t *testing.T) {
	m, path := walkModel(t)
	m = m.expandAll(true)
	before := fileBytes(t, path)

	m = up(onEntry(t, m, "db", "db-prod"), key2("m"))
	m = pickDestination(t, m, "Net")
	m = up(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.dirtyCount() != 0 {
		t.Errorf("esc staged %d change(s)", m.dirtyCount())
	}

	m = up(onEntry(t, m, "db", "db-prod"), key2("m"))
	m = up(pickDestination(t, m, "Net"), key2("enter"))
	_ = m.switchTab(tabChanges).View()

	if after := fileBytes(t, path); after != before {
		t.Error("carrying wrote to the file")
	}
}

// The cursor carries the name. Where something would land is a question about
// the row the cursor is on, so the answer belongs on that row — the footer says
// it too, but the eye is in the tree.
func TestTheCursorCarriesTheName(t *testing.T) {
	t.Setenv("HARMOS_NERDFONT", "0")
	m := up(onEntry(t, moveModel(t), "db", "db-prod"), key2("m"))
	moved := ic().moved

	for _, where := range []string{"Net", "Infra", "own"} {
		mm := pickDestination(t, m, where)
		rows := mm.treeLines(mm.leftPaneW()-2, 20)
		row := ansi.Strip(rows[mm.tsel])
		if !strings.Contains(row, moved+" db-prod") {
			t.Errorf("the cursor on %q should carry what is in hand: %q", where, row)
		}
		if !strings.Contains(row, where) {
			t.Errorf("and the folder it is landing on has to stay readable: %q", row)
		}
		// Only the cursor's row: a name on every row is a name on none.
		for i, r := range rows {
			if i != mm.tsel && strings.Contains(ansi.Strip(r), moved+" db-prod") {
				t.Errorf("row %d also carries the tag: %q", i, ansi.Strip(r))
			}
		}
	}

	// Nothing is carried when nothing is in hand.
	idle := onRow(t, moveModel(t), "Net")
	for _, r := range idle.treeLines(idle.leftPaneW()-2, 20) {
		if strings.Contains(ansi.Strip(r), moved+" ") {
			t.Errorf("a tag with nothing in hand: %q", ansi.Strip(r))
		}
	}
}
