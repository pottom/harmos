package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	gokeepasslib "github.com/tobischo/gokeepasslib/v3"

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

// d acts on what the cursor is on. It used to act on the entry table's selected
// row even while the tree had focus, so deleting a folder deleted an entry
// inside it — the wrong object, with nothing on screen to say so.
func TestDeleteTargetsWhatTheCursorIsOn(t *testing.T) {
	m := editModel(t)

	var folder *node
	for i, tl := range m.visible() {
		if tl.node.id != "" {
			m.tsel, m.focus, folder = i, 0, tl.node
			break
		}
	}
	if folder == nil {
		t.Fatal("no folder row in the tree")
	}
	m = up(m, key2("d"))
	ops := m.chg.Effective()
	if len(ops) != 1 || ops[0].Kind != edit.DeleteGroup {
		t.Fatalf("d on a folder row should stage a folder deletion, got %+v", ops)
	}
	if ops[0].Target != folder.id {
		t.Errorf("staged %q, want the folder under the cursor %q", ops[0].Target, folder.id)
	}
	if ops[0].Name != folder.name {
		t.Errorf("a staged folder deletion needs its name for the diff, got %q", ops[0].Name)
	}

	// In the table, the same key means the entry.
	m2 := up(editModel(t), tea.KeyMsg{Type: tea.KeyTab})
	entry := m2.selEntry()
	m2 = up(m2, key2("d"))
	ops2 := m2.chg.Effective()
	if len(ops2) != 1 || ops2[0].Kind != edit.DeleteEntry || ops2[0].Target != entry.ID {
		t.Fatalf("d in the entry table should stage that entry, got %+v", ops2)
	}
}

// A staged deletion stays where it was and is marked there: the row keeps its
// place in the tree, takes the delete colour, and is struck through — with the
// icon following, cursor or no cursor.
func TestDeletedFolderStaysAndIsMarked(t *testing.T) {
	m := editModel(t)
	var folder *node
	for i, tl := range m.visible() {
		if tl.node.id != "" {
			m.tsel, m.focus, folder = i, 0, tl.node
			break
		}
	}
	rowsBefore := len(m.visible())
	m = up(m, key2("d"))

	if len(m.visible()) != rowsBefore {
		t.Errorf("a folder staged for deletion must stay in the tree: %d rows, was %d",
			len(m.visible()), rowsBefore)
	}

	chg := m.changeStates()[folder]
	if chg.own != edit.Deleted {
		t.Fatalf("the folder's own state should be Deleted, got %v", chg)
	}
	name, icon, _, marker := m.treeRowStyle(folder, chg)
	del, delMarker := changeStyle(edit.Deleted)
	if name.GetForeground() != del.GetForeground() {
		t.Error("a deleted row's name must take the delete colour")
	}
	if icon.GetForeground() != del.GetForeground() {
		t.Error("its icon must take it too — an icon left in the writable colour reads as 'fine'")
	}
	if name.GetStrikethrough() {
		t.Error("nothing is struck through: the marker is the non-colour signal")
	}
	if marker != delMarker {
		t.Errorf("marker = %q, want the delete glyph %q", marker, delMarker)
	}

	// And the parent above it is marked as containing a deletion, without being
	// dressed up as deleted itself.
	parent := parentOf(m.roots)[folder]
	if parent != nil {
		pc := m.changeStates()[parent]
		if pc.own != 0 {
			t.Errorf("the parent is not being deleted; own = %v", pc.own)
		}
		if pc.inside != edit.Deleted {
			t.Errorf("the parent should show that something inside it is going, got %v", pc.inside)
		}
		pn, _, _, pm := m.treeRowStyle(parent, pc)
		if pn.GetStrikethrough() {
			t.Error("a folder that merely contains a deletion must not be struck through")
		}
		if pm == "" {
			t.Error("but it should carry a marker, so a collapsed folder still says so")
		}
	}
}

// The delete key is a toggle: the same key on the same row takes it back.
func TestDeleteKeyToggles(t *testing.T) {
	m := up(editModel(t), tea.KeyMsg{Type: tea.KeyTab})

	m = up(m, key2("d"))
	if m.dirtyCount() != 1 {
		t.Fatalf("d should stage a deletion, staged %d", m.dirtyCount())
	}
	m = up(m, key2("d"))
	if m.dirtyCount() != 0 {
		t.Fatalf("d again should take it back, %d still staged", m.dirtyCount())
	}
	if !strings.Contains(m.flash, "no longer staged") {
		t.Errorf("it should say what it did, got %q", m.flash)
	}

	// D on a row already staged for the bin changes which deletion it is rather
	// than un-staging: otherwise upgrading takes two presses with an un-staged
	// row in between, which reads as the key having failed.
	m = up(m, key2("d"))
	m = up(m, key2("D"))
	ops := m.chg.Effective()
	if len(ops) != 1 {
		t.Fatalf("expected exactly one deletion, got %d", len(ops))
	}
	if !ops[0].Perm {
		t.Error("D over a bin deletion should make it permanent")
	}
	m = up(m, key2("D"))
	if m.dirtyCount() != 0 {
		t.Errorf("D again should take it back, %d still staged", m.dirtyCount())
	}

	// An edit staged earlier survives the toggle — only the deletion is undone.
	m2 := intoEditor(t, editModel(t))
	m2 = typeStr(m2, "-edited")
	m2 = up(m2, tea.KeyMsg{Type: tea.KeyEnter})
	m2 = up(m2, key2("d"))
	m2 = up(m2, key2("d"))
	if m2.dirtyCount() != 1 {
		t.Errorf("the earlier edit should survive, %d changes staged", m2.dirtyCount())
	}
	if ops := m2.chg.Effective(); len(ops) != 1 || ops[0].Kind != edit.EditEntry {
		t.Errorf("what survived should be the edit, got %+v", ops)
	}
}

// A folder deletion is one operation — one decision, one undo — but everything
// inside it is going, and the screen has to say so.
func TestFolderDeletionCarriesItsContents(t *testing.T) {
	m := editModel(t)

	var folder *node
	for i, tl := range m.visible() {
		if tl.node.id != "" && len(tl.node.entries) > 0 {
			m.tsel, m.focus, folder = i, 0, tl.node
			break
		}
	}
	if folder == nil {
		t.Fatal("no folder with entries in the fixture")
	}
	m = up(m, key2("d"))

	if n := len(m.chg.Effective()); n != 1 {
		t.Fatalf("a folder deletion is one operation, got %d", n)
	}

	// The entries inside it read as going, without a marker of their own.
	doomed := m.doomedPrefixes()
	for _, e := range folder.entries {
		if !atOrUnderDoomedFolder(doomed, e.Source, e.Path) {
			t.Errorf("%q is inside the deleted folder but does not know it", e.Title)
		}
		if m.chg.StateOf(e.ID) != 0 {
			t.Errorf("%q must not be staged separately — one folder, one undo", e.Title)
		}
	}

	// And the Changes tab names every entry that goes with it: a count is a
	// promise the reader cannot check.
	var body string
	for _, r := range m.changeRows(m.contentW()) {
		body += r.text() + "\n"
	}
	for _, e := range folder.entries {
		if !strings.Contains(body, e.Title) {
			t.Errorf("%q goes with the folder but is not listed in the review:\n%s", e.Title, body)
		}
	}
	if entries, _ := m.goesWithIt(folder.id); entries != len(folder.entries) {
		t.Errorf("counted %d entries, the folder holds %d", entries, len(folder.entries))
	}

	// Folded, where the list cannot be read, the count takes its place.
	m = up(m.switchTab(tabChanges), key2("z"))
	var folded string
	for _, r := range m.changeRows(m.contentW()) {
		if r.kind == rowChange {
			folded = r.text()
		}
	}
	if !strings.Contains(folded, "with") {
		t.Errorf("a folded folder deletion should say how much is under it, got %q", folded)
	}
}

// A sub-folder of a deleted folder wears the deletion but carries no marker:
// nothing was staged against it, so there is nothing there to undo.
func TestSubfoldersOfADeletedFolderAreMarkedButNotStaged(t *testing.T) {
	m := up(New([]vault.Entry{
		{ID: "s:1", GroupID: "s:g:2", Source: "s", Path: "Top/Inner", Title: "e", Password: secret.New("p")},
	}, []vault.Folder{
		{ID: "s:g:1", Source: "s", Path: "Top", Name: "Top"},
		{ID: "s:g:2", Source: "s", ParentID: "s:g:1", Path: "Top/Inner", Name: "Inner"},
	}, "", 30*time.Second), tea.WindowSizeMsg{Width: 100, Height: 30})
	m.handles = map[string]*vault.Handle{"s": nil}

	m.chg, _ = m.chg.Add(edit.Op{Kind: edit.DeleteGroup, Source: "s", Target: "s:g:1", Name: "Top"})
	m = m.expandAll(true) // so the sub-folder is a visible row

	states := m.changeStates()
	var top, inner *node
	for _, tl := range m.visible() {
		switch tl.node.id {
		case "s:g:1":
			top = tl.node
		case "s:g:2":
			inner = tl.node
		}
	}
	if top == nil || inner == nil {
		t.Fatalf("expected both folders in the tree: top=%v inner=%v", top, inner)
	}
	if states[top].own != edit.Deleted {
		t.Errorf("the folder itself is staged: %v", states[top])
	}
	if !states[inner].doomed {
		t.Errorf("the sub-folder is going with it: %v", states[inner])
	}
	if states[inner].own != 0 {
		t.Error("but nothing was staged against the sub-folder")
	}

	del, _ := changeStyle(edit.Deleted)
	name, icon, markerStyle, marker := m.treeRowStyle(inner, states[inner])
	if name.GetForeground() != del.GetForeground() || icon.GetForeground() != del.GetForeground() {
		t.Error("a doomed sub-folder should wear the delete colour")
	}
	if name.GetStrikethrough() {
		t.Error("and not be struck through")
	}
	// It carries a marker — something must say "going" without colour — but a
	// faded one: this is a consequence of a decision made on the folder above,
	// not a decision to be undone here.
	delStyle, delMarker2 := changeStyle(edit.Deleted)
	if marker != delMarker2 {
		t.Errorf("a doomed row needs a marker readers without colour can see, got %q", marker)
	}
	if markerStyle.GetForeground() == delStyle.GetForeground() {
		t.Error("but a faded one, so it does not read as staged in its own right")
	}
}

// Marking a run of rows costs one key per row: the cursor moves on by itself.
func TestDeleteAdvancesTheCursor(t *testing.T) {
	// Two entries in one folder, which the standard fixture does not have.
	dir := t.TempDir()
	path := dir + "/two.kdbx"
	vaulttest.Write(t, path, vaulttest.RecycleBin(), vaulttest.Shape(func(db *gokeepasslib.Database) []gokeepasslib.Group {
		mk := func(title string) gokeepasslib.Entry {
			e := gokeepasslib.NewEntry()
			e.Values = append(e.Values,
				vaulttest.Val("Title", title),
				vaulttest.PVal("Password", "pw-"+title))
			e.Times = gokeepasslib.NewTimeData()
			return e
		}
		infra := gokeepasslib.NewGroup()
		infra.Name = "Infra"
		infra.Entries = []gokeepasslib.Entry{mk("one"), mk("two")}
		root := gokeepasslib.NewGroup()
		root.Name = "Root"
		root.Groups = []gokeepasslib.Group{infra}
		return []gokeepasslib.Group{root}
	}))

	h, err := vault.OpenHandle(path, "own", vault.Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	v := h.Snapshot()
	m := up(New(v.Entries, v.Folders, "", 30*time.Second), tea.WindowSizeMsg{Width: 110, Height: 34})
	m.handles = map[string]*vault.Handle{"own": h}
	m.writeOK = map[string]bool{"own": true}

	m = up(m, tea.KeyMsg{Type: tea.KeyTab}) // into the entry table
	if f := m.currentFolder(); f == nil || len(f.entries) < 2 {
		t.Fatalf("expected two entries in the folder, got %v", f)
	}

	m = up(m, key2("d"))
	if m.esel != 1 {
		t.Errorf("the cursor should move on after staging, esel = %d", m.esel)
	}
	if !strings.Contains(m.flash, "↑ then d") {
		t.Errorf("once the cursor has moved, the hint must say so: %q", m.flash)
	}

	// Two keys, two rows staged.
	m = up(m, key2("d"))
	if m.dirtyCount() != 2 {
		t.Errorf("two presses should stage two deletions, got %d", m.dirtyCount())
	}

	// Un-staging leaves the cursor where it is: it is a correction, not a run.
	m.esel = 0
	m = up(m, key2("d"))
	if m.esel != 0 {
		t.Errorf("un-staging should not move the cursor, esel = %d", m.esel)
	}
	if !strings.Contains(m.flash, "no longer staged") {
		t.Errorf("flash = %q", m.flash)
	}
}
