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
// Written after a hand audit found three things: the picker never said what was
// being moved, n cancelled it without saying so — the "no" of a confirmation it
// had stopped being, and the new-entry key everywhere else — and nothing could
// be moved to the top of a source, a place the vault has no trouble with.

func moveModel(t *testing.T) Model {
	t.Helper()
	m, _ := walkModel(t)
	return m.expandAll(true)
}

// m opens the picker from every surface a target can be selected on.
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
			if m.edit != editMove {
				t.Fatalf("m should open the picker, got mode %d (%q)", m.edit, m.flash)
			}
			if len(m.moveDests) == 0 {
				t.Error("and offer somewhere to go")
			}
		})
	}
}

// The picker says what is moving and where it is now. Pressing m on the wrong
// row is easy; noticing it from a list of destinations is not.
func TestMovePickerNamesWhatIsMoving(t *testing.T) {
	m := up(onEntry(t, moveModel(t), "db", "db-prod"), key2("m"))
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "db-prod") {
		t.Errorf("the picker should name what is moving:\n%s", out)
	}
	if !strings.Contains(out, "now in") || !strings.Contains(out, "Infra › db") {
		t.Errorf("and where it is now:\n%s", out)
	}
}

// What is on offer, and what is deliberately not.
func TestMoveDestinations(t *testing.T) {
	t.Run("an entry is not offered its own folder", func(t *testing.T) {
		m := up(onEntry(t, moveModel(t), "db", "db-prod"), key2("m"))
		if labels(m).has("db") {
			t.Errorf("offered the folder it is already in: %v", labels(m))
		}
	})
	t.Run("a folder is offered neither itself nor its parent", func(t *testing.T) {
		m := up(onRow(t, moveModel(t), "db"), key2("m"))
		for _, no := range []string{"db", "Infra"} {
			if labels(m).has(no) {
				t.Errorf("offered %q: %v", no, labels(m))
			}
		}
	})
	t.Run("the top of a source is a place", func(t *testing.T) {
		// The tree draws a source there and gives the row no identity, so the
		// walk used to skip it — and nothing could be moved to the top at all.
		m := up(onEntry(t, moveModel(t), "db", "db-prod"), key2("m"))
		var found bool
		for _, d := range m.moveDests {
			if strings.Contains(d.label, "top level") {
				found = true
			}
		}
		if !found {
			t.Errorf("the top level should be a destination: %v", labels(m))
		}
	})
	t.Run("a folder staged for deletion is not", func(t *testing.T) {
		m := up(onRow(t, moveModel(t), "Net"), key2("d"))
		m = up(onEntry(t, m, "db", "db-prod"), key2("m"))
		if labels(m).has("Net") {
			t.Errorf("offered a folder about to stop existing: %v", labels(m))
		}
	})
}

type destLabels []string

func (d destLabels) has(name string) bool {
	for _, l := range d {
		if l == name {
			return true
		}
	}
	return false
}

func labels(m Model) destLabels {
	var out destLabels
	for _, d := range m.moveDests {
		out = append(out, strings.TrimSpace(d.label))
	}
	return out
}

// esc cancels; nothing else does. n used to, silently — it is the "no" of a
// confirmation this stopped being, and the new-entry key everywhere else.
func TestMovePickerOnlyEscapeCancels(t *testing.T) {
	m := up(onEntry(t, moveModel(t), "db", "db-prod"), key2("m"))

	for _, k := range []string{"n", "N", "q", "/", "d", "1"} {
		if got := up(m, key2(k)).edit; got != editMove {
			t.Errorf("%q closed the picker (mode %d)", k, got)
		}
	}
	for _, k := range []tea.KeyMsg{{Type: tea.KeyCtrlC}, {Type: tea.KeyCtrlS}} {
		mm := up(m, k)
		if mm.saveConfirm || mm.quitGuard {
			t.Errorf("%v reached past the picker", k)
		}
	}
	if got := up(m, tea.KeyMsg{Type: tea.KeyEsc}).edit; got != editNone {
		t.Errorf("esc should close it, mode %d", got)
	}
}

// The picker opens on wherever the last move went, so a run of moves out of one
// folder costs one keystroke each after the first.
func TestMovePickerRemembersAcrossASurface(t *testing.T) {
	m := onEntry(t, moveModel(t), "db", "db-prod")
	m = up(m, key2("m"))
	m = pickDestination(t, m, "Net")
	want := m.moveDests[m.moveSel].id
	m = up(m, key2("enter"))

	m = onEntry(t, m, "db", "db-stage")
	m = up(m, key2("m"))
	if got := m.moveDests[m.moveSel].id; got != want {
		t.Errorf("the picker opened on %q, want %q", got, want)
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
	m = pickDestination(t, m, "Net")
	m = up(m, key2("enter")) // this one comes out first

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
		t.Error("a locked source should not open the picker")
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
	for i, d := range m.moveDests {
		if strings.Contains(d.label, "top level") {
			m.moveSel = i
		}
	}
	m = up(m, key2("enter"))

	out := ansi.Strip(m.switchTab(tabChanges).View())
	if strings.Contains(out, " moved ") || !strings.Contains(out, "top level") {
		t.Errorf("the review should name where it went:\n%s", out)
	}
	if !strings.Contains(out, "Infra") {
		t.Errorf("and where it came from:\n%s", out)
	}
}
