package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/vault"
)

// stageAnEdit takes a model through a real edit and leaves it staged.
func stageAnEdit(t *testing.T, m Model) Model {
	t.Helper()
	m = intoEditor(t, m)
	m = typeStr(m, "-edited")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.dirtyCount() != 1 {
		t.Fatalf("expected one staged change, got %d", m.dirtyCount())
	}
	return m
}

// The end of the chain: unlock, stage, review, confirm. Only the last step
// writes, and only after saying which file and how much.
func TestSaveWritesAfterConfirmation(t *testing.T) {
	m := stageAnEdit(t, editModel(t))
	path := m.handles["own"].Path()
	before, _ := os.ReadFile(path)

	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if !m.saveConfirm {
		t.Fatal("^s should ask before writing")
	}
	out := ansi.Strip(m.View())
	for _, want := range []string{"Write these changes?", path, "backup:"} {
		if !strings.Contains(out, want) {
			t.Errorf("the confirmation should name %q:\n%s", want, out)
		}
	}

	// Declining writes nothing and keeps the change.
	m = up(m, key2("n"))
	if m.saveConfirm {
		t.Error("n should dismiss the confirmation")
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("declining still wrote to the file")
	}
	if m.dirtyCount() != 1 {
		t.Error("declining should keep the change staged")
	}

	// Confirming runs the save off the update loop.
	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	nm, cmd := m.Update(key2("y"))
	m = nm.(Model)
	if !m.saving {
		t.Fatal("confirming should start a save")
	}
	if cmd == nil {
		t.Fatal("the save must run off the update loop; Argon2 would freeze the interface")
	}
	msg := cmd()
	done, ok := msg.(saveDoneMsg)
	if !ok {
		t.Fatalf("expected a saveDoneMsg, got %T", msg)
	}
	if done.err != nil {
		t.Fatalf("save failed: %v", done.err)
	}

	m = m.onSaveDone(done)
	if m.dirtyCount() != 0 {
		t.Errorf("the set should be empty after a save, got %d", m.dirtyCount())
	}
	if after, _ := os.ReadFile(path); string(after) == string(before) {
		t.Error("the file should have changed")
	}
	// And the tree now shows the file rather than our idea of it.
	found := false
	for _, e := range m.mergedEntries {
		if strings.HasSuffix(e.Title, "-edited") {
			found = true
		}
	}
	if !found {
		t.Error("the reloaded view should show the edited entry")
	}
}

// Reverting a change from the review list, and being told when it takes others
// with it.
func TestRevertFromTheChangesTab(t *testing.T) {
	m := stageAnEdit(t, editModel(t))
	m = up(m, tabKey(tabChanges))
	if m.tab != tabChanges {
		t.Fatalf("the Changes key should open the Changes tab, got %d", m.tab)
	}
	if out := ansi.Strip(m.View()); !strings.Contains(out, "-edited") {
		t.Errorf("the change should be listed:\n%s", out)
	}

	m = up(m, key2("x"))
	if m.dirtyCount() != 0 {
		t.Errorf("x should revert the selected change, %d left", m.dirtyCount())
	}
	if !strings.Contains(m.flash, "reverted") {
		t.Errorf("flash = %q", m.flash)
	}
}

// No secret may reach the review list. It is on screen, in the scrollback, and
// in whatever the terminal is logging.
func TestChangesTabShowsNoSecrets(t *testing.T) {
	m := editModel(t)
	m = intoEditor(t, m)
	// Move to the password field and replace it.
	m = up(m, tea.KeyMsg{Type: tea.KeyTab})
	m = up(m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeStr(m, "supersecret")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	m = up(m, tabKey(tabChanges))
	out := ansi.Strip(m.View())
	if strings.Contains(out, "supersecret") {
		t.Errorf("a password reached the review list:\n%s", out)
	}
	if strings.Contains(out, "secret-pw") {
		t.Errorf("the previous password reached the review list:\n%s", out)
	}
	if !strings.Contains(out, "Password") {
		t.Errorf("it should still report that the password changed:\n%s", out)
	}
}

// Quitting with staged work asks first — from q and from ctrl+c, which is the
// more reflexive way to leave.
func TestQuitGuard(t *testing.T) {
	for _, k := range []tea.KeyMsg{key2("q"), {Type: tea.KeyCtrlC}} {
		m := stageAnEdit(t, editModel(t))
		nm, cmd := m.Update(k)
		m = nm.(Model)
		if !m.quitGuard {
			t.Fatalf("%v should ask before discarding staged work", k)
		}
		if cmd != nil {
			t.Errorf("%v should not quit yet", k)
		}
		out := ansi.Strip(m.View())
		if !strings.Contains(out, "staged and not written") {
			t.Errorf("the guard should say what is at stake:\n%s", out)
		}

		// esc stays.
		m = up(m, tea.KeyMsg{Type: tea.KeyEsc})
		if m.quitGuard {
			t.Error("esc should dismiss the guard")
		}
		if m.dirtyCount() != 1 {
			t.Error("and keep the change")
		}
	}
}

// With nothing staged, quitting is immediate — the guard must not become a
// nuisance in the read-only case, which is most of the time.
func TestQuitIsImmediateWhenClean(t *testing.T) {
	m := editModel(t)
	nm, cmd := m.Update(key2("q"))
	if nm.(Model).quitGuard {
		t.Error("a clean session should quit without asking")
	}
	if cmd == nil {
		t.Error("q should quit")
	}
}

// Another writer moved the file: refuse, keep the staged set, and offer to
// re-read rather than reconcile.
func TestConflictKeepsTheStagedChanges(t *testing.T) {
	m := stageAnEdit(t, editModel(t))
	m = m.onSaveDone(saveDoneMsg{source: "own", err: vaultErrChangedUnderneath()})

	if m.saveConflict != "own" {
		t.Fatalf("a conflict should be reported, got %q", m.saveConflict)
	}
	if m.dirtyCount() != 1 {
		t.Error("a conflict must not throw the user's staged work away")
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "changed on disk") || !strings.Contains(out, "Nothing was written") {
		t.Errorf("the conflict should say what happened and what did not:\n%s", out)
	}

	m = up(m, key2("r"))
	if m.saveConflict != "" {
		t.Error("r should dismiss the conflict")
	}
	if m.dirtyCount() != 1 {
		t.Error("reloading should keep the staged changes for review")
	}
}

// Keys are ignored while the file is being written: there is no safe way to act
// on a half-written state.
func TestKeysAreIgnoredWhileSaving(t *testing.T) {
	m := stageAnEdit(t, editModel(t))
	m.saving = true

	before := m.dirtyCount()
	m = up(m, key2("x"))
	m = up(m, tabKey(tabChanges))
	if m.dirtyCount() != before {
		t.Error("a keystroke during a save changed the staged set")
	}
	if m.tab == tabChanges {
		t.Error("a keystroke during a save switched tabs")
	}
}

func vaultErrChangedUnderneath() error { return vault.ErrChangedUnderneath }

// The tab files every change under the source and folder it actually lives in.
// Everything used to land under one heading, because the model's merged view was
// only populated by a reload.
func TestChangesGroupedByWhereTheyLive(t *testing.T) {
	m := stageAnEdit(t, editModel(t))
	m = up(m, tabKey(tabChanges))

	groups := m.groupChanges()
	if len(groups) == 0 {
		t.Fatal("a staged change should produce a group")
	}
	for _, g := range groups {
		if g.source == "" {
			t.Error("every group belongs to a source")
		}
		if g.path == "" {
			t.Errorf("the change is in a folder, but it was filed at the root: %+v", g)
		}
	}

	out := ansi.Strip(m.View())
	if !strings.Contains(out, "›") && !strings.Contains(out, groups[0].path) {
		t.Errorf("the folder should appear as a heading:\n%s", out)
	}
}

// z folds what the cursor is on; Z folds the lot. Same keys as the vault tree.
func TestChangesFolding(t *testing.T) {
	m := up(stageAnEdit(t, editModel(t)), tabKey(tabChanges))

	rows := m.changeRows(m.contentW())
	full := len(rows)
	hunks := 0
	for _, r := range rows {
		if r.kind == rowHunk {
			hunks++
		}
	}
	if hunks == 0 {
		t.Fatal("an edit should show a hunk to fold")
	}

	m = up(m, key2("z")) // the cursor starts on the change
	if got := len(m.changeRows(m.contentW())); got != full-hunks {
		t.Errorf("z should hide this change's %d hunk rows: %d rows, was %d", hunks, got, full)
	}
	m = up(m, key2("z"))
	if got := len(m.changeRows(m.contentW())); got != full {
		t.Errorf("z again should bring them back: %d rows, want %d", got, full)
	}

	m = up(m, key2("Z"))
	for _, r := range m.changeRows(m.contentW()) {
		if r.kind == rowChange || r.kind == rowHunk {
			t.Errorf("Z should fold every group, %q is still shown", r.text())
		}
	}
	m = up(m, key2("Z"))
	if got := len(m.changeRows(m.contentW())); got != full {
		t.Errorf("Z again should open everything: %d rows, want %d", got, full)
	}
}

// x on a folder heading takes back everything filed under it.
func TestRevertAWholeGroup(t *testing.T) {
	m := up(stageAnEdit(t, editModel(t)), tabKey(tabChanges))
	if m.dirtyCount() == 0 {
		t.Fatal("nothing staged to revert")
	}

	// Onto the heading above the change.
	rows := m.changeRows(m.contentW())
	for i, idx := range selectableRows(rows) {
		if rows[idx].kind == rowFolder {
			m.chgSel = i
			break
		}
	}
	m = up(m, key2("x"))
	if m.dirtyCount() != 0 {
		t.Errorf("x on a heading should revert its group, %d left", m.dirtyCount())
	}
	if !strings.Contains(m.flash, "reverted") {
		t.Errorf("it should say what it did, got %q", m.flash)
	}
}

// The strikethrough belongs to the name of the thing being deleted, and to
// nothing else. Striking "moved to the recycle bin" says the move is cancelled,
// which is the opposite of what is about to happen — and the selection must not
// smear it across the row either.
func TestOnlyTheNameIsStruckThrough(t *testing.T) {
	c := edit.Change{State: edit.Deleted, Kind: edit.DeleteEntry, Title: "PrismaCloud",
		Detail: "moved to the recycle bin"}
	segs := changeHeading(c, false, "", 80)

	var name, detail rowSeg
	for _, s := range segs {
		switch {
		case strings.Contains(s.text, "PrismaCloud"):
			name = s
		case strings.Contains(s.text, "recycle bin"):
			detail = s
		}
	}
	if name.text == "" || detail.text == "" {
		t.Fatalf("expected a name and a summary segment, got %+v", segs)
	}
	if !name.style.GetStrikethrough() {
		t.Error("the name of a deleted thing should be struck through")
	}
	if detail.style.GetStrikethrough() {
		t.Error("the summary describes what will happen; striking it says it will not")
	}

	// Under the cursor the row keeps both: nothing is re-styled, a background is
	// put behind what is already there.
	row := changeRow{kind: rowChange, segs: segs}
	plain, selected := row.render(80, false), row.render(80, true)
	if ansiStrip(plain) != ansiStrip(selected) {
		t.Errorf("selection changed the text:\n%q\n%q", ansiStrip(plain), ansiStrip(selected))
	}
	if strings.Count(selected, "\x1b[9m") != strings.Count(plain, "\x1b[9m") {
		t.Error("selection changed how much of the row is struck through")
	}
}

// A folder and an entry with the same name are very different things to be
// deleting, so the row says which it is.
func TestChangeRowsNameTheKindOfThing(t *testing.T) {
	entry := changeHeading(edit.Change{State: edit.Deleted, Kind: edit.DeleteEntry, Title: "same"}, false, "", 60)
	group := changeHeading(edit.Change{State: edit.Deleted, Kind: edit.DeleteGroup, Title: "same"}, false, "", 60)

	var eText, gText string
	for _, s := range entry {
		eText += s.text
	}
	for _, s := range group {
		gText += s.text
	}
	if eText == gText {
		t.Errorf("an entry and a folder render identically: %q", eText)
	}
	i := ic()
	if !strings.Contains(eText, i.entry) {
		t.Errorf("an entry change should carry the entry icon: %q", eText)
	}
	if !strings.Contains(gText, i.folder) {
		t.Errorf("a folder change should carry the folder icon: %q", gText)
	}
}
