package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault"
)

// Vault text is data, not instructions.
//
// Nothing sanitised it on the way to the screen, so an entry could drive the
// terminal: \x1b[2J clears it on every frame, an OSC rewrites the window title,
// \x1b[?1049l leaves the alt-screen buffer, and a bare \r returns the cursor to
// column 0 mid-row so the entry overwrites the panel border to its left.
//
// A newline is quieter and just as bad. ansi.StringWidth counts it as no cells,
// so the frame grows a row and every width check in the audit still passes —
// which is why this needed a test of its own.
func TestVaultTextCannotDriveTheTerminal(t *testing.T) {
	t.Setenv("HARMOS_NERDFONT", "0")

	const clear, osc, alt = "\x1b[2J", "\x1b]0;PWNED\x07", "\x1b[?1049l"
	hostile := []string{clear, osc, alt, "\r", "\n", "\x7f", "\x00"}

	for _, bad := range hostile {
		ents := []vault.Entry{{
			ID: "s:1", GroupID: "s:g:1", Source: "s", Path: "f",
			Title: "t" + bad + "x", Username: "u" + bad, URL: "https://" + bad,
			Notes: "n" + bad + "x", Password: secret.New("p"),
			Tags:   []string{"tag" + bad},
			Custom: []vault.Field{{Name: "k" + bad, Value: "v" + bad}},
			Files:  []vault.Attachment{{Name: "f" + bad + ".txt"}},
		}}
		folders := []vault.Folder{{ID: "s:g:1", Source: "s", Path: "f" + bad, Name: "f" + bad}}

		m := up(New(ents, folders, "", 30*time.Second), tea.WindowSizeMsg{Width: 80, Height: 20})
		m = m.expandAll(true)
		m.tsel = firstFolderWithEntries(m.roots)

		// Every surface an entry's own text reaches.
		screens := map[string]Model{
			"tree and table": m,
			"entry detail":   up(up(m, tea.KeyMsg{Type: tea.KeyTab}), tea.KeyMsg{Type: tea.KeyRight}),
			"results":        searchFor(m, "t"),
			"changes":        m.switchTab(tabChanges),
		}
		for name, s := range screens {
			out := s.View()
			if strings.Contains(out, clear) || strings.Contains(out, "\x1b]") || strings.Contains(out, alt) {
				t.Errorf("%s: %q reached the terminal as a control sequence", name, bad)
			}
			if strings.Contains(out, "\r") {
				t.Errorf("%s: %q put a carriage return on screen", name, bad)
			}
			if got := len(strings.Split(out, "\n")); got != 20 {
				t.Errorf("%s: %q made the frame %d rows, want 20", name, bad, got)
			}
		}
	}
}

func searchFor(m Model, q string) Model {
	m = up(m, key2("/"))
	m = typeStr(m, q)
	return up(m, tea.KeyMsg{Type: tea.KeyEnter})
}

// g from a search result navigates by identity.
//
// It used to walk the tree splitting the path on "/" and matching children by
// name, then pick the entry by Title and Username — the technique buildTree
// documents as wrong, in the same file. Two sibling folders with one name are
// legal in KeePass and any merge makes them, so g landed in the first one, on
// its first entry, and the ↵ that follows copied a password nobody asked for
// while the screen named a different one.
func TestGoingToAResultUsesIdentity(t *testing.T) {
	ents := []vault.Entry{
		{ID: "s:1", GroupID: "s:g:1", Source: "s", Path: "Dup", Title: "first-dup", Password: secret.New("p1")},
		{ID: "s:2", GroupID: "s:g:2", Source: "s", Path: "Dup", Title: "second-dup", Password: secret.New("p2")},
	}
	folders := []vault.Folder{
		{ID: "s:g:1", Source: "s", Path: "Dup", Name: "Dup"},
		{ID: "s:g:2", Source: "s", Path: "Dup", Name: "Dup"},
	}
	m := up(New(ents, folders, "", 30*time.Second), tea.WindowSizeMsg{Width: 100, Height: 30})

	m = searchFor(m, "second-dup")
	if len(m.results) != 1 {
		t.Fatalf("expected one result, got %d", len(m.results))
	}
	m = up(m, key2("g"))

	e := m.selEntry()
	if e == nil {
		t.Fatal("g should land on the entry it was pointing at")
	}
	if e.ID != "s:2" {
		t.Errorf("landed on %q (%s) — the next ↵ would copy the wrong password", e.ID, e.Title)
	}

	// And a folder whose own name contains the path separator is reachable.
	slash := []vault.Entry{{ID: "s:9", GroupID: "s:g:9", Source: "s", Path: "a/b", Title: "inside", Password: secret.New("p")}}
	sf := []vault.Folder{{ID: "s:g:9", Source: "s", Path: "a/b", Name: "a/b"}}
	m2 := up(New(slash, sf, "", 30*time.Second), tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 = up(searchFor(m2, "inside"), key2("g"))
	if e := m2.selEntry(); e == nil || e.ID != "s:9" {
		t.Errorf("a folder named %q should still be reachable, landed on %v", "a/b", e)
	}
}

// A staged edit changes the fields it was given and forgets nothing else. The
// projection used to rebuild the row from the draft alone, so attachments and
// times vanished and every custom field was renamed to its storage key — at the
// moment the reader is deciding whether to approve the write.
func TestStagingAnEditKeepsWhatItDidNotTouch(t *testing.T) {
	m := intoTable(t, editModel(t))
	before := *m.selEntry()

	m = up(m, key2("e"))
	m.editForm = m.editForm.setValue("username", "changed")
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})

	var after *vault.Entry
	for i := range m.viewEntries {
		if m.viewEntries[i].ID == before.ID {
			after = &m.viewEntries[i]
		}
	}
	if after == nil {
		t.Fatal("the entry left the view")
	}
	if len(after.Files) != len(before.Files) {
		t.Errorf("attachments: %d before, %d after", len(before.Files), len(after.Files))
	}
	if !after.Modified.Equal(before.Modified) {
		t.Error("the times were dropped")
	}
	for i, f := range after.Custom {
		if i < len(before.Custom) && f.Name != before.Custom[i].Name {
			t.Errorf("custom field renamed to its storage key: %q, was %q", f.Name, before.Custom[i].Name)
		}
	}
}
