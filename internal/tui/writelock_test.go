package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/config"
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

// Three states, all of them visible, each with a word.
//
// A padlock only tells you which way round it is if you already know the
// convention, and colour is gone under NO_COLOR and in a mono terminal — so the
// word carries the meaning. And a source that can never be written says so:
// showing nothing reads equally as "read-only", "not loaded yet" and "somebody
// forgot to draw it".
func TestLockBadgeShowsAllThreeStates(t *testing.T) {
	m := lockModel(t)
	m.handles = map[string]*vault.Handle{"own": {}}

	locked := ansi.Strip(m.lockBadge("own"))
	if !strings.Contains(locked, "ro") || strings.Contains(locked, "fixed") {
		t.Errorf("a source that can be unlocked should read ro, got %q", locked)
	}

	m.writeOK = map[string]bool{"own": true}
	if unlocked := ansi.Strip(m.lockBadge("own")); !strings.Contains(unlocked, "rw") {
		t.Errorf("an unlocked source should read rw, got %q", unlocked)
	}

	// A Pleasant cache: no handle, and no unlock to offer.
	m.writeOK = map[string]bool{}
	m.handles = map[string]*vault.Handle{}
	fixed := ansi.Strip(m.lockBadge("own"))
	if !strings.Contains(fixed, "fixed") {
		t.Errorf("a permanently read-only source should say so, got %q", fixed)
	}
	if fixed == locked {
		t.Error("permanently read-only must read differently from merely locked")
	}
}

// Pressing the unlock key on a permanently read-only source explains why, rather
// than doing nothing.
func TestUnlockOnAPleasantSourceExplains(t *testing.T) {
	m := lockModel(t)
	m.srcType = map[string]config.Type{"own": config.Pleasant}
	m.handles = map[string]*vault.Handle{}

	m = up(m, key2("ctrl+w"))
	if m.confirmUnlock != "" {
		t.Error("there is nothing to confirm; it can never be unlocked")
	}
	if !strings.Contains(m.flash, "sync") {
		t.Errorf("it should say why — the cache is rebuilt by sync — got %q", m.flash)
	}
}

// The badge has to survive the cursor landing on it.
//
// SelRow pads the selected row to the full width, so a badge appended afterwards
// is pushed past the edge and clipped by the panel — which looks exactly like
// the padlock vanishing whenever you move onto a source.
func TestLockBadgeSurvivesSelection(t *testing.T) {
	m := lockModel(t)
	m.handles = map[string]*vault.Handle{"own": {}}
	m.tsel, m.focus = 0, 0 // the source root, focused

	lines := m.treeLines(60, 10)
	if len(lines) == 0 {
		t.Fatal("no tree rows")
	}
	selected := ansi.Strip(lines[0])
	if !strings.Contains(selected, "ro") {
		t.Errorf("the selected source row lost its badge: %q", selected)
	}
	if dw(selected) > 60 {
		t.Errorf("the selected row is %d cells wide, want at most 60: %q", dw(selected), selected)
	}

	// And it is still there when the pane is not focused.
	m.focus = 1
	if s := ansi.Strip(m.treeLines(60, 10)[0]); !strings.Contains(s, "ro") {
		t.Errorf("the badge disappeared when the tree lost focus: %q", s)
	}
}

// The badge must not push the row past the pane. It used to be appended after
// the truncation, which is exactly how a column ends up one cell too wide.
func TestLockBadgeFitsInThePane(t *testing.T) {
	m := lockModel(t)
	m.handles = map[string]*vault.Handle{"own": {}}

	for _, w := range []int{20, 30, 60} {
		for _, line := range m.treeLines(w, 10) {
			if got := dw(ansi.Strip(line)); got > w {
				t.Errorf("at width %d a row is %d cells wide: %q", w, got, ansi.Strip(line))
			}
		}
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

// The unlock confirmation offers buttons, with the action the user came for as
// the default — so the common answer is one keypress, and the screen says so
// rather than leaving it to be discovered.
func TestUnlockConfirmationHasADefaultButton(t *testing.T) {
	m := lockModel(t)
	m.handles = map[string]*vault.Handle{"own": {}}
	m.confirmUnlock, m.confirmSel = "own", 0

	out := ansi.Strip(m.View())
	if !strings.Contains(out, "Unlock") || !strings.Contains(out, "Cancel") {
		t.Errorf("both buttons should be on screen:\n%s", out)
	}

	// Enter alone unlocks.
	m2 := up(m, key2("enter"))
	if !m2.writeUnlocked("own") {
		t.Error("enter on the default button should unlock")
	}

	// Moving to Cancel and pressing enter does not.
	m3 := up(m, tea.KeyMsg{Type: tea.KeyRight})
	if m3.confirmSel != 1 {
		t.Fatalf("right should move to the second button, got %d", m3.confirmSel)
	}
	m3 = up(m3, key2("enter"))
	if m3.writeUnlocked("own") {
		t.Error("enter on Cancel must not unlock")
	}
	if m3.confirmUnlock != "" {
		t.Error("and it should close the confirmation")
	}
}

// A stray keystroke must not answer a confirmation. Dismissing on anything at
// all means a key aimed at the screen behind it decides the question.
func TestConfirmationIgnoresUnrelatedKeys(t *testing.T) {
	m := lockModel(t)
	m.handles = map[string]*vault.Handle{"own": {}}
	m.confirmUnlock = "own"

	m = up(m, key2("j"))
	if m.confirmUnlock != "own" {
		t.Error("an unrelated key should leave the confirmation up")
	}
	if m.writeUnlocked("own") {
		t.Error("and must certainly not have answered it")
	}
}
