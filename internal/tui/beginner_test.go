package tui

import (
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/edit"
)

// What the interface tells somebody who has not been told anything.
//
// From the first-run audit: D was advertised nowhere and the help truncated its
// explanation at every width, the editor called staging "save" while every
// toast used "save" for the write, locking a source with work staged said
// nothing about what that meant, and the marks that colour a staged session
// were defined in no place at all.

// D is a permanent delete. It has to be offered, and its difference from d has
// to survive the pane it is explained in.
func TestPermanentDeleteIsDiscoverable(t *testing.T) {
	t.Setenv("HARMOS_NERDFONT", "0")
	m, _ := walkModel(t)
	m = onEntry(t, m.expandAll(true), "db", "db-prod")

	if out := ansi.Strip(m.View()); !strings.Contains(out, "D") {
		t.Errorf("the footer of an unlocked source should offer D:\n%s", out)
	}

	// And the help says what it does, at every width — the pane used to cap at
	// 48 columns whatever the terminal was, cutting "(again undoes)" off.
	for _, w := range []int{100, 140, 200} {
		mm := up(m, tea.WindowSizeMsg{Width: w, Height: 40})
		keys := strings.Join(mm.keyList(helpLeftW(w)-2), "\n")
		if !strings.Contains(keys, "permanently") {
			t.Errorf("at %d columns the help does not say what D does:\n%s", w, keys)
		}
		if w >= 140 && !strings.Contains(keys, "undoes") {
			t.Errorf("at %d columns there is room for the whole line and it is still cut:\n%s", w, keys)
		}
	}
}

// One word, one meaning. The editor stages; ^s writes.
func TestStagingIsNotCalledSaving(t *testing.T) {
	m := up(intoTable(t, editModel(t)), key2("e"))
	hint := ansi.Strip(m.editForm.Hint())
	if strings.Contains(hint, "save") {
		t.Errorf("the editor does not save, it stages: %q", hint)
	}
	if !strings.Contains(hint, "stage") {
		t.Errorf("and it should say so: %q", hint)
	}
}

// Locking a source with work staged takes the write key away with it.
func TestRelockingSaysWhatItCosts(t *testing.T) {
	m := stageAnEdit(t, editModel(t))
	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlW})

	if m.writeOK["own"] {
		t.Fatal("^w should lock it")
	}
	for _, want := range []string{"staged change", "^w"} {
		if !strings.Contains(m.flash, want) {
			t.Errorf("locking with work staged should mention %q: %q", want, m.flash)
		}
	}
}

// The marks are the visual language of a staged session, and they were defined
// nowhere. A reader who deleted one entry saw its folder and its folder's
// folder take the same mark, and read it as "these are going too".
func TestTheStagedMarksHaveALegend(t *testing.T) {
	t.Setenv("HARMOS_NERDFONT", "0")
	m, _ := walkModel(t)
	keys := strings.Join(m.keyList(60), "\n")

	for _, st := range []edit.State{edit.New, edit.Modified, edit.Moved, edit.Deleted, edit.Purged} {
		if !strings.Contains(keys, markerFor(st)) {
			t.Errorf("the legend does not show the %v mark", st)
		}
	}
	for _, want := range []string{"recycle bin", "gone for good", "something inside it"} {
		if !strings.Contains(keys, want) {
			t.Errorf("the legend should say %q:\n%s", want, keys)
		}
	}
}

// The tab strip names the key that reaches each tab.
func TestTheTabsSayHowToReachThem(t *testing.T) {
	m, _ := walkModel(t)
	strip := ansi.Strip(m.tabIndicator())
	for _, want := range []string{"1 Vault", "2 Changes", "3 Generate", "4 Settings"} {
		if !strings.Contains(strip, want) {
			t.Errorf("the strip should read %q: %q", want, strip)
		}
	}
}

// The help's frame sits where every tab's does, so ? does not move the panel.
func TestTheHelpFrameDoesNotJump(t *testing.T) {
	t.Setenv("HARMOS_NERDFONT", "0")
	for _, size := range [][2]int{{97, 29}, {60, 20}, {41, 11}} {
		m, _ := walkModel(t)
		m = up(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})

		vault := frameBottom(t, m.View())
		help := frameBottom(t, up(m, key2("?")).View())
		if vault != help {
			t.Errorf("%dx%d: the vault's frame ends at row %d and the help's at %d",
				size[0], size[1], vault, help)
		}
	}
}

// A copy that did nothing said nothing, so it was indistinguishable from one
// that worked — and the reader pasted whatever the clipboard held before.
func TestACopyThatFoundNothingSaysSo(t *testing.T) {
	ents := []vault.Entry{{ID: "s:1", GroupID: "s:g:1", Source: "s", Path: "f", Title: "bare",
		Password: secret.New("p")}} // no username, no url
	m := up(New(ents, []vault.Folder{{ID: "s:g:1", Source: "s", Path: "f", Name: "f"}}, "", 30*time.Second),
		tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m.expandAll(true)
	m.tsel = firstFolderWithEntries(m.roots)
	m = up(m, tea.KeyMsg{Type: tea.KeyTab})

	for _, k := range []tea.KeyMsg{{Type: tea.KeyCtrlU}, {Type: tea.KeyCtrlO}} {
		mm := up(m, k)
		if mm.remaining != 0 {
			t.Errorf("%v started a countdown for a field that is not there", k)
		}
		if mm.flash == "" {
			t.Errorf("%v said nothing at all", k)
		}
	}
}

// ctrl+t is offered only for a TOTP that can be read. The footer promised it for
// any non-empty otp field while the detail drew no row and the key did nothing,
// so a malformed seed looked like harmos having lost the TOTP.
func TestTOTPIsOnlyOfferedWhenItCanBeRead(t *testing.T) {
	mk := func(otp string) Model {
		ents := []vault.Entry{{ID: "s:1", GroupID: "s:g:1", Source: "s", Path: "f",
			Title: "e", Password: secret.New("p"), TOTP: otp}}
		m := up(New(ents, []vault.Folder{{ID: "s:g:1", Source: "s", Path: "f", Name: "f"}}, "", 30*time.Second),
			tea.WindowSizeMsg{Width: 100, Height: 30})
		m = m.expandAll(true)
		m.tsel = firstFolderWithEntries(m.roots)
		m = up(m, tea.KeyMsg{Type: tea.KeyTab})
		return up(m, tea.KeyMsg{Type: tea.KeyRight})
	}

	good := ansi.Strip(mk("otpauth://totp/x?secret=JBSWY3DPEHPK3PXP").View())
	if !strings.Contains(good, "ctrl+t") {
		t.Errorf("a readable TOTP should offer the key:\n%s", good)
	}
	bad := ansi.Strip(mk("this is not an otpauth uri").View())
	if strings.Contains(bad, "ctrl+t") {
		t.Errorf("an unreadable one must not:\n%s", bad)
	}
}
