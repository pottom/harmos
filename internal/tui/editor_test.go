package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault"
	"github.com/pottom/harmos/internal/vault/vaulttest"
)

// editModel is a model over a real, writable kdbx — the editor talks to a handle
// to read a draft and to mint an identity, so a fixture is not enough.
func editModel(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/v.kdbx"
	vaulttest.Write(t, path, vaulttest.RecycleBin())

	h, err := vault.OpenHandle(path, "own", vault.Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	v := h.Snapshot()

	m := New(v.Entries, v.Folders, "", 30*time.Second)
	m = up(m, tea.WindowSizeMsg{Width: 110, Height: 34})
	m.handles = map[string]*vault.Handle{"own": h}
	m.writeOK = map[string]bool{"own": true}
	return m
}

// Onto the entry, then into the editor.
func intoEditor(t *testing.T, m Model) Model {
	t.Helper()
	m = up(m, tea.KeyMsg{Type: tea.KeyTab}) // tree → entry table
	if m.selEntry() == nil {
		t.Fatal("expected an entry under the cursor")
	}
	m = up(m, key2("e"))
	if m.edit != editEntry {
		t.Fatalf("e should open the editor, got mode %d", m.edit)
	}
	return m
}

// The one that must never regress. A form with free text cannot share a keyboard
// with a single-letter quit: typing "q" into a title would end the session and
// lose everything staged.
func TestEditorSwallowsQAndHelp(t *testing.T) {
	m := intoEditor(t, editModel(t))

	m = up(m, key2("q"))
	if m.edit != editEntry {
		t.Fatal("q must not quit, or reach anything else, while the editor is open")
	}
	m = up(m, key2("?"))
	if m.help {
		t.Error("? must not open help while the editor is open")
	}
	// And it goes into the field, where it belongs.
	if !strings.Contains(m.editForm.Value("title"), "q") {
		t.Errorf("q should have been typed into the title, got %q", m.editForm.Value("title"))
	}
}

// Staging changes nothing on disk. That is the whole promise of the editor.
func TestStagedEditDoesNotTouchDisk(t *testing.T) {
	m := editModel(t)
	path := m.handles["own"].Path()
	before, err := readFile(path)
	if err != nil {
		t.Fatal(err)
	}

	m = intoEditor(t, m)
	m = typeStr(m, "-edited")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.edit != editNone {
		t.Fatal("submitting should close the editor")
	}
	if m.dirtyCount() != 1 {
		t.Errorf("one change should be staged, got %d", m.dirtyCount())
	}
	after, err := readFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("staging an edit wrote to the file")
	}
}

// A staged edit has to be visible where the entry is, not only in a tab you have
// to go and look at.
func TestStagedEditColoursTheRow(t *testing.T) {
	m := intoEditor(t, editModel(t))
	m = typeStr(m, "-edited")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	states := m.chg.State()
	if len(states) != 1 {
		t.Fatalf("expected one changed target, got %v", states)
	}
	for _, st := range states {
		if st != edit.Modified {
			t.Errorf("state = %v, want Modified", st)
		}
		_, marker := changeStyle(st)
		if strings.TrimSpace(marker) == "" {
			t.Error("a changed row needs a non-colour marker")
		}
	}
}

// The editor says which mode you are in, twice: an amber border and a word.
func TestEditorShowsItIsAMode(t *testing.T) {
	m := intoEditor(t, editModel(t))
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "-- EDIT --") {
		t.Errorf("the editor should say it is a mode:\n%s", out)
	}
}

// Deleting stages straight away — no prompt, because nothing is written — and
// says which of the two deletions it staged. With the recycle bin off, a "move
// to bin" delete is permanent, and saying otherwise is a lie.
func TestDeleteStagesWithoutAskingAndSaysWhichKind(t *testing.T) {
	m := editModel(t)
	m = up(m, tea.KeyMsg{Type: tea.KeyTab})
	m = up(m, key2("d"))
	if m.edit != editNone {
		t.Fatalf("staging a delete must not open a modal, got mode %d", m.edit)
	}
	if m.dirtyCount() != 1 {
		t.Fatalf("d should stage one deletion, staged %d", m.dirtyCount())
	}
	if !strings.Contains(m.flash, "recycle bin") {
		t.Errorf("with a bin, it should say so, got %q", m.flash)
	}
	if !strings.Contains(m.flash, "undoes it") {
		t.Errorf("it should say how to take it back, got %q", m.flash)
	}

	// Now with the bin switched off.
	m2 := editModel(t)
	m2.handles["own"].DisableRecycleBinForTest()
	m2 = up(m2, tea.KeyMsg{Type: tea.KeyTab})
	m2 = up(m2, key2("d"))
	if !strings.Contains(m2.flash, "permanently") {
		t.Errorf("with no bin, a plain delete is permanent and must say so, got %q", m2.flash)
	}
	if !strings.Contains(m2.flash, "no recycle bin") {
		t.Errorf("and should explain why, got %q", m2.flash)
	}
}

// The one confirmation in the flow is the write, and it leads with Cancel when
// the set contains something the vault cannot get back.
func TestSaveConfirmLeadsWithCancelOnPermanentDelete(t *testing.T) {
	m := editModel(t)
	m = up(m, tea.KeyMsg{Type: tea.KeyTab})
	m = up(m, key2("d")) // to the bin: recoverable
	m = up(m, tabKey(tabChanges))
	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if !m.saveConfirm {
		t.Fatal("^s should ask before writing")
	}
	if m.confirmSel != 0 {
		t.Errorf("an ordinary write should lead with Write, focus was %d", m.confirmSel)
	}

	m2 := editModel(t)
	m2 = up(m2, tea.KeyMsg{Type: tea.KeyTab})
	m2 = up(m2, key2("D")) // permanent
	m2 = up(m2, tabKey(tabChanges))
	m2 = up(m2, tea.KeyMsg{Type: tea.KeyCtrlS})
	if m2.confirmSel != 1 {
		t.Errorf("a permanent delete should lead with Cancel, focus was %d", m2.confirmSel)
	}
	out := ansi.Strip(m2.View())
	if !strings.Contains(out, "permanent deletion") {
		t.Errorf("the write confirmation must name the permanent deletion:\n%s", out)
	}
	// enter on the leading button must not write.
	m2 = up(m2, tea.KeyMsg{Type: tea.KeyEnter})
	if m2.saving {
		t.Error("enter on Cancel started a write")
	}
}

// A locked source keeps its letters free, and says what to do rather than
// appearing dead.
func TestEditKeysNeedAnUnlockedSource(t *testing.T) {
	m := editModel(t)
	m.writeOK = map[string]bool{} // locked again

	m = up(m, tea.KeyMsg{Type: tea.KeyTab})
	m = up(m, key2("e"))
	if m.edit != editNone {
		t.Error("a locked source must not open the editor")
	}
	if !strings.Contains(m.flash, "^w") {
		t.Errorf("it should say how to unlock, got %q", m.flash)
	}
}

// Creating stages a creation, and the entry gets its identity before the file
// has heard of it — so an edit staged afterwards names the same target.
func TestNewEntryStagesACreation(t *testing.T) {
	m := editModel(t)
	before := m.handles["own"].Snapshot()

	m = up(m, key2("n")) // on a folder in the tree
	if m.edit != editEntry || !m.editNew {
		t.Fatalf("n should open a new-entry editor, got mode %d new=%v", m.edit, m.editNew)
	}
	m = typeStr(m, "brand new")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	ops := m.chg.Effective()
	if len(ops) != 1 || ops[0].Kind != edit.CreateEntry {
		t.Fatalf("expected one creation, got %v", ops)
	}
	if ops[0].Target == "" {
		t.Error("a creation needs a target identity from the start")
	}
	// Minting must leave nothing behind.
	if after := m.handles["own"].Snapshot(); len(after.Entries) != len(before.Entries) {
		t.Errorf("minting an id changed the vault: %d -> %d", len(before.Entries), len(after.Entries))
	}
}

// The generator is the one the Generate tab configured, so the two cannot
// disagree about what a good password is.
func TestGeneratorFillsThePasswordField(t *testing.T) {
	m := intoEditor(t, editModel(t))
	before := m.editForm.Raw("password")

	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlG})
	after := m.editForm.Raw("password")
	if after == before || after == "" {
		t.Errorf("ctrl+g should roll a new password, got %q", after)
	}
	if strings.Contains(ansi.Strip(m.View()), after) {
		t.Error("the generated password must still be masked on screen")
	}
}

func readFile(p string) ([]byte, error) { return os.ReadFile(p) }
