package tui

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault"
)

// onDisk saves and reports what the file holds, as paths.
func onDisk(t *testing.T, m Model, path string) []string {
	t.Helper()
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
	v := h.Snapshot()
	var got []string
	for _, e := range v.Entries {
		got = append(got, e.Path+"/"+e.Title)
	}
	for _, f := range v.Folders {
		got = append(got, f.Path+"/")
	}
	sort.Strings(got)
	return got
}

func has(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}

// The reduction keeps the *first* Seq of a collapsed chain, and the set used to
// be applied in Seq order — so an operation could sort before something it
// depends on and the whole save failed. Three shapes, each reachable with four
// keystrokes.
func TestASetCanAlwaysBeSaved(t *testing.T) {
	t.Run("move into a folder created after the first move", func(t *testing.T) {
		m, path := walkModel(t)
		m = m.expandAll(true)

		m = up(onEntry(t, m, "db", "db-prod"), key2("m"))
		m = up(pickDestination(t, m, "Net"), key2("enter"))

		m = up(onRow(t, m, "Infra"), key2("N"))
		m = typeStr(m, "Later")
		m = up(m, key2("enter"))

		m = up(onEntry(t, m, "Net", "db-prod"), key2("m"))
		m = up(pickDestination(t, m, "Later"), key2("enter"))

		got := onDisk(t, m, path)
		if !has(got, "Infra/Later/db-prod") {
			t.Errorf("the entry should be in the folder made for it:\n%s", strings.Join(got, "\n"))
		}
	})

	t.Run("permanently delete a folder, then move something out of it", func(t *testing.T) {
		m, path := walkModel(t)
		m = m.expandAll(true)

		m = up(onRow(t, m, "db"), key2("D")) // no recycle bin to rescue it from
		m = up(onEntry(t, m, "db", "db-prod"), key2("m"))
		m = up(pickDestination(t, m, "Net"), key2("enter"))

		got := onDisk(t, m, path)
		if !has(got, "Net/db-prod") {
			t.Errorf("the entry moved out should have survived:\n%s", strings.Join(got, "\n"))
		}
		if has(got, "Infra/db/") {
			t.Errorf("and the folder should be gone:\n%s", strings.Join(got, "\n"))
		}
	})

	t.Run("create inside a folder, then delete the folder", func(t *testing.T) {
		m, path := walkModel(t)
		m = m.expandAll(true)

		m = up(onRow(t, m, "Net"), key2("n"))
		m = typeStr(m, "brand-new")
		m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
		m = up(onRow(t, m, "Net"), key2("d"))

		// The creation is cancelled: the file never saw it, so there is nothing
		// to delete and nothing to put in a bin.
		for _, c := range m.chg.Diff() {
			if c.Title == "brand-new" {
				t.Errorf("the review still lists an entry the file never had: %+v", c)
			}
		}
		got := onDisk(t, m, path)
		for _, p := range got {
			if strings.Contains(p, "brand-new") {
				t.Errorf("it was written anyway, into %q:\n%s", p, strings.Join(got, "\n"))
			}
		}
	})
}

// The order the rules do not constrain is the order the reader staged things
// in, so a review reads in the sequence they built it.
func TestOrderingKeepsStagingOrderWhereItCan(t *testing.T) {
	m, _ := walkModel(t)
	m = m.expandAll(true)

	m = up(onEntry(t, m, "db", "db-prod"), key2("e"))
	m.editForm = m.editForm.setValue("username", "one")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	m = up(onRow(t, m, "Net"), key2("r"))
	m.inlineInput.SetValue("Networks")
	m = up(m, key2("enter"))

	m = up(onEntry(t, m, "db", "db-stage"), key2("d"))

	seqs := make([]int, 0, 3)
	for _, op := range m.chg.Effective() {
		seqs = append(seqs, op.Seq)
	}
	if !sort.IntsAreSorted(seqs) {
		t.Errorf("nothing here constrains the order; it should be as staged: %v", seqs)
	}
}

// "Save and quit" goes through the save confirmation.
//
// It used to call saveCmd directly, so the one screen naming the file, the
// backup and what is about to stop existing never appeared. A set holding a
// permanent deletion was written by an ↵ pressed out of habit, on a modal that
// had said only "1 change(s) are staged and not written."
func TestQuitAndSaveStillAsks(t *testing.T) {
	t.Setenv("HARMOS_NERDFONT", "0")
	m, path := walkModel(t)
	m = m.expandAll(true)
	before := fileBytes(t, path)

	m = up(onRow(t, m, "db"), key2("D")) // permanent: the irreversible half
	nm, cmd := m.Update(key2("q"))
	m = nm.(Model)
	if !m.quitGuard || cmd != nil {
		t.Fatal("q with staged work should ask, not quit")
	}

	// The first choice is "Save and quit"; take it the way a reader would.
	nm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if cmd != nil {
		t.Error("↵ on the quit guard should not start a write on its own")
	}
	if !m.saveConfirm {
		t.Fatal("it should raise the save confirmation")
	}
	if after := fileBytes(t, path); after != before {
		t.Fatal("something was written before the confirmation")
	}

	out := ansi.Strip(m.View())
	for _, want := range []string{"Write these changes?", filepath.Base(path), "backup:", "deleted permanently"} {
		if !strings.Contains(out, want) {
			t.Errorf("the confirmation should name %q:\n%s", want, out)
		}
	}
	// And a permanent deletion still leads with Cancel, so the habit costs
	// nothing here either.
	if m.confirmSel != 1 {
		t.Errorf("confirmSel = %d, want Cancel", m.confirmSel)
	}
}

// The conflict guard is not one-shot, and reloading does not licence a silent
// overwrite.
//
// onSaveDone reopens the handle to throw away a half-applied database, which
// gives it a fresh fingerprint — so the vault's own guard could not fire twice.
// Conflict → esc → ^s wrote the file with nothing said, and Reload → ^s wrote
// the staged drafts over whatever the other writer had put there.
func TestAConflictHasToBeAnswered(t *testing.T) {
	m := stageAnEdit(t, editModel(t))

	m = m.onSaveDone(saveDoneMsg{source: "own", err: vaultErrChangedUnderneath()})
	if m.saveConflict != "own" {
		t.Fatalf("a conflict should be reported, got %q", m.saveConflict)
	}
	if m.confirmSel != 1 {
		t.Error("the dangerous half is overwriting, so it must not be the default")
	}
	out := ansi.Strip(m.View())
	for _, want := range []string{"Overwrite", "does not merge"} {
		if !strings.Contains(out, want) {
			t.Errorf("the screen should say %q:\n%s", want, out)
		}
	}

	// Walking away and trying again asks again.
	m = up(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.saveConflict != "" {
		t.Fatal("esc should dismiss the screen")
	}
	m, cmd := m.askToSave()
	if cmd != nil || m.saveConfirm {
		t.Error("^s on a source that moved under us must not go straight to the write")
	}
	if m.saveConflict != "own" {
		t.Errorf("it should ask again, got %q", m.saveConflict)
	}

	// So does reloading: it shows what they did, it does not merge it.
	m = up(m, key2("r"))
	if m.dirtyCount() != 1 {
		t.Error("reloading keeps the staged work")
	}
	m, _ = m.askToSave()
	if m.saveConflict != "own" {
		t.Error("after a reload the write still has to be agreed to")
	}

	// Only saying overwrite out loud lets it through.
	m = up(m, key2("o"))
	if m.saveConflict != "" {
		t.Fatal("choosing overwrite should close the screen")
	}
	if !strings.Contains(m.flash, "overwriting") {
		t.Errorf("and say what was chosen: %q", m.flash)
	}
	m, _ = m.askToSave()
	if !m.saveConfirm {
		t.Error("now the save confirmation should come up")
	}
}

// x reverts the row, not one operation of it.
//
// A row's Seq is the *first* of a collapsed chain, and x used to Revert that
// number — so the later halves stayed staged, the row stayed where it was, and
// the diff showed a "before" the file never held: two edits to a username
// reverted to "step-one → step-two".
func TestRevertUndoesTheWholeRow(t *testing.T) {
	for _, c := range []struct {
		name  string
		stage func(*testing.T, Model) Model
	}{
		{"two edits of one entry", func(t *testing.T, m Model) Model {
			for _, v := range []string{"step-one", "step-two"} {
				m = up(onEntry(t, m, "db", "db-prod"), key2("e"))
				m.editForm = m.editForm.setValue("username", v)
				m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
			}
			return m
		}},
		{"two renames of one folder", func(t *testing.T, m Model) Model {
			for _, v := range []string{"Net-one", "Net-two"} {
				m = up(onRow(t, m, currentNetName(m)), key2("r"))
				m.inlineInput.SetValue(v)
				m = up(m, key2("enter"))
			}
			return m
		}},
		{"two moves of one entry", func(t *testing.T, m Model) Model {
			m = up(onEntry(t, m, "db", "db-prod"), key2("m"))
			m = up(pickDestination(t, m, "Net"), key2("enter"))
			m = up(onEntry(t, m, "Net", "db-prod"), key2("m"))
			m = up(pickDestination(t, m, "Infra"), key2("enter"))
			return m
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, _ := walkModel(t)
			m = c.stage(t, m.expandAll(true))
			if m.dirtyCount() == 0 {
				t.Fatal("nothing staged")
			}

			ch := up(m.switchTab(tabChanges), key2("x"))
			if n := ch.dirtyCount(); n != 0 {
				t.Errorf("x left %d change(s) staged: %v", n, ch.chg.Effective())
			}
			if !strings.Contains(ch.flash, "reverted") {
				t.Errorf("flash = %q", ch.flash)
			}
		})
	}
}

// currentNetName is whatever the Net folder is called right now, since the
// rename cases keep changing it.
func currentNetName(m Model) string {
	for _, n := range rowNames(m) {
		if strings.HasPrefix(n, "Net") {
			return n
		}
	}
	return "Net"
}

// A source that once conflicted must not refuse every later save. The mark
// exists to stop a set built against gone content being written; once that set
// is written there is no set left to stop.
func TestAConflictMarkDoesNotOutliveItsChanges(t *testing.T) {
	m := stageAnEdit(t, editModel(t))
	m = m.onSaveDone(saveDoneMsg{source: "own", err: vaultErrChangedUnderneath()})
	if !m.stale["own"] {
		t.Fatal("the conflict should be remembered")
	}

	m = m.onSaveDone(saveDoneMsg{source: "own"}) // and then it went through
	if m.stale["own"] {
		t.Error("a written source is not stale")
	}
	if m.saveConflict != "" {
		t.Error("and no conflict is pending")
	}

	// And the next set of work is asked about normally rather than refused for a
	// conflict that is over.
	next := stageAnEdit(t, editModel(t))
	next.stale = m.stale
	next, _ = next.askToSave()
	if next.saveConflict != "" {
		t.Errorf("a later save was refused for a conflict that is over: %q", next.saveConflict)
	}
	if !next.saveConfirm {
		t.Error("it should have raised the confirmation")
	}
}
