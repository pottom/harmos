package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	gokeepasslib "github.com/tobischo/gokeepasslib/v3"

	"github.com/pottom/harmos/internal/clip"
	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault"
	"github.com/pottom/harmos/internal/vault/vaulttest"
)

// The end-to-end walk.
//
// A real kdbx, driven through the interface the way somebody uses it: create,
// edit, rename, move, delete, review, write — then the file is reopened and
// asked what actually landed. Every assertion here is something the walk found
// broken the first time it ran, which is the argument for it existing: none of
// them showed up in a test of the operation on its own.
func walkModel(t *testing.T) (Model, string) {
	t.Helper()
	path := t.TempDir() + "/walk.kdbx"
	vaulttest.Write(t, path, vaulttest.RecycleBin(), vaulttest.Shape(func(db *gokeepasslib.Database) []gokeepasslib.Group {
		mk := func(title, user string) gokeepasslib.Entry {
			e := gokeepasslib.NewEntry()
			e.Values = append(e.Values,
				vaulttest.Val("Title", title), vaulttest.Val("UserName", user),
				vaulttest.PVal("Password", "pw-"+title), vaulttest.Val("URL", "https://"+title),
				vaulttest.Val("Notes", "line one\nline two"))
			e.Times = gokeepasslib.NewTimeData()
			return e
		}
		db2 := gokeepasslib.NewGroup()
		db2.Name = "db"
		db2.Entries = []gokeepasslib.Entry{mk("db-prod", "svc_admin"), mk("db-stage", "svc")}

		infra := gokeepasslib.NewGroup()
		infra.Name = "Infra"
		infra.Groups = []gokeepasslib.Group{db2}
		infra.Entries = []gokeepasslib.Entry{mk("jump-host", "root")}

		net := gokeepasslib.NewGroup()
		net.Name = "Net"
		net.Entries = []gokeepasslib.Entry{mk("router", "admin")}

		root := gokeepasslib.NewGroup()
		root.Name = "Root"
		root.Groups = []gokeepasslib.Group{infra, net}
		return []gokeepasslib.Group{root}
	}))

	h, err := vault.OpenHandle(path, "own", vault.Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	v := h.Snapshot()
	m := up(New(v.Entries, v.Folders, "", 30*time.Second), tea.WindowSizeMsg{Width: 100, Height: 30})
	m.handles = map[string]*vault.Handle{"own": h}
	m.writeOK = map[string]bool{"own": true}
	return m, path
}

func onRow(t *testing.T, m Model, name string) Model {
	t.Helper()
	m = m.expandAll(true)
	for i, tl := range m.visible() {
		if tl.node.name == name {
			m.tsel, m.esel, m.focus = i, 0, 0
			return m
		}
	}
	t.Fatalf("no tree row %q; rows: %v", name, rowNames(m))
	return m
}

func rowNames(m Model) []string {
	var out []string
	for _, tl := range m.visible() {
		out = append(out, tl.node.name)
	}
	return out
}

func onEntry(t *testing.T, m Model, folder, title string) Model {
	t.Helper()
	m = onRow(t, m, folder)
	f := m.currentFolder()
	for i, e := range f.entries {
		if e.Title == title {
			m.esel, m.focus = i, 1
			return m
		}
	}
	t.Fatalf("no entry %q in %q", title, folder)
	return m
}

func TestWalkEveryOperation(t *testing.T) {
	t.Setenv("HARMOS_NERDFONT", "0")
	m, path := walkModel(t)

	// A new folder, inside a folder that is closed. It has to be visible and
	// under the cursor afterwards, or the next key acts on the old selection.
	m = onRow(t, m, "Infra")
	m = up(m, key2("N"))
	if m.edit != editFolder {
		t.Fatalf("N should open the folder editor, got mode %d", m.edit)
	}
	m = typeStr(m, "Fresh")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !slices.Contains(rowNames(m), "Fresh") {
		t.Fatalf("a staged folder must appear in the tree: %v", rowNames(m))
	}
	if currentName(m) != "Fresh" {
		t.Errorf("the cursor should be on what was just made, it is on %q", currentName(m))
	}

	// An entry inside that folder — which does not exist on disk yet.
	m = up(m, key2("n"))
	if m.edit != editEntry {
		t.Fatalf("n inside a staged folder should open the editor, got mode %d (%q)", m.edit, m.flash)
	}
	m = typeStr(m, "brand-new")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	// And editing that entry, which also does not exist on disk yet.
	m = up(m, key2("e"))
	if m.edit != editEntry {
		t.Fatalf("e on a staged entry should open the editor, got mode %d (%q)", m.edit, m.flash)
	}
	m = typeStr(m, "-v2")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	if n := m.dirtyCount(); n != 2 {
		t.Errorf("a creation edited again is still one creation: %d changes staged", n)
	}

	// A rename, which the tree has to answer to.
	m = onRow(t, m, "Net")
	m = up(m, key2("r"))
	m = typeStr(m, "work")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !slices.Contains(rowNames(m), "Network") {
		t.Fatalf("a renamed folder should show its new name: %v", rowNames(m))
	}

	// An edit, a move and two deletions.
	m = onEntry(t, m, "db", "db-prod")
	m = up(m, key2("e"))
	m = typeStr(m, "-edited")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	m = onEntry(t, m, "db", "db-stage")
	m = up(m, key2("m"))
	for _, d := range m.moveDests {
		if strings.TrimSpace(d.label) == "db" {
			t.Error("the folder something is already in is not a destination")
		}
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	if strings.Contains(m.flash, "  ") {
		t.Errorf("the message should name the destination plainly: %q", m.flash)
	}

	m = onEntry(t, m, "Infra", "jump-host")
	m = up(m, key2("d"))
	m = onRow(t, m, "Network")
	m = up(m, key2("D"))

	// The review names the same things the tree does.
	c := m.switchTab(tabChanges)
	review := ansi.Strip(c.View())
	for _, want := range []string{"Fresh", "brand-new-v2", "db-prod-edited", "db-stage", "jump-host", "router"} {
		if !strings.Contains(review, want) {
			t.Errorf("the review should mention %q:\n%s", want, review)
		}
	}
	// A folder renamed and then deleted is not renamed at all, and both surfaces
	// have to agree on what it is called.
	if strings.Contains(review, "Network") {
		t.Errorf("a deleted folder keeps the name it will have — the rename is reduced away:\n%s", review)
	}

	// Write it, then ask the file what happened.
	c, cmd := c.askToSave()
	if cmd != nil {
		t.Fatal("asking to save should not write; the confirmation does")
	}
	c, cmd = c.updateSaveConfirm("y")
	if cmd == nil {
		t.Fatal("confirming should start the write")
	}
	c = c.onSaveDone(cmd().(saveDoneMsg))
	if c.flash != "saved" {
		t.Fatalf("save: %q", c.flash)
	}
	if c.dirtyCount() != 0 {
		t.Errorf("a completed save leaves nothing staged, %d left", c.dirtyCount())
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

	want := []string{
		"Infra/Fresh/",             // the folder that was created
		"Infra/Fresh/brand-new-v2", // the entry created inside it, edited again
		"Infra/db-stage",           // moved out of db
		"Infra/db/",
		"Infra/db/db-prod-edited", // edited
		"Infra/",
		"Recycle Bin/",
		"Recycle Bin/jump-host", // binned
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("what landed on disk:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Creating at the top of a source: the row is the source, so the folder ID is
// empty — which is the root group, not a mistake. It used to be staged happily
// and then fail at the write with "malformed id".
func TestCreateAtTheTopOfASource(t *testing.T) {
	m, path := walkModel(t)
	m = onRow(t, m, "own")

	m = up(m, key2("N"))
	if m.edit != editFolder {
		t.Fatalf("N on a source root should open the folder editor, got %d", m.edit)
	}
	m = typeStr(m, "TopLevel")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

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
	for _, f := range h.Snapshot().Folders {
		if f.Path == "TopLevel" {
			return
		}
	}
	t.Error("a folder made at the top of a source should be there after the save")
}

// currentName is what the cursor is on, folder or entry.
func currentName(m Model) string {
	if m.focus == 1 {
		if e := m.selEntry(); e != nil {
			return e.Title
		}
	}
	if f := m.currentFolder(); f != nil {
		return f.name
	}
	return ""
}

// The global keys are handled before the per-tab dispatch, so a surface that
// captures what you type has to be excluded from them. It used to name only the
// search box: a password with a "q" in it quit the program mid-entry.
func TestTypedTextIsNotAGlobalKey(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	if _, err := config.WriteKdbxSource(cfg, "own", filepath.Join(dir, "own.kdbx"), "", false); err != nil {
		t.Fatal(err)
	}

	// The save-password prompt: a masked field, reached from Settings → Sources.
	m := up(New(nil, nil, cfg, 30*time.Second), tea.WindowSizeMsg{Width: 90, Height: 24})
	m = up(m, tabKey(tabSettings))
	m = up(m, tea.KeyMsg{Type: tea.KeyTab})
	m = up(m, key2("p"))
	if m.setMode != setPrompt {
		t.Fatalf("p should open the password prompt, mode %d", m.setMode)
	}
	m = typeStr(m, "Qq?1x")
	if got := m.promptInput.Value(); got != "Qq?1x" {
		t.Errorf("the prompt should have every character typed into it, it has %q", got)
	}
	if m.help {
		t.Error("? typed into a password opened the help")
	}
	if m.tab != tabSettings {
		t.Error("a digit typed into a password switched tab")
	}

	// The Generate tab's exclude field, where the digits are the point.
	g := up(New(nil, nil, "", 30*time.Second), tea.WindowSizeMsg{Width: 90, Height: 24})
	g = up(g, tabKey(tabGenerate))
	g.focus, g.genRow = 0, genExclude
	g = typeStr(g, "q1234?")
	if got := g.genOpts.Exclude; got != "q1234?" {
		t.Errorf("excluding characters should accept all of them, got %q", got)
	}
	if g.tab != tabGenerate || g.help {
		t.Error("typing into the exclude field left the tab")
	}
}

// Marking a deletion in the entry-detail split must not swap the entry the pane
// is rendering: the reader believes they are still on what they marked.
func TestDeleteInDetailKeepsTheEntry(t *testing.T) {
	m, _ := walkModel(t)
	m = onEntry(t, m, "db", "db-prod")
	m = up(m, tea.KeyMsg{Type: tea.KeyRight}) // into the detail
	if !m.detail {
		t.Fatal("→ should open the detail")
	}
	before := m.selEntry().ID

	m = up(m, key2("d"))
	if !m.detail {
		t.Fatal("staging should not leave the detail view")
	}
	if got := m.selEntry(); got == nil || got.ID != before {
		t.Errorf("the detail pane changed entry under the reader: %v", got)
	}
	if strings.Contains(m.flash, "↑ then") {
		t.Errorf("the cursor did not move, so the hint must not say it did: %q", m.flash)
	}
	// And the same key takes it back, as the hint now says.
	m = up(m, key2("d"))
	if m.dirtyCount() != 0 {
		t.Errorf("d again should undo it, %d staged", m.dirtyCount())
	}
}

// The copy chords work while the search box has focus — that is where results
// are picked, and requiring ↵ first is a rule nobody would guess.
func TestCopyChordsWhileSearching(t *testing.T) {
	m, _ := walkModel(t)
	m = up(m, key2("/"))
	m = typeStr(m, "db-prod")
	if !m.searchMode || len(m.results) == 0 {
		t.Fatalf("expected results while searching: mode=%v n=%d", m.searchMode, len(m.results))
	}
	query := m.input.Value()
	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlY})

	// True everywhere: the chord is a command, not a character, and it does not
	// take you out of the box you are typing in.
	if m.input.Value() != query {
		t.Errorf("the chord was typed into the search box: %q", m.input.Value())
	}
	if !m.searchMode {
		t.Error("and it should leave the search box where it was")
	}

	// The rest needs somewhere to copy to. CI has no clipboard on Linux and
	// none is implemented on Windows, so say what is missing rather than
	// failing for a reason that has nothing to do with the behaviour.
	if err := clip.Copy([]byte("probe")); err != nil {
		t.Skipf("no clipboard here: %v", err)
	}
	m2 := up(mustSearch(t), tea.KeyMsg{Type: tea.KeyCtrlY})
	if m2.copiedWhat != "password" {
		t.Errorf("ctrl+y should copy the selected result's password, copied %q", m2.copiedWhat)
	}
}

// mustSearch is a model with a query typed and results showing.
func mustSearch(t *testing.T) Model {
	t.Helper()
	m, _ := walkModel(t)
	m = up(m, key2("/"))
	m = typeStr(m, "db-prod")
	if len(m.results) == 0 {
		t.Fatal("expected results")
	}
	return m
}

// secondSource opens another real kdbx as a second writable source.
func secondSource(t *testing.T, m Model, name string) (Model, string) {
	t.Helper()
	path := t.TempDir() + "/" + name + ".kdbx"
	vaulttest.Write(t, path, vaulttest.RecycleBin())
	h, err := vault.OpenHandle(path, name, vault.Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	v := h.Snapshot()
	m.handles[name] = h
	m.writeOK[name] = true
	return m.rebuild(append(m.mergedEntries, v.Entries...), append(m.mergedFolders, v.Folders...)), path
}

// A save that fails halfway leaves the handle's database disagreeing with the
// file — Apply mutates operation by operation and does not roll back. The next
// save must not write that difference: it is damage nobody reviewed.
func TestFailedSaveDoesNotPoisonTheNextOne(t *testing.T) {
	m, path := walkModel(t)

	// A set that applies partway and then fails: a real deletion, then an
	// operation naming something that is not there. The interface refuses to
	// stage the second kind now (see TestNothingIsStagedIntoSomethingThatIsGoing),
	// so it goes in by hand — the point here is what a failure leaves behind.
	m = onRow(t, m, "db")
	m = up(m, key2("D"))
	m.chg, _ = m.chg.Add(edit.Op{
		Kind: edit.EditEntry, Source: "own", Target: "own:AAAAAAAAAAAAAAAAAAAAAA",
		Before: &edit.Draft{ID: "own:AAAAAAAAAAAAAAAAAAAAAA"},
		After:  &edit.Draft{ID: "own:AAAAAAAAAAAAAAAAAAAAAA", Title: "nowhere"},
	})

	c := m.switchTab(tabChanges)
	c, _ = c.askToSave()
	c, cmd := c.updateSaveConfirm("y")
	if cmd == nil {
		t.Fatal("expected the write to start")
	}
	c = c.onSaveDone(cmd().(saveDoneMsg))
	if !strings.HasPrefix(c.flash, "save failed") {
		t.Fatalf("this set should not apply: %q", c.flash)
	}

	// Give up on all of it, then make one unrelated, innocent change.
	for c.dirtyCount() > 0 {
		before := c.dirtyCount()
		c = up(c, key2("x"))
		if c.dirtyCount() >= before {
			t.Fatalf("x did not revert anything (%d staged)", c.dirtyCount())
		}
	}
	c = c.switchTab(tabVault)
	c = onEntry(t, c, "Infra", "jump-host")
	c = up(c, key2("e"))
	c = typeStr(c, "-ok")
	c = up(c, tea.KeyMsg{Type: tea.KeyEnter})

	c = c.switchTab(tabChanges)
	c, _ = c.askToSave()
	c, cmd = c.updateSaveConfirm("y")
	if cmd == nil {
		t.Fatal("expected the second write to start")
	}
	c = c.onSaveDone(cmd().(saveDoneMsg))
	if c.flash != "saved" {
		t.Fatalf("the innocent change should save: %q", c.flash)
	}

	h, err := vault.OpenHandle(path, "own", vault.Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	v := h.Snapshot()
	var titles []string
	for _, e := range v.Entries {
		titles = append(titles, e.Path+"/"+e.Title)
	}
	sort.Strings(titles)
	want := []string{"Infra/db/db-prod", "Infra/db/db-stage", "Infra/jump-host-ok", "Net/router"}
	if strings.Join(titles, " ") != strings.Join(want, " ") {
		t.Errorf("the failed set left damage on disk:\n%v\nwant:\n%v", titles, want)
	}
}

// When one source of a multi-source save fails, the ones already written are
// written: their changes come off the staged set, or a retry writes them twice.
func TestPartialSaveDoesNotRewriteWhatLanded(t *testing.T) {
	m, _ := walkModel(t)
	m, other := secondSource(t, m, "two")

	// A creation in "own" that will succeed, and a set in "two" that cannot.
	m = onRow(t, m, "Net")
	m = up(m, key2("n"))
	m = typeStr(m, "newbie")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	m = onRow(t, m, "Infra") // the second source's folder, same name
	for i, tl := range m.visible() {
		if tl.node.source == "two" && tl.node.id != "" {
			m.tsel, m.focus = i, 0
		}
	}
	m = up(m, key2("D"))
	f := m.currentFolder()
	if f == nil || f.source != "two" {
		t.Fatalf("expected to be on the second source's folder, got %v", f)
	}
	for _, e := range f.entries {
		m = m.revealTarget(e.ID, false)
		m = up(m, key2("D")) // an entry inside a folder already deleted: unappliable
	}

	c := m.switchTab(tabChanges)
	c, _ = c.askToSave()
	c, cmd := c.updateSaveConfirm("y")
	c = c.onSaveDone(cmd().(saveDoneMsg))

	// Whatever happened to "two", "own" is either written and unstaged, or not
	// written and still staged — never written and still staged.
	h, err := vault.OpenHandle(other, "two", vault.Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	_ = h
	ownStaged := c.chg.ForSource("own").Len()
	if strings.HasPrefix(c.flash, "save failed") && ownStaged > 0 {
		t.Errorf("own was written but is still staged (%d ops) — a retry would write it again", ownStaged)
	}
}

// The write lock is a lock. Staging then locking must leave the file alone.
func TestLockedSourceCannotBeSaved(t *testing.T) {
	m, path := walkModel(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	m = onEntry(t, m, "db", "db-prod")
	m = up(m, key2("e"))
	m = typeStr(m, "-sneaky")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlW}) // lock it again
	if m.writeUnlocked("own") {
		t.Fatal("^w should have locked the source")
	}

	c := m.switchTab(tabChanges)
	c, cmd := c.askToSave()
	if cmd != nil || c.saveConfirm {
		t.Fatal("a locked source must not even reach the confirmation")
	}
	if !strings.Contains(c.flash, "locked") {
		t.Errorf("and it should say why: %q", c.flash)
	}

	// Even driven past the confirmation, the writer refuses.
	c.saveConfirm = true
	c, cmd = c.updateSaveConfirm("y")
	if cmd != nil {
		c = c.onSaveDone(cmd().(saveDoneMsg))
		if !strings.Contains(c.flash, "locked") {
			t.Errorf("the save itself should refuse a locked source: %q", c.flash)
		}
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("a locked source was written")
	}
}

// Nothing may be staged into, or separately out of, a folder that is already
// staged for deletion. The design has always said so; nothing implemented it,
// and the results were quiet: an entry moved into a deleted folder was binned
// with it, a new entry was created inside the recycle bin, and deleting
// something inside a deleted folder made the set unappliable.
func TestNothingIsStagedIntoSomethingThatIsGoing(t *testing.T) {
	m, _ := walkModel(t)
	m = onRow(t, m, "Net")
	m = up(m, key2("d")) // Net is going

	// A new entry inside it.
	m = onRow(t, m, "Net")
	m = up(m, key2("n"))
	if m.edit != editNone {
		t.Errorf("n inside a deleted folder should be refused, got mode %d", m.edit)
	}
	if !strings.Contains(m.flash, "going") {
		t.Errorf("and say why: %q", m.flash)
	}

	// A new folder inside it.
	m = up(m, key2("N"))
	if m.edit != editNone {
		t.Errorf("N inside a deleted folder should be refused, got mode %d", m.edit)
	}

	// Deleting something inside it separately.
	before := m.dirtyCount()
	m = onEntry(t, m, "Net", "router")
	m = up(m, key2("d"))
	if m.dirtyCount() != before {
		t.Errorf("the entry is already going with the folder; %d changes staged", m.dirtyCount())
	}
	if !strings.Contains(m.flash, "already going") {
		t.Errorf("and it should say so: %q", m.flash)
	}

	// And it is not offered as a destination for a move.
	m = onEntry(t, m, "db", "db-prod")
	m = up(m, key2("m"))
	for _, d := range m.moveDests {
		if strings.TrimSpace(d.label) == "Net" {
			t.Error("a folder staged for deletion is not somewhere to move things to")
		}
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyEsc})
}

// A folder cannot be moved into itself. The guard lived in the vault, after the
// review and the confirmation had both said yes.
func TestAFolderIsNotItsOwnDestination(t *testing.T) {
	m, _ := walkModel(t)
	m = onRow(t, m, "Infra")
	m = up(m, key2("m"))
	if m.edit != editMove {
		t.Fatalf("m should open the picker, got mode %d", m.edit)
	}
	for _, d := range m.moveDests {
		if strings.TrimSpace(d.label) == "db" {
			t.Error("db is inside Infra; it cannot be where Infra goes")
		}
		if strings.TrimSpace(d.label) == "Infra" {
			t.Error("and Infra is not its own destination")
		}
	}
}

// Undoing a folder undoes what was staged inside it, and the confirmation says
// how many go with it.
func TestRevertingAFolderTakesItsStagedContents(t *testing.T) {
	m, _ := walkModel(t)
	m = onRow(t, m, "Infra")
	m = up(m, key2("N"))
	m = typeStr(m, "Box")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = up(m, key2("n"))
	m = typeStr(m, "inside")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.dirtyCount() != 2 {
		t.Fatalf("expected the folder and the entry staged, got %d", m.dirtyCount())
	}

	c := m.switchTab(tabChanges)
	for i, idx := range selectableRows(c.changeRows(c.contentW())) {
		if c.changeRows(c.contentW())[idx].kind == rowChange {
			c.chgSel = i
			break
		}
	}
	c = up(c, key2("x"))
	if c.dirtyCount() != 0 {
		t.Errorf("undoing the folder should take the entry with it, %d left", c.dirtyCount())
	}
	if !strings.Contains(c.flash, "dependent") {
		t.Errorf("and say how many went with it: %q", c.flash)
	}
}

// From the results list the tree cursor is not on screen, so it cannot be the
// parent for anything created there — it belongs to whichever source was last
// browsed, and staging against it produced operations whose source and parent
// came from different vaults.
func TestCreatingFromTheResultsListUsesTheResultsFolder(t *testing.T) {
	m, _ := walkModel(t)
	m, _ = secondSource(t, m, "two")

	// Park the tree cursor in the first source, then search and pick a result
	// that lives in the second.
	m = onRow(t, m, "db")
	m = up(m, key2("/"))
	m = typeStr(m, "db-prod")

	var picked *vault.Entry
	for i, r := range m.results {
		if r.Entry.Source == "two" {
			m.sel = i
			e := r.Entry
			picked = &e
			break
		}
	}
	if picked == nil {
		t.Skip("the second source has no matching entry")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter}) // leave the box, keep the results

	m = up(m, key2("N"))
	if m.edit != editFolder {
		t.Fatalf("N should open the folder editor, got mode %d (%q)", m.edit, m.flash)
	}
	m = typeStr(m, "FromResults")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	ops := m.chg.Effective()
	if len(ops) != 1 {
		t.Fatalf("expected one staged folder, got %+v", ops)
	}
	if ops[0].Source != picked.Source {
		t.Errorf("staged against %q, but the result is in %q", ops[0].Source, picked.Source)
	}
	if ops[0].Parent != "" && !strings.HasPrefix(ops[0].Parent, picked.Source+":") {
		t.Errorf("the parent %q belongs to another source", ops[0].Parent)
	}
}

// Editing an entry after moving it must not put it back: the draft it was
// loaded from came off the file, and taking the group from there undid the move
// in the projection.
func TestEditingAfterAMoveKeepsTheMove(t *testing.T) {
	m, _ := walkModel(t)
	m = onEntry(t, m, "db", "db-prod")
	id := m.selEntry().ID

	m = up(m, key2("m"))
	var dest string
	for i, d := range m.moveDests {
		if strings.TrimSpace(d.label) == "Net" {
			m.moveSel, dest = i, d.id
		}
	}
	if dest == "" {
		t.Fatal("expected Net as a destination")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	m = m.revealTarget(id, false)
	m = up(m, key2("e"))
	if m.edit != editEntry {
		t.Fatalf("e should open the editor, got %d (%q)", m.edit, m.flash)
	}
	m = typeStr(m, "-edited")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	for _, e := range m.viewEntries {
		if e.ID != id {
			continue
		}
		if e.Path != "Net" {
			t.Errorf("the entry should still be where it was moved: %q", e.Path)
		}
		if e.GroupID != dest {
			t.Errorf("and its folder should say so: %q, want %q", e.GroupID, dest)
		}
		if home := m.homeOf(id); home != dest {
			t.Errorf("homeOf says %q — the move picker would offer the folder it is in", home)
		}
		return
	}
	t.Error("the entry left the projection")
}

// An otpauth:// URI is the shared seed. The editor was printing it in full, on
// e, with no reveal keypress — while every other surface treated it as a secret.
func TestTheEditorDoesNotPrintTheTOTPSeed(t *testing.T) {
	const seed = "ZZTOTPCANARY222"
	path := t.TempDir() + "/otp.kdbx"
	vaulttest.Write(t, path, vaulttest.RecycleBin(), vaulttest.Shape(func(db *gokeepasslib.Database) []gokeepasslib.Group {
		e := gokeepasslib.NewEntry()
		e.Values = append(e.Values,
			vaulttest.Val("Title", "with-otp"),
			vaulttest.PVal("Password", "pw"),
			vaulttest.PVal("otp", "otpauth://totp/x?secret="+seed+"&digits=6&period=30"))
		e.Times = gokeepasslib.NewTimeData()
		g := gokeepasslib.NewGroup()
		g.Name = "Infra"
		g.Entries = []gokeepasslib.Entry{e}
		root := gokeepasslib.NewGroup()
		root.Name = "Root"
		root.Groups = []gokeepasslib.Group{g}
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

	m = onEntry(t, m, "Infra", "with-otp")
	m = up(m, key2("e"))
	if m.edit != editEntry {
		t.Fatalf("e should open the editor, got %d (%q)", m.edit, m.flash)
	}
	if out := ansi.Strip(m.View()); strings.Contains(out, seed) {
		t.Errorf("the TOTP seed is on screen unasked:\n%s", out)
	}
	// And it is still there to be revealed and edited.
	if !strings.Contains(m.editForm.Raw("totp"), seed) {
		t.Error("the field should still hold the URI")
	}
}

// Deleting a child and then its folder is one deletion, not two. Applying both
// pulled the child out of the folder it went with: on disk the bin held the
// child at its root and the folder beside it.
func TestDeletingAChildThenItsFolderIsOneDeletion(t *testing.T) {
	m, path := walkModel(t)

	m = onEntry(t, m, "db", "db-prod")
	m = up(m, key2("d")) // the child first
	if m.dirtyCount() != 1 {
		t.Fatalf("expected the entry staged, got %d", m.dirtyCount())
	}
	m = onRow(t, m, "db")
	m = up(m, key2("d")) // then the folder around it

	if n := m.dirtyCount(); n != 1 {
		t.Errorf("the folder subsumes the entry: %d changes staged", n)
	}
	for _, op := range m.chg.Effective() {
		if op.Kind == edit.DeleteEntry {
			t.Errorf("the entry's own deletion should be gone: %+v", op)
		}
	}

	c := m.switchTab(tabChanges)
	c, _ = c.askToSave()
	c, cmd := c.updateSaveConfirm("y")
	if cmd == nil {
		t.Fatal("expected the write to start")
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
	want := []string{"Infra/jump-host", "Net/router", "Recycle Bin/db/db-prod", "Recycle Bin/db/db-stage"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the child should have gone into the bin inside its folder:\n%v\nwant:\n%v", got, want)
	}
}

// A save leaves the tree agreeing with the file. The projection is derived from
// the staged set, so clearing the set without re-deriving showed everything that
// had just been created twice — and the obvious next move is to delete one.
func TestTheTreeMatchesTheFileAfterASave(t *testing.T) {
	m, path := walkModel(t)
	m = onRow(t, m, "db")
	m = up(m, key2("n"))
	m = typeStr(m, "made-up")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	c := m.switchTab(tabChanges)
	c, _ = c.askToSave()
	c, cmd := c.updateSaveConfirm("y")
	c = c.onSaveDone(cmd().(saveDoneMsg))
	if c.flash != "saved" {
		t.Fatalf("save: %q", c.flash)
	}

	h, err := vault.OpenHandle(path, "own", vault.Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	onDisk := len(h.Snapshot().Entries)
	if len(c.viewEntries) != onDisk {
		t.Errorf("the tree holds %d entries, the file %d", len(c.viewEntries), onDisk)
	}
	seen := 0
	for _, e := range c.viewEntries {
		if e.Title == "made-up" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("the new entry appears %d times in the tree", seen)
	}
}

// A vault file is input like any other. Control characters in what it holds
// must not reach the terminal: a tab measures one cell to the width maths and
// eight to the terminal, and "\x1b[2J" clears the screen it is drawn on.
func TestFileContentCannotDriveTheTerminal(t *testing.T) {
	const nasty = "tab\there\nsecond\x1b[31;5mANSI\x1b[2J\x00nul"

	path := t.TempDir() + "/nasty.kdbx"
	vaulttest.Write(t, path, vaulttest.Shape(func(db *gokeepasslib.Database) []gokeepasslib.Group {
		e := gokeepasslib.NewEntry()
		e.Values = append(e.Values,
			vaulttest.Val("Title", "carrier"),
			vaulttest.PVal("Password", "pw"),
			vaulttest.Val("Notes", nasty),
			vaulttest.PVal("otp", "otpauth://totp/x?secret=AAAA"+"\x1b[2J"))
		e.Tags = "clean;na\x1bsty"
		e.Times = gokeepasslib.NewTimeData()
		g := gokeepasslib.NewGroup()
		g.Name = "Infra"
		g.Entries = []gokeepasslib.Entry{e}
		root := gokeepasslib.NewGroup()
		root.Name = "Root"
		root.Groups = []gokeepasslib.Group{g}
		return []gokeepasslib.Group{root}
	}))

	h, err := vault.OpenHandle(path, "own", vault.Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	v := h.Snapshot()

	var got *vault.Entry
	for i := range v.Entries {
		if v.Entries[i].Title == "carrier" {
			got = &v.Entries[i]
		}
	}
	if got == nil {
		t.Fatal("fixture entry missing")
	}
	if strings.ContainsRune(got.Notes, '\x1b') || strings.ContainsRune(got.Notes, '\t') || strings.ContainsRune(got.Notes, 0) {
		t.Errorf("Notes still carries control characters: %q", got.Notes)
	}
	if !strings.Contains(got.Notes, "\n") {
		t.Error("but its line breaks are the point and must survive")
	}
	if strings.ContainsRune(got.TOTP, '\x1b') {
		t.Errorf("TOTP still carries an escape: %q", got.TOTP)
	}
	for _, tag := range got.Tags {
		if strings.ContainsRune(tag, '\x1b') {
			t.Errorf("a tag still carries an escape: %q", tag)
		}
	}

	// And no escape sequence from the file reaches the terminal. The text of it
	// survives — "[2J" is just characters once the ESC is gone — which is the
	// point: the reader sees what the file says without the terminal obeying it.
	m := up(New(v.Entries, v.Folders, "", 30*time.Second), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = onEntry(t, m, "Infra", "carrier")
	m = up(m, tea.KeyMsg{Type: tea.KeyRight}) // the detail pane, where Notes are shown
	for _, seq := range []string{"\x1b[2J", "\x1b[31;5m"} {
		if strings.Contains(m.View(), seq) {
			t.Errorf("an escape sequence from the file reached the terminal: %q", seq)
		}
	}
}

// Unlocking a source has to change what the interface offers. It used to change
// the footer not at all: the editing keys appeared nowhere outside the ? overlay,
// so the headline feature of v0.2 was undiscoverable from the interface.
func TestUnlockingChangesWhatTheFooterOffers(t *testing.T) {
	m, _ := walkModel(t)
	m.writeOK = map[string]bool{} // locked, as a fresh config would be
	m = onRow(t, m, "Infra")

	locked := ansi.Strip(m.hints())
	if !strings.Contains(locked, "^w") {
		t.Errorf("a locked source should name the key that unlocks it: %q", locked)
	}

	m.writeOK = map[string]bool{"own": true}
	unlocked := ansi.Strip(m.hints())
	if unlocked == locked {
		t.Fatal("the footer is identical before and after unlocking")
	}
	for _, key := range []string{"e ", "d ", "^s"} {
		if !strings.Contains(unlocked, key) {
			t.Errorf("an unlocked source should offer %q: %q", key, unlocked)
		}
	}

	// And in the entry table, where the keys mean slightly different things.
	m = up(m, tea.KeyMsg{Type: tea.KeyTab})
	table := ansi.Strip(m.hints())
	if !strings.Contains(table, "n new") {
		t.Errorf("the table should offer the new-entry key: %q", table)
	}
}

// The first source is added to a config that was read before it existed, so the
// vault stays empty. The screen has to say what to do about that.
func TestOnboardingSaysWhatHappensNext(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	m := up(New(nil, nil, cfg, 30*time.Second), tea.WindowSizeMsg{Width: 100, Height: 30})
	m.tab, m.setCat, m.focus, m.onboarding = tabSettings, catSources, 1, true

	m = up(m, key2("a"))
	if m.setMode != setForm {
		t.Fatalf("a should open the add form, mode %d", m.setMode)
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyTab}) // type toggle → name
	m = typeStr(m, "first")
	m = up(m, tea.KeyMsg{Type: tea.KeyTab}) // name → path
	m = typeStr(m, filepath.Join(dir, "v.kdbx"))
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.setMode != setList {
		t.Fatalf("the form should submit (status %q)", m.setStatus)
	}
	if !strings.Contains(m.setStatus, "restart") {
		t.Errorf("the first source needs a next step, got %q", m.setStatus)
	}
}

// Two sibling folders with the same name are two folders. The tree used to key
// itself by path, so they collapsed into one row where the last one silently won
// every operation aimed at either — and a merge produces this shape routinely.
func TestSiblingFoldersWithTheSameName(t *testing.T) {
	path := t.TempDir() + "/same.kdbx"
	vaulttest.Write(t, path, vaulttest.Shape(func(db *gokeepasslib.Database) []gokeepasslib.Group {
		mk := func(title string) gokeepasslib.Entry {
			e := gokeepasslib.NewEntry()
			e.Values = append(e.Values, vaulttest.Val("Title", title), vaulttest.PVal("Password", "pw"))
			e.Times = gokeepasslib.NewTimeData()
			return e
		}
		first := gokeepasslib.NewGroup()
		first.Name = "Same"
		first.Entries = []gokeepasslib.Entry{mk("in-first")}
		second := gokeepasslib.NewGroup()
		second.Name = "Same"
		second.Entries = []gokeepasslib.Entry{mk("in-second")}
		root := gokeepasslib.NewGroup()
		root.Name = "Root"
		root.Groups = []gokeepasslib.Group{first, second}
		return []gokeepasslib.Group{root}
	}))

	h, err := vault.OpenHandle(path, "own", vault.Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	v := h.Snapshot()
	m := up(New(v.Entries, v.Folders, "", 30*time.Second), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m.expandAll(true)

	var rows []*node
	for _, tl := range m.visible() {
		if tl.node.name == "Same" {
			rows = append(rows, tl.node)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("two folders named Same should be two rows, got %d", len(rows))
	}
	if rows[0].id == rows[1].id {
		t.Error("and they should be different folders")
	}
	for _, r := range rows {
		if len(r.entries) != 1 {
			t.Errorf("each holds its own entry, %q holds %d", r.id, len(r.entries))
		}
	}
}

// A folder whose name contains "/" is one folder. The tree used to split its
// path, growing a phantom parent row with no ID — which the writer reads as
// "the vault root", so anything created on that row went to the top of the file.
func TestAFolderNamedWithASlash(t *testing.T) {
	path := t.TempDir() + "/slash.kdbx"
	vaulttest.Write(t, path, vaulttest.Shape(func(db *gokeepasslib.Database) []gokeepasslib.Group {
		e := gokeepasslib.NewEntry()
		e.Values = append(e.Values, vaulttest.Val("Title", "victim"), vaulttest.PVal("Password", "pw"))
		e.Times = gokeepasslib.NewTimeData()
		g := gokeepasslib.NewGroup()
		g.Name = "a/b"
		g.Entries = []gokeepasslib.Entry{e}
		root := gokeepasslib.NewGroup()
		root.Name = "Root"
		root.Groups = []gokeepasslib.Group{g}
		return []gokeepasslib.Group{root}
	}))

	h, err := vault.OpenHandle(path, "own", vault.Credentials{Password: secret.New("pw")})
	if err != nil {
		t.Fatal(err)
	}
	v := h.Snapshot()
	m := up(New(v.Entries, v.Folders, "", 30*time.Second), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m.expandAll(true)

	var names []string
	for _, tl := range m.visible() {
		names = append(names, tl.node.name)
		if tl.node.name == "a" && tl.node.id == "" && tl.depth > 0 {
			t.Error("a row that belongs to no folder — anything created here goes to the vault root")
		}
	}
	if !slices.Contains(names, "a/b") {
		t.Errorf("the folder should appear under its own name: %v", names)
	}
}

// A save swallows every key while it runs and takes two Argon2 derivations per
// source. The screen used to be identical to the moment before it, so the only
// signal that anything was happening was that the program had stopped answering.
func TestASaveSaysItIsHappening(t *testing.T) {
	m, _ := walkModel(t)
	m = onEntry(t, m, "db", "db-prod")
	m = up(m, key2("e"))
	m = typeStr(m, "-x")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	c := m.switchTab(tabChanges)
	before := ansi.Strip(c.View())
	c, _ = c.askToSave()
	c, cmd := c.updateSaveConfirm("y")
	if cmd == nil {
		t.Fatal("expected the write to start")
	}
	if !c.saving {
		t.Fatal("the model should know it is saving")
	}

	during := ansi.Strip(c.View())
	if during == before {
		t.Error("the screen is identical while the write runs")
	}
	if !strings.Contains(during, "Writing") {
		t.Errorf("it should say what is happening:\n%s", during)
	}
	// And it goes away when the write lands.
	c = c.onSaveDone(cmd().(saveDoneMsg))
	if c.saving || strings.Contains(ansi.Strip(c.View()), "Writing…") {
		t.Error("and stop saying it afterwards")
	}
}

// One backup per file per run, not one per save. The interface reopens the
// handle after every successful save, and the flag that says "already backed up"
// lived on the handle — so every save left another full copy of the vault beside
// it, forever, and nothing prunes them.
func TestOneBackupPerRunAcrossSaves(t *testing.T) {
	m, path := walkModel(t)
	dir := filepath.Dir(path)

	save := func(m Model, title string) Model {
		t.Helper()
		m = onRow(t, m, "db") // whatever is in there — the titles grow as we go
		m.focus, m.esel = 1, 0
		m = up(m, key2("e"))
		m = typeStr(m, title)
		m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
		c := m.switchTab(tabChanges)
		c, _ = c.askToSave()
		c, cmd := c.updateSaveConfirm("y")
		if cmd == nil {
			t.Fatal("expected the write to start")
		}
		c = c.onSaveDone(cmd().(saveDoneMsg))
		if c.flash != "saved" {
			t.Fatalf("save: %q", c.flash)
		}
		return c.switchTab(tabVault)
	}

	backups := func() []string {
		got, err := filepath.Glob(filepath.Join(dir, "*.harmos-backup-*.kdbx"))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	m = save(m, "-one")
	if n := len(backups()); n != 1 {
		t.Fatalf("the first save takes the backup, found %d", n)
	}
	m = save(m, "-two")
	_ = save(m, "-three")
	if n := len(backups()); n != 1 {
		t.Errorf("later saves in the same run add %d more", n-1)
	}
}

// The help must name keys that work where it names them. ^w and / are bound on
// the Vault tab only, and the shared trailer advertised them on all four.
func TestHelpDoesNotPromiseKeysThatDoNothingHere(t *testing.T) {
	m, _ := walkModel(t)

	vault := strings.Join(m.keyList(60), "\n")
	for _, key := range []string{"^w", "/"} {
		if !strings.Contains(vault, key) {
			t.Errorf("the Vault help should name %q: %s", key, vault)
		}
	}

	for _, tab := range []int{tabChanges, tabGenerate, tabSettings} {
		list := strings.Join(up(m, tabKey(tab)).keyList(60), "\n")
		for _, key := range []string{"unlock / lock", "search every source"} {
			if strings.Contains(list, key) {
				t.Errorf("tab %d names %q, which is not bound there:\n%s", tab, key, list)
			}
		}
	}
}

// Staging rebuilds the tree, so it must leave the tree looking exactly as it
// did. It used to restore only the open rows and never the closed ones, so a
// folder the reader had collapsed — a source root most visibly — sprang open
// again the moment anything was marked.
func TestStagingLeavesTheTreeAsItWas(t *testing.T) {
	m, _ := walkModel(t)
	m = m.expandAll(true)

	// Close one folder and one source root, and remember the shape.
	for _, tl := range m.visible() {
		if tl.node.name == "db" {
			tl.node.expanded = false
		}
	}
	openShape := rowNames(m)

	m = onEntry(t, m, "Infra", "jump-host")
	m = up(m, key2("d"))
	if got := rowNames(m); strings.Join(got, " ") != strings.Join(openShape, " ") {
		t.Errorf("marking an entry changed the tree:\n%v\nwas:\n%v", got, openShape)
	}

	// The same for a collapsed source root, which is the one every reader hits.
	m2, _ := walkModel(t)
	for _, r := range m2.roots {
		r.expanded = false
	}
	closed := rowNames(m2)
	if len(closed) != 1 {
		t.Fatalf("a closed source should be one row, got %v", closed)
	}
	if got := rowNames(m2.restage()); strings.Join(got, " ") != strings.Join(closed, " ") {
		t.Errorf("a rebuild reopened the tree: %v", got)
	}
}

// Marking a folder leaves the cursor on the folder. The rebuild restored the
// selection through selEntry, which answers "which entry is current" even when
// the tree has focus — so marking a folder that happened to hold entries threw
// the reader into the entry table. On an empty folder nothing happened, which is
// what made it look arbitrary.
func TestMarkingAFolderKeepsTheCursorInTheTree(t *testing.T) {
	m, _ := walkModel(t)
	m = onRow(t, m, "db") // holds two entries
	if f := m.currentFolder(); f == nil || len(f.entries) == 0 {
		t.Fatal("this test needs a folder with entries in it")
	}
	rows := rowNames(m)
	at := m.tsel

	m = up(m, key2("d"))
	if m.focus != 0 {
		t.Errorf("marking a folder should not move into the entry table (focus %d)", m.focus)
	}
	// It advances one row, as marking does everywhere — but along the tree,
	// which is where the cursor was.
	if m.tsel != at+1 && at+1 < len(rows) {
		t.Errorf("the cursor should step to the next row of the tree: %d → %d", at, m.tsel)
	}

	// And the same for an empty one, which always behaved.
	m2, _ := walkModel(t)
	m2 = onRow(t, m2, "Net")
	m2 = up(m2, key2("d"))
	if m2.focus != 0 {
		t.Errorf("an empty folder should behave the same (focus %d)", m2.focus)
	}
}

// The invariant the two fixes above are instances of: staging changes what the
// vault will contain, never how the reader is looking at it.
//
// Every staging rebuilds the tree, because the tree shows the vault as it will
// be. So every staging is a chance to lose the reader's place — and the two ways
// it did were found by hand, one at a time, because the tests until now covered
// each key on its own and nothing covered what they share.
func TestStagingNeverMovesTheReader(t *testing.T) {
	type step struct {
		name string
		on   string // the tree row to stand on
		keys []tea.KeyMsg
	}
	steps := []step{
		{"delete an entry", "db", []tea.KeyMsg{key2("d")}},
		{"delete a folder", "db", []tea.KeyMsg{key2("d")}},
		{"delete permanently", "db", []tea.KeyMsg{key2("D")}},
		{"rename a folder", "db", []tea.KeyMsg{key2("r"), key2("X"), {Type: tea.KeyEnter}}},
		{"edit an entry", "db", []tea.KeyMsg{key2("e"), key2("X"), {Type: tea.KeyEnter}}},
	}

	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			m, _ := walkModel(t)
			m = m.expandAll(true)
			// A shape worth preserving: one folder closed, the rest open.
			for _, tl := range m.visible() {
				if tl.node.name == "Net" {
					tl.node.expanded = false
				}
			}
			m = onRow(t, m, st.on)
			if strings.HasPrefix(st.name, "delete an entry") || strings.HasPrefix(st.name, "edit an entry") {
				m.focus, m.esel = 1, 0 // acting on an entry, from the table
			}

			shape, focus := treeShape(m), m.focus
			for _, k := range st.keys {
				m = up(m, k)
			}
			if m.edit != editNone {
				t.Fatalf("the editor should have closed (mode %d)", m.edit)
			}
			if m.dirtyCount() == 0 {
				t.Fatalf("nothing was staged (flash %q)", m.flash)
			}
			if got := treeShape(m); strings.Join(got, " ") != strings.Join(shape, " ") {
				t.Errorf("the tree changed shape:\n%v\nwas:\n%v", got, shape)
			}
			if m.focus != focus {
				t.Errorf("the focus moved between panes: %d → %d", focus, m.focus)
			}
		})
	}
}

// treeShape is which rows are visible and how deep, by identity — a rename
// changes a row's name and that is not a change of shape.
func treeShape(m Model) []string {
	var out []string
	for _, tl := range m.visible() {
		out = append(out, fmt.Sprintf("%d:%s/%s", tl.depth, tl.node.source, tl.node.id))
	}
	return out
}

// Two folders of the same name in one place are two folders, and marking one
// must not speak for the other. A recycle bin routinely holds a pair: the guard
// keyed on the path refused the second with "already going with Ibasa copy".
func TestMarkingOneOfTwoSameNamedFolders(t *testing.T) {
	path := t.TempDir() + "/pair.kdbx"
	vaulttest.Write(t, path, vaulttest.RecycleBin(), vaulttest.Shape(func(db *gokeepasslib.Database) []gokeepasslib.Group {
		mk := func(title string) gokeepasslib.Entry {
			e := gokeepasslib.NewEntry()
			e.Values = append(e.Values, vaulttest.Val("Title", title), vaulttest.PVal("Password", "pw"))
			e.Times = gokeepasslib.NewTimeData()
			return e
		}
		inner := gokeepasslib.NewGroup()
		inner.Name = "amq"
		inner.Entries = []gokeepasslib.Entry{mk("deep")}

		first := gokeepasslib.NewGroup()
		first.Name = "copy"
		first.Entries = []gokeepasslib.Entry{mk("in-first")}
		second := gokeepasslib.NewGroup()
		second.Name = "copy"
		second.Groups = []gokeepasslib.Group{inner}
		second.Entries = []gokeepasslib.Entry{mk("in-second")}

		holder := gokeepasslib.NewGroup()
		holder.Name = "Holder"
		holder.Groups = []gokeepasslib.Group{first, second}
		root := gokeepasslib.NewGroup()
		root.Name = "Root"
		root.Groups = []gokeepasslib.Group{holder}
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
	m = m.expandAll(true)

	var ids []string
	for _, tl := range m.visible() {
		if tl.node.name == "copy" {
			ids = append(ids, tl.node.id)
		}
	}
	if len(ids) != 2 {
		t.Fatalf("expected two folders named copy, got %d", len(ids))
	}

	mark := func(m Model, id string) Model {
		t.Helper()
		for i, tl := range m.visible() {
			if tl.node.id == id {
				m.tsel, m.focus = i, 0
			}
		}
		return up(m, key2("d"))
	}

	m = mark(m, ids[0])
	if m.dirtyCount() != 1 {
		t.Fatalf("the first should stage: %d (%q)", m.dirtyCount(), m.flash)
	}
	m = mark(m, ids[1])
	if m.dirtyCount() != 2 {
		t.Errorf("the second is a different folder and must stage too: %d staged (%q)",
			m.dirtyCount(), m.flash)
	}

	// And what is genuinely inside one of them is still refused.
	var deep string
	for _, e := range m.viewEntries {
		if e.Title == "deep" {
			deep = e.ID
		}
	}
	before := m.dirtyCount()
	m = m.revealTarget(deep, false)
	m = up(m, key2("d"))
	if m.dirtyCount() != before {
		t.Errorf("an entry inside a folder already going should not stage again (%q)", m.flash)
	}
}
