package tui

import (
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/theme"
	"github.com/pottom/harmos/internal/vault/vaulttest"
)

// settingsModel is the Settings tab with a few sources to click on. The pane
// reads them off the config file rather than the model, so there has to be one.
func settingsModel(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	for _, name := range []string{"own", "work", "spare"} {
		kdbx := filepath.Join(dir, name+".kdbx")
		vaulttest.Write(t, kdbx)
		if _, err := config.WriteKdbxSource(cfgPath, name, kdbx, "", false); err != nil {
			t.Fatal(err)
		}
	}

	m := up(New(nil, nil, "", 30*time.Second), tea.WindowSizeMsg{Width: 110, Height: 34})
	m.configPath = cfgPath
	return m.switchTab(tabSettings)
}

// The hit-test and the renderers have to agree about which content row belongs
// to which cursor index. Every pane starts its rows at a different offset, and
// a second copy of that arithmetic in the mouse code would drift — which is the
// bug the tab hit-test was fixed for once already.
//
// So: move the cursor, render the pane, find the row it marked, and check the
// hit-test maps that row back to the same index.
func TestSettingsRowAtMatchesTheRender(t *testing.T) {
	m := settingsModel(t)
	m.focus = 1

	// The panes do not agree on how a selected row looks — the sources table
	// swaps in the source's own icon where the others put a ▸ — so the marked
	// row is found by what changes when the pane takes the focus, which is the
	// one thing all three do.
	panes := []struct {
		cat   int
		count int
		lines func(Model) []string
		set   func(Model, int) Model
	}{
		{catSources, 3,
			func(m Model) []string { return m.sourceLines(80, m.sources()) },
			func(m Model, i int) Model { m.setSel = i; return m }},
		{catTheme, len(theme.Names()),
			func(m Model) []string { return m.themeLines(80) },
			func(m Model, i int) Model { m.themeSel = i; return m }},
		{catPrefs, 2,
			func(m Model) []string { return m.prefsLines(80) },
			func(m Model, i int) Model { m.prefSel = i; return m }},
	}

	for _, p := range panes {
		mm := m
		mm.setCat = p.cat
		// The theme pane moves its ● between the saved theme and the previewed
		// one as the focus changes; pinning them together leaves the cursor as
		// the only thing that differs.
		mm.themeOrig = mm.themeName
		if got := mm.settingsRowCount(); got != p.count {
			t.Errorf("cat %d: settingsRowCount = %d, want %d", p.cat, got, p.count)
		}
		idle := mm
		idle.focus = 0
		unselected := p.lines(idle)

		for i := range p.count {
			marked := markedRow(unselected, p.lines(p.set(mm, i)))
			if marked < 0 {
				t.Fatalf("cat %d index %d: nothing rendered as selected", p.cat, i)
			}
			idx, ok := settingsRowAt(p.cat, marked, p.count)
			if !ok || idx != i {
				t.Errorf("cat %d: the render marks line %d for index %d, the hit-test says %d (ok=%v)",
					p.cat, marked, i, idx, ok)
			}
		}
		// And nothing outside the rows resolves.
		if _, ok := settingsRowAt(p.cat, -1, p.count); ok {
			t.Errorf("cat %d: a row above the pane resolved", p.cat)
		}
		if _, ok := settingsRowAt(p.cat, 99, p.count); ok {
			t.Errorf("cat %d: a row below the pane resolved", p.cat)
		}
	}
}

// markedRow is the line that changed when the pane took the focus — the one it
// drew as selected.
func markedRow(unselected, selected []string) int {
	for i := range selected {
		if i >= len(unselected) || ansi.Strip(unselected[i]) != ansi.Strip(selected[i]) {
			return i
		}
	}
	return -1
}

// Clicking a category switches to it and puts the focus back on the list.
func TestSettingsClickPicksACategory(t *testing.T) {
	m := settingsModel(t)
	m.focus, m.setCat = 1, catSources

	m = up(m, click(4, 2+catPrefs))
	if m.setCat != catPrefs {
		t.Errorf("clicking a category should select it, got %d", m.setCat)
	}
	if m.focus != 0 {
		t.Error("and put the focus on the list it was clicked in")
	}

	// Outside the list, nothing moves.
	before := m.setCat
	m = up(m, click(4, 2+len(settingsCats)+3))
	if m.setCat != before {
		t.Error("a click below the categories selected one")
	}
}

// Clicking a row in the right pane selects it and takes the focus.
func TestSettingsClickPicksARow(t *testing.T) {
	m := settingsModel(t)
	m.setCat, m.focus, m.setSel = catSources, 0, 0

	m = up(m, click(60, 3)) // the source table's first row, under its heading
	if m.focus != 1 {
		t.Error("clicking the right pane should focus it")
	}
	if m.setSel != 0 {
		t.Errorf("setSel = %d, want 0", m.setSel)
	}
	m = up(m, click(60, 5))
	if m.setSel != 2 {
		t.Errorf("setSel = %d, want 2", m.setSel)
	}

	// The heading is not a row.
	m.setSel = 1
	m = up(m, click(60, 2))
	if m.setSel != 1 {
		t.Errorf("a click on the column headings moved the cursor to %d", m.setSel)
	}
}

// The theme pane previews as the cursor moves, so a click has to preview too —
// otherwise it would select a theme the screen does not show.
func TestSettingsClickPreviewsATheme(t *testing.T) {
	m := settingsModel(t)
	m.setCat, m.focus = catTheme, 1
	m.themeOrig = m.themeName

	names := theme.Names()
	if len(names) < 3 {
		t.Skip("not enough built-in themes to tell a click from a default")
	}
	m = up(m, click(60, 2+2))
	if m.themeSel != 2 {
		t.Fatalf("themeSel = %d, want 2", m.themeSel)
	}
	if m.themeName != names[2] {
		t.Errorf("the click selected %q but the screen shows %q", names[2], m.themeName)
	}
}

// A form or a prompt owns the whole screen; the panes behind it are not live.
func TestSettingsClicksIgnoredUnderAModal(t *testing.T) {
	for _, mode := range []int{setForm, setPrompt, setRemove} {
		m := settingsModel(t)
		m.setCat, m.setSel, m.setMode = catSources, 1, mode
		m = up(m, click(60, 3))
		if m.setSel != 1 {
			t.Errorf("mode %d: a click reached the list behind (setSel=%d)", mode, m.setSel)
		}
	}
}
