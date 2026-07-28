package tui

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	gokeepasslib "github.com/tobischo/gokeepasslib/v3"

	"github.com/pottom/harmos/internal/config"
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
	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlY})
	if m.copiedWhat != "password" {
		t.Errorf("ctrl+y should copy the selected result's password, copied %q", m.copiedWhat)
	}
	if !m.searchMode {
		t.Error("and leave the search box where it was")
	}
}
