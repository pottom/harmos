package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/config"
)

func TestGeneratorOptsPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if _, err := config.WriteKdbxSource(path, "own", filepath.Join(dir, "own.kdbx"), "", false); err != nil {
		t.Fatal(err)
	}
	if err := config.SetGenerator(path, 28, true, true, false, false, true, false, ""); err != nil {
		t.Fatal(err)
	}
	m := New(nil, nil, path, 30*time.Second)
	if m.genOpts.Length != 28 || m.genOpts.Digit != false || m.genOpts.AvoidAmbig != true {
		t.Errorf("saved generator options were not loaded: %+v", m.genOpts)
	}
}

func genTab() Model {
	m := New(nil, nil, "", 30*time.Second)
	m = up(m, tea.WindowSizeMsg{Width: 100, Height: 22})
	return up(m, tabKey(tabGenerate))
}

func space(m Model) Model {
	return up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
}

// gotoRow moves the option cursor to the given row, switching to the options pane
// first (the tab now lands on the password pane).
func gotoRow(m Model, row int) Model {
	if m.focus != 0 {
		m = up(m, tea.KeyMsg{Type: tea.KeyEsc})
	}
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
	if len(m.genList) != 1 {
		t.Fatalf("entering the tab should roll one password, got %d", len(m.genList))
	}
	if p := m.genList[0]; len([]rune(p)) != 20 {
		t.Fatalf("default length should be 20, got %q (%d)", p, len([]rune(p)))
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

func TestGenerateExcludeTyping(t *testing.T) {
	m := genTab()
	m = gotoRow(m, genExclude)
	for _, r := range "aeiou" { // type characters to exclude
		m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.genOpts.Exclude != "aeiou" {
		t.Fatalf("typed exclude = %q, want aeiou", m.genOpts.Exclude)
	}
	for _, p := range m.genList {
		if strings.ContainsAny(p, "aeiou") {
			t.Errorf("excluded char in generated %q", p)
		}
	}
	// backspace removes the last one
	m = up(m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.genOpts.Exclude != "aeio" {
		t.Errorf("after backspace = %q, want aeio", m.genOpts.Exclude)
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

func reroll(m Model) Model {
	return up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
}

func TestGenerateRerollAndCopy(t *testing.T) {
	m := genTab()
	if m.focus != 1 {
		t.Fatal("the Generate tab should land on the password pane")
	}
	m = reroll(m)
	if len(m.genList) != 2 || m.genSel != 1 {
		t.Fatalf("reroll should grow history and make the newest the hero, got n=%d sel=%d", len(m.genList), m.genSel)
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyDown}) // down the list = older roll
	if m.genSel != 0 {
		t.Errorf("↓ should move to the older roll, got %d", m.genSel)
	}
	// enter copies; it must not panic or lose the selection (clipboard may be
	// unavailable in CI, so we don't assert the countdown started).
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.genSel != 0 {
		t.Errorf("copying should keep the selection, got %d", m.genSel)
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyEsc}) // back to options
	if m.focus != 0 {
		t.Error("esc should return to the options pane")
	}
}

// Clicking a recent entry promotes it to the hero; colouring and grouping never
// change the underlying text.
func TestGenerateMouseAndColor(t *testing.T) {
	m := genTab()
	m = reroll(m)
	m = reroll(m) // 3 in history → a recent list of 2
	heroBefore := m.genList[m.genSel]

	_, listStart, order := m.genLayout(m.genVisRows())
	if len(order) < 2 {
		t.Fatal("expected a recent list after rerolling")
	}
	// order[0] is the current hero; click the second row to pick a different one.
	m = up(m, click(m.genLeftW()+5, 2+listStart+1))
	if m.focus != 1 || m.genList[m.genSel] == heroBefore {
		t.Errorf("clicking a recent entry should promote it (hero was %q, now %q)", heroBefore, m.genList[m.genSel])
	}

	if got := ansi.Strip(colorizePw("aB3!xZ")); got != "aB3!xZ" {
		t.Errorf("colourising must not change the text, got %q", got)
	}
	if got := ansi.Strip(heroPassword("abcdefgh")); got != "abcdefgh" {
		t.Errorf("the hero must show the password verbatim, got %q", got)
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
