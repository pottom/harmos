package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault"
)

func testModel() Model {
	return New([]vault.Entry{
		{Source: "work", Path: "Infra", Title: "db-prod", Username: "svc_admin", Password: secret.New("p1")},
		{Source: "work", Path: "Infra", Title: "db-staging", Username: "svc", Password: secret.New("p2")},
		{Source: "personal", Path: "Net", Title: "router", Username: "admin", Password: secret.New("p3"), URL: "https://10.0.0.1"},
	}, 30*time.Second)
}

func up(m Model, msg tea.Msg) Model {
	nm, _ := m.Update(msg)
	return nm.(Model)
}

func typeStr(m Model, s string) Model {
	for _, r := range s {
		m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// The tree is the base surface: it renders on an empty query with no typing.
func TestTreeIsTheBase(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})
	out := m.View()
	// both source roots show as folders in the left pane
	for _, want := range []string{"personal", "work", "sources"} {
		if !strings.Contains(out, want) {
			t.Errorf("browse view missing %q", want)
		}
	}
	if m.searching() {
		t.Error("fresh model must not be searching")
	}
}

// Renders at every breakpoint, including a resize sequence, and refuses when tiny.
func TestRendersAcrossBreakpoints(t *testing.T) {
	m := testModel()
	sizes := [][2]int{{200, 50}, {100, 30}, {80, 24}, {60, 20}, {40, 12}, {30, 8}, {100, 30}}
	for _, sz := range sizes {
		m = up(m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		out := m.View()
		if out == "" {
			t.Fatalf("%dx%d rendered empty", sz[0], sz[1])
		}
		if (sz[0] < 40 || sz[1] < 10) && !strings.Contains(out, "too small") {
			t.Errorf("%dx%d should refuse to render", sz[0], sz[1])
		}
	}
}

// In tree browse, letters are free for future hotkeys — they must not type into
// the search box. Only "/" opens search.
func TestLettersAreFreeUntilSlash(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = typeStr(m, "abc")
	if m.searchMode || m.input.Value() != "" {
		t.Fatalf("letters must not start a search: mode=%v value=%q", m.searchMode, m.input.Value())
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !m.searchMode {
		t.Fatal("/ should open the search box")
	}
}

// / opens search, typing filters, enter leaves the box keeping the filter, and
// esc clears the filter back to the tree.
func TestSlashSearchFlow(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeStr(m, "db-prod")
	if !m.searchMode {
		t.Fatal("should be in search mode while typing")
	}
	if len(m.results) == 0 || m.results[0].Entry.Title != "db-prod" {
		t.Fatalf("top result wrong: %+v", m.results)
	}
	if !strings.Contains(m.View(), "Search results") {
		t.Error("search should show the results pane")
	}
	// enter leaves the box but keeps the filter/results
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.searchMode {
		t.Error("enter should leave the search box")
	}
	if !m.showResults() || m.input.Value() != "db-prod" {
		t.Error("enter must keep the filter and results")
	}
	// esc clears the filter back to the tree
	m = up(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.showResults() || m.input.Value() != "" {
		t.Error("esc should clear the filter back to the tree")
	}
}

// Navigating the tree into a folder's table and opening an entry's details.
func TestBrowseIntoDetails(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})
	// visible tree: personal, Net, work, Infra (roots expanded, leaves collapsed)
	// walk down to a folder that holds entries, then into its table
	var folder *node
	for i := 0; i < len(m.visible()); i++ {
		if f := m.currentFolder(); f != nil && len(f.entries) > 0 {
			folder = f
			break
		}
		m = up(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if folder == nil {
		t.Fatal("no folder with entries found in the tree")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyTab}) // into the entry table
	if m.focus != 1 {
		t.Fatalf("tab should move focus to the table, focus=%d", m.focus)
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyEnter}) // open details
	if !m.detail {
		t.Fatal("enter on an entry should open the details screen")
	}
	if e := m.selEntry(); e == nil {
		t.Fatal("details screen has no selected entry")
	}
	if !strings.Contains(m.View(), "Username") {
		t.Error("details should show the Username field")
	}
	// reveal toggles, esc leaves
	m = up(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if !m.reveal {
		t.Error("ctrl+r should reveal the password")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.detail {
		t.Error("esc should leave the details screen")
	}
}

// q quits from browse but is an ordinary character while typing a search.
func TestQuitAndSearchQ(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})

	// q in browse returns a (quit) command
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}); cmd == nil {
		t.Error("q should quit from browse mode")
	}

	// q while searching is typed, not a quit
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if m.input.Value() != "q" {
		t.Errorf("q should be typed in search mode, got %q", m.input.Value())
	}
}

// Help overlay toggles and any key closes it.
func TestHelpToggles(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !m.help || !strings.Contains(m.View(), "keys") {
		t.Fatal("? should open help")
	}
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if m.help {
		t.Error("any key should close help")
	}
}
