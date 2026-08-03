package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/theme"
	"github.com/pottom/harmos/internal/vault"
)

var escSeq = regexp.MustCompile("\x1b\\[[0-9;]*m")

// Highlighting must not corrupt what it highlights.
//
// Lowercasing can change a string's byte length — "İ" is two bytes and becomes a
// one-byte "i", "K" (U+212A) is three — so an offset found in the lowered string
// is not an offset in the original. Slicing with one lands inside a rune and
// puts a style code between the halves of a character, which prints garbage.
//
// The test needs a colour profile: without a TTY lipgloss renders no codes at
// all, so there is nothing to land in the wrong place and the bug hides.
func TestHighlightNeverSplitsARune(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)

	cases := []struct{ s, q string }{
		{"İstanbul", "i"}, // the one that was broken
		{"KELVIN K", "k"}, // U+212A KELVIN SIGN, three bytes to one
		{"straße", "ss"},  // one rune, two-character query
		{"STRASSE", "ss"},
		{"Ärger", "a"},
		{"Ärger", "ä"},
		{"münchen-db", "MÜNCHEN"},
	}
	for _, c := range cases {
		for _, out := range []string{
			highlight(c.s, c.q, theme.Strong),
			highlightTerms(c.s, []string{strings.ToLower(c.q)}, theme.Strong),
		} {
			for _, run := range escSeq.Split(out, -1) {
				if !utf8.ValidString(run) {
					t.Errorf("%q / %q: a style code landed inside a rune: %q", c.s, c.q, out)
					break
				}
			}
			if plain := escSeq.ReplaceAllString(out, ""); plain != c.s {
				t.Errorf("%q / %q: the text became %q", c.s, c.q, plain)
			}
		}
	}
}

// The same arithmetic decides where a search snippet starts.
func TestSnippetSlicesOnRuneBoundaries(t *testing.T) {
	for _, c := range []struct{ val, term string }{
		{"İstanbul office — the database credentials for the branch", "database"},
		{"KELVIN Kmeasurements and the api key beyond", "api"},
	} {
		got := snippet(c.val, []string{c.term}, 30)
		if !utf8.ValidString(ansi.Strip(got)) {
			t.Errorf("snippet(%q, %q) is not valid UTF-8: %q", c.val, c.term, got)
		}
	}
}

// However deep a folder sits, its name stays readable. Past nine levels in a
// narrow pane the indent used to take the whole row, leaving a lone ellipsis:
// a row that can be counted and scrolled past but not identified or acted on.
func TestADeepFolderStillHasAName(t *testing.T) {
	path := ""
	var ents []vault.Entry
	for i := range 12 {
		if path != "" {
			path += "/"
		}
		path += "level" + string(rune('A'+i))
		ents = append(ents, vault.Entry{
			Source: "s", Path: path, Title: "e" + string(rune('A'+i)), Password: secret.New("p"),
		})
	}
	m := up(New(ents, nil, "", 30*time.Second), tea.WindowSizeMsg{Width: 60, Height: 30})
	m = m.expandAll(true)

	rows := m.treeLines(m.leftPaneW()-2, 20)
	var named int
	for _, r := range rows {
		plain := ansi.Strip(r)
		if strings.TrimSpace(plain) == "" {
			continue
		}
		if !strings.Contains(plain, "level") && !strings.Contains(plain, "s ") {
			t.Errorf("a tree row with no readable name: %q", plain)
			continue
		}
		named++
	}
	if named < 13 {
		t.Errorf("only %d of 13 rows carry a name", named)
	}
}

// The selection is a background, not a recolouring. A row that turned accent
// under the cursor read as a different kind of row among its neighbours; the
// state colour of something staged still wins, because that is about the row
// rather than about where the cursor happens to be.
func TestSelectionKeepsTheRowsColour(t *testing.T) {
	if got, want := theme.SelRow.GetForeground(), theme.Strong.GetForeground(); got != want {
		t.Errorf("a selected row renders %v, ordinary text %v — they should match", got, want)
	}
	if theme.SelRow.GetBackground() == theme.Strong.GetBackground() {
		t.Error("and the selection needs a background of its own to be visible at all")
	}
	del, _ := changeStyle(edit.Deleted)
	if got := selRowStyle(theme.SelRow, edit.Deleted).GetForeground(); got != del.GetForeground() {
		t.Error("a row staged for deletion keeps its state colour under the cursor")
	}
}

// The cursor says which row is current; it does not repaint what the row is.
//
// A selected row used to be flattened into one string and rendered in a single
// colour, so the folder icon, the entry count and the staged marker all took the
// selection's. Each keeps its own now, over the selection's background.
func TestSelectedRowKeepsItsOwnColours(t *testing.T) {
	t.Setenv("HARMOS_NERDFONT", "0")
	lipgloss.SetColorProfile(termenv.TrueColor)

	m, _ := walkModel(t)
	m = m.expandAll(true)
	m = up(onRow(t, m, "db"), key2("d")) // a marker to carry
	m = onRow(t, m, "Infra")             // whose parent is the row under the cursor

	rows := m.treeLines(m.leftPaneW()-2, 10)
	if len(rows) < 2 {
		t.Fatalf("expected a few rows, got %d", len(rows))
	}
	selected := rows[m.tsel]

	if got := len(foregrounds(selected)); got < 2 {
		t.Errorf("the selected row renders %d colour(s) — the icon and the marker "+
			"lost theirs to the selection:\n%q", got, selected)
	}
	// And it is a background that marks it, across the whole width.
	if !strings.Contains(selected, "48;2;") {
		t.Errorf("the selected row has no background:\n%q", selected)
	}
}

// foregrounds is the set of distinct foreground colours in a rendered line.
func foregrounds(s string) map[string]bool {
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`38;2;[0-9;]+`).FindAllString(s, -1) {
		out[m] = true
	}
	return out
}
