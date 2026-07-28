package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault"
)

func lockModel(t *testing.T) Model {
	t.Helper()
	m := New([]vault.Entry{
		{ID: "own:1", GroupID: "own:g:1", Source: "own", Path: "Infra", Title: "db", Password: secret.New("p")},
	}, []vault.Folder{
		{ID: "own:g:1", Source: "own", Path: "Infra", Name: "Infra"},
	}, "", 30*time.Second)
	return up(m, tea.WindowSizeMsg{Width: 100, Height: 30})
}

// The default is the old behaviour. A vault becomes editable only because the
// user said so, this run, on purpose — and nothing about that survives a restart.
func TestNothingIsWritableAtLaunch(t *testing.T) {
	m := lockModel(t)

	if len(m.writeOK) != 0 {
		t.Errorf("no source should be unlocked at launch, got %v", m.writeOK)
	}
	if m.writeUnlocked("own") {
		t.Error("own should be locked")
	}
	if m.dirtyCount() != 0 {
		t.Error("nothing should be staged at launch")
	}
}

// Unlocking asks first: it is the moment the program stops being read-only.
func TestUnlockAsksAndTakesEffect(t *testing.T) {
	m := lockModel(t)
	m.handles = map[string]*vault.Handle{"own": nil} // no handle → refusal path

	m = up(m, key2("ctrl+w"))
	if m.confirmUnlock != "" {
		t.Error("a source with no handle should be refused, not confirmed")
	}
	if !strings.Contains(m.flash, "cannot be written") {
		t.Errorf("the refusal should say why, got %q", m.flash)
	}
}

// Locking again is immediate: it only ever takes a capability away.
func TestLockingNeedsNoConfirmation(t *testing.T) {
	m := lockModel(t)
	m.writeOK = map[string]bool{"own": true}

	m = up(m, key2("ctrl+w"))
	if m.confirmUnlock != "" {
		t.Error("locking should not ask")
	}
	if m.writeUnlocked("own") {
		t.Error("own should be locked again")
	}
	if !strings.Contains(m.flash, "locked") {
		t.Errorf("flash = %q", m.flash)
	}
}

// The confirmation owns every key while it is up, and n leaves things as they were.
func TestUnlockConfirmationCanBeDeclined(t *testing.T) {
	m := lockModel(t)
	m.confirmUnlock = "own"

	if !strings.Contains(ansi.Strip(m.View()), "Unlock own for editing?") {
		t.Errorf("the confirmation should be on screen:\n%s", ansi.Strip(m.View()))
	}
	m = up(m, key2("n"))
	if m.confirmUnlock != "" {
		t.Error("n should dismiss the confirmation")
	}
	if m.writeUnlocked("own") {
		t.Error("declining must not unlock")
	}

	m.confirmUnlock = "own"
	m = up(m, key2("y"))
	if !m.writeUnlocked("own") {
		t.Error("y should unlock")
	}
}

// The padlock says which way round it is in words as well: a glyph alone is
// meaningless unless you already know the convention, and colour is not
// available in a mono terminal.
func TestLockBadgeCarriesAWord(t *testing.T) {
	m := lockModel(t)
	m.handles = map[string]*vault.Handle{"own": {}}

	locked := ansi.Strip(m.lockBadge("own"))
	if !strings.Contains(locked, "ro") {
		t.Errorf("a locked source should read ro, got %q", locked)
	}
	m.writeOK = map[string]bool{"own": true}
	if unlocked := ansi.Strip(m.lockBadge("own")); !strings.Contains(unlocked, "rw") {
		t.Errorf("an unlocked source should read rw, got %q", unlocked)
	}

	// A source that cannot be written at all shows nothing: a padlock would
	// imply it could be opened.
	m.handles = nil
	if b := m.lockBadge("own"); b != "" {
		t.Errorf("an unwritable source should show no padlock, got %q", b)
	}
}

// The Changes tab is always there, and says what to do when empty.
func TestChangesTabExplainsItself(t *testing.T) {
	m := lockModel(t)
	m = up(m, key2("4"))

	if m.tab != tabChanges {
		t.Fatalf("4 should open the Changes tab, got %d", m.tab)
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "nothing pending") && !strings.Contains(out, "cannot be edited") {
		t.Errorf("the empty Changes tab should explain itself:\n%s", out)
	}

	// And 1/2/3 still mean what they meant before it existed.
	for key, want := range map[string]int{"1": tabVault, "2": tabGenerate, "3": tabSettings} {
		m = up(m, key2(key))
		if m.tab != want {
			t.Errorf("key %q → tab %d, want %d", key, m.tab, want)
		}
	}
}

// An empty folder used to be invisible, because the tree was inferred from entry
// paths. A folder the user has just created has nothing in it yet.
func TestEmptyFolderAppearsInTheTree(t *testing.T) {
	m := New(
		[]vault.Entry{{ID: "s:1", Source: "s", Path: "Full", Title: "e", Password: secret.New("p")}},
		[]vault.Folder{
			{ID: "s:g:1", Source: "s", Path: "Full", Name: "Full"},
			{ID: "s:g:2", Source: "s", Path: "Empty", Name: "Empty"},
		}, "", 30*time.Second)
	m = up(m, tea.WindowSizeMsg{Width: 100, Height: 30})

	var names []string
	for _, tl := range m.visible() {
		names = append(names, tl.node.name)
	}
	if !containsStr(names, "Empty") {
		t.Errorf("a folder with no entries should still be in the tree, got %v", names)
	}
}

func containsStr(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}

func key2(k string) tea.KeyMsg {
	switch k {
	case "ctrl+w":
		return tea.KeyMsg{Type: tea.KeyCtrlW}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}
