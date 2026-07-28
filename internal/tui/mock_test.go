package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault"
)

// The kitchen sink: every kind of staged change, in one session, across two
// sources — an edit touching every field kind, a created entry and folder, a
// move, a rename, a binned delete, a permanent delete, and a folder deletion
// with a subtree under it.
//
// It exists to be looked at (`go test -run TestMockChangesReview -v` prints it)
// and to be asserted against. Every one of the checks below is something that
// was wrong when this mock was first rendered.
func mockChanges(t *testing.T, w, h int) Model {
	t.Helper()
	ent := func(id, src, path, title, user string) vault.Entry {
		return vault.Entry{ID: id, Source: src, Path: path, Title: title, Username: user, Password: secret.New("p")}
	}
	fld := func(id, src, path, name string) vault.Folder {
		return vault.Folder{ID: id, Source: src, Path: path, Name: name}
	}

	ents := []vault.Entry{
		ent("own:1", "own", "Infra/db", "db-prod", "svc_admin"),
		ent("own:2", "own", "Infra/db", "db-stage", "svc"),
		ent("own:3", "own", "Infra", "jump-host", "root"),
		ent("own:4", "own", "Archive", "old-thing", "nobody"),
		ent("own:5", "own", "Archive/2019", "ancient", "nobody"),
		ent("own:6", "own", "Archive/2019/q1", "very-old", "nobody"),
		ent("work:1", "work", "Net", "router", "admin"),
		ent("work:2", "work", "Net", "switch", "admin"),
	}
	for i := range 14 {
		ents = append(ents, ent(fmt.Sprintf("own:a%d", i), "own", "Archive", fmt.Sprintf("archived-%02d", i), "x"))
	}
	folders := []vault.Folder{
		fld("own:g:1", "own", "Infra", "Infra"),
		fld("own:g:2", "own", "Infra/db", "db"),
		fld("own:g:3", "own", "Archive", "Archive"),
		fld("own:g:4", "own", "Archive/2019", "2019"),
		fld("own:g:5", "own", "Archive/2019/q1", "q1"),
		fld("own:g:6", "own", "Infra/new", "new"),
		fld("work:g:1", "work", "Net", "Net"),
	}

	m := up(New(ents, folders, "", 30*time.Second), tea.WindowSizeMsg{Width: w, Height: h})

	notesBefore := "primary box\nrotate quarterly\ncontact: alice@example\nticket INF-4412\nkeep-1\nkeep-2\nkeep-3\nkeep-4\nkeep-5\ntail line"
	notesAfter := "primary box\nrotate quarterly\ncontact: bob@example\nticket INF-4412\nkeep-1\nkeep-2\nkeep-3\nkeep-4\nkeep-5\ntail line\nexpires end of Q3"
	before := edit.Draft{ID: "own:1", GroupID: "own:g:2", Title: "db-prod", Username: "svc_admin",
		URL: "https://db.example.internal", Password: secret.New("old-pw"), Notes: notesBefore,
		Tags: "infra;prod", TOTP: "otpauth://totp/db?secret=AAAA",
		Fields: []edit.DraftField{{Key: "pps.cuf.Environment", Value: "prod"}, {Key: "Recovery", Value: "r1", Protected: true}}}
	after := before
	after.Username = "svc-admin"
	after.Password = secret.New("a-much-longer-new-password")
	after.Notes = notesAfter
	after.Tags = "infra;prod;pci"
	after.URL = "https://db.example.internal:5432"
	after.Fields = []edit.DraftField{{Key: "pps.cuf.Environment", Value: "production"}, {Key: "Owner", Value: "platform"}}

	// Staged by hand, so the unlock the UI would have done has to be too.
	m.writeOK = map[string]bool{"own": true, "work": true}
	add := func(op edit.Op) { m.chg, _ = m.chg.Add(op) }
	add(edit.Op{Kind: edit.EditEntry, Source: "own", Target: "own:1", Before: &before, After: &after})
	add(edit.Op{Kind: edit.CreateEntry, Source: "own", Target: "own:new1", Parent: "own:g:2",
		After: &edit.Draft{ID: "own:new1", GroupID: "own:g:2", Title: "db-replica", Username: "svc",
			Password: secret.New("x"), URL: "https://replica"}})
	add(edit.Op{Kind: edit.CreateGroup, Source: "own", Target: "own:g:6", Parent: "own:g:1", Name: "new"})
	add(edit.Op{Kind: edit.DeleteEntry, Source: "own", Target: "own:2", Name: "db-stage",
		Before: &edit.Draft{ID: "own:2", GroupID: "own:g:2", Title: "db-stage"}})
	add(edit.Op{Kind: edit.MoveEntry, Source: "own", Target: "own:3", Parent: "own:g:2"})
	add(edit.Op{Kind: edit.DeleteGroup, Source: "own", Target: "own:g:3", Name: "Archive", Perm: true})
	add(edit.Op{Kind: edit.RenameGroup, Source: "work", Target: "work:g:1", Name: "Network"})
	add(edit.Op{Kind: edit.DeleteEntry, Source: "work", Target: "work:2", Name: "switch", Perm: true,
		Before: &edit.Draft{ID: "work:2", GroupID: "work:g:1", Title: "switch"}})
	return m.switchTab(tabChanges)
}

func TestMockChangesReview(t *testing.T) {
	t.Setenv("HARMOS_NERDFONT", "0")
	m := mockChanges(t, 100, 40)
	out := ansi.Strip(m.View())
	t.Log("\n" + m.View()) // -v prints the whole screen for a human to look at

	// A move used to render as "(untitled)  moved": the operation carried no
	// name, and "moved" without a destination is the half the reader knew.
	if strings.Contains(out, "(untitled)") {
		t.Errorf("every change should name what it is about:\n%s", out)
	}
	if !strings.Contains(out, "jump-host") || !strings.Contains(out, "moved to Infra") {
		t.Errorf("a move should say what moved and where to:\n%s", out)
	}

	// The tally distinguishes its states. Modified and Moved both printed "~".
	tally := ansi.Strip(m.impactTally())
	for _, want := range []string{"+", "~", "→", "-"} {
		if !strings.Contains(tally, want) {
			t.Errorf("the tally should carry %q: %q", want, tally)
		}
	}

	// Counts are in folders and entries, never in "things".
	m2, _ := mockChanges(t, 100, 40).askToSave()
	confirm := ansi.Strip(m2.View())
	for _, want := range []string{"folder and 1 entry added", "1 entry moved", "1 folder changed"} {
		if !strings.Contains(confirm, want) {
			t.Errorf("the confirmation should say %q:\n%s", want, confirm)
		}
	}
	// All of a source's removals being permanent reads as "permanently", not
	// "1 of them permanently".
	if !strings.Contains(confirm, "removed, permanently") {
		t.Errorf("grammar: when everything goes permanently, say so plainly:\n%s", confirm)
	}
	// The focused button is the bracketed one — brackets read as "this one".
	if !strings.Contains(confirm, "[ Cancel ]") || strings.Contains(confirm, "[ Write ]") {
		t.Errorf("a set with a permanent deletion leads with Cancel, and it is the bracketed one:\n%s", confirm)
	}
}
