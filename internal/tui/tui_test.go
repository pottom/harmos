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
		{Source: "personal", Path: "Net", Title: "router", Username: "admin", Password: secret.New("p3")},
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

// Renders at every breakpoint, including a resize sequence, and refuses when tiny
// (spec §8a).
func TestRendersAcrossBreakpoints(t *testing.T) {
	m := typeStr(testModel(), "db") // non-empty so the list renders
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
	// selection survived the resize sequence
	if m.sel != 0 {
		t.Errorf("selection = %d after resizes", m.sel)
	}
}

func TestTypingFiltersAndRanks(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 100, Height: 30})
	m = typeStr(m, "db-prod")
	if len(m.results) == 0 {
		t.Fatal("no results for db-prod")
	}
	if got := m.results[0].Entry.Title; got != "db-prod" {
		t.Errorf("top result = %q, want db-prod", got)
	}
	// esc clears back to the console home
	m = up(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.input.Value() != "" {
		t.Error("esc should clear the query")
	}
	if !strings.Contains(m.View(), "your sources") {
		t.Error("empty query should show the console home")
	}
}

func TestPeekIsCommandMode(t *testing.T) {
	m := up(testModel(), tea.WindowSizeMsg{Width: 120, Height: 30})
	m = typeStr(m, "db")
	m = up(m, tea.KeyMsg{Type: tea.KeyTab})
	if !m.peek {
		t.Fatal("tab should enter peek")
	}
	before := m.input.Value()
	m = up(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if m.input.Value() != before {
		t.Error("letters must be commands (not typed) while peeking")
	}
	// the split shows the detail on the right
	if !strings.Contains(m.View(), "Username") {
		t.Error("peek should show the detail")
	}
}
