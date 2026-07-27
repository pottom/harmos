package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func genTab() Model {
	m := New(nil, "", 30*time.Second)
	m = up(m, tea.WindowSizeMsg{Width: 100, Height: 22})
	return up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}}) // Generate tab
}

func space(m Model) Model {
	return up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
}

// gotoRow moves the option cursor to the given row.
func gotoRow(m Model, row int) Model {
	for m.genRow < row {
		m = up(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	for m.genRow > row {
		m = up(m, tea.KeyMsg{Type: tea.KeyUp})
	}
	return m
}

func TestGeneratePopulatesOnEntry(t *testing.T) {
	m := genTab()
	if m.tab != 2 {
		t.Fatalf("2 should open the Generate tab, got %d", m.tab)
	}
	if len(m.genList) != 50 {
		t.Fatalf("entering the tab should generate the default 50, got %d", len(m.genList))
	}
	for _, p := range m.genList {
		if len([]rune(p)) != 20 {
			t.Fatalf("default length should be 20, got %q (%d)", p, len([]rune(p)))
		}
	}
	if !strings.Contains(m.View(), "Password") {
		t.Error("the password pane should render")
	}
}

func TestGenerateToggleShrinksPool(t *testing.T) {
	m := genTab()
	before := m.genOpts.PoolSize()
	m = gotoRow(m, genSymbol)
	m = space(m)
	if m.genOpts.Symbol {
		t.Error("space should toggle symbols off")
	}
	if m.genOpts.PoolSize() >= before {
		t.Errorf("disabling a class should shrink the pool: before %d, after %d", before, m.genOpts.PoolSize())
	}
}

func TestGenerateAdjustLength(t *testing.T) {
	m := genTab()
	m = gotoRow(m, genLength)
	m = up(m, tea.KeyMsg{Type: tea.KeyRight}) // +1
	if m.genOpts.Length != 21 {
		t.Fatalf("→ should raise length to 21, got %d", m.genOpts.Length)
	}
	if len([]rune(m.genList[0])) != 21 {
		t.Errorf("the batch should regenerate at the new length, got %d", len([]rune(m.genList[0])))
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyLeft}) // −1 back to 20
	if m.genOpts.Length != 20 {
		t.Errorf("← should lower length to 20, got %d", m.genOpts.Length)
	}
}

func TestGenerateNoClassesErrors(t *testing.T) {
	m := genTab()
	for _, r := range []int{genLower, genUpper, genDigit, genSymbol} {
		m = gotoRow(m, r)
		m = space(m)
	}
	if m.genErr == "" {
		t.Error("disabling every class should surface an error")
	}
	if len(m.genList) != 0 {
		t.Errorf("no classes should clear the list, got %d", len(m.genList))
	}
	if !strings.Contains(ansi.Strip(m.View()), m.genErr) {
		t.Error("the error should show in the pane")
	}
}

func TestGenerateListCopyKeepsState(t *testing.T) {
	m := genTab()
	m = up(m, tea.KeyMsg{Type: tea.KeyTab}) // into the list
	if m.focus != 1 {
		t.Fatal("tab should move focus to the list")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyDown}) // move selection
	if m.genSel != 1 {
		t.Errorf("↓ should move the list cursor, got %d", m.genSel)
	}
	// enter copies; it must not panic or lose the selection (clipboard may be
	// unavailable in CI, so we don't assert the countdown started).
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.genSel != 1 {
		t.Errorf("copying should keep the selection, got %d", m.genSel)
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyEsc}) // back to options
	if m.focus != 0 {
		t.Error("esc should return to the options pane")
	}
}

// Clicking an alternative promotes it to the hero; colouring and grouping never
// change the underlying text.
func TestGenerateMouseAndColor(t *testing.T) {
	m := genTab()
	// click the first "more" alternative (content row genHeroRows → y = 2+genHeroRows)
	m = up(m, click(genLeftW+5, 2+genHeroRows))
	if m.focus != 1 || m.genSel != 1 {
		t.Errorf("clicking an alternative should promote it, got focus=%d sel=%d", m.focus, m.genSel)
	}
	if got := ansi.Strip(colorizePw("aB3!xZ")); got != "aB3!xZ" {
		t.Errorf("colourising must not change the text, got %q", got)
	}
	if got := ansi.Strip(heroPassword("abcdefgh")); got != "abcd efgh" {
		t.Errorf("grouping should chunk by 4 without altering chars, got %q", got)
	}
}

func TestStrengthLabel(t *testing.T) {
	for _, tc := range []struct {
		bits float64
		want string
	}{{40, "weak"}, {75, "fair"}, {100, "strong"}, {130, "very strong"}} {
		if got, _ := strengthLabel(tc.bits); got != tc.want {
			t.Errorf("%.0f bits → %q, want %q", tc.bits, got, tc.want)
		}
	}
}
