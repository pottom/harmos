package tui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/keyring"
	"github.com/pottom/harmos/internal/theme"
)

// Settings sub-modes (modal overlays on top of the two-pane list).
const (
	setList    = iota // the two-pane category selector + content
	setRemove         // the remove-source confirmation overlay
	setForm           // the add/edit form overlay
	setPrompt         // the save-password prompt overlay
	setSyncing        // a sync is running
)

// Settings categories (left pane).
const (
	catSources = iota
	catTheme
	catIcons
	catPrefs
)

var settingsCats = []string{"Sources", "Theme", "Icons", "Preferences"}

// updateSettings handles keys while the Settings tab is active: modal overlays
// first, then the two-pane list (left = category, right = its content).
func (m Model) updateSettings(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.setMode {
	case setRemove:
		return m.updateRemove(key, m.sources())
	case setForm:
		return m.updateForm(key, msg)
	case setPrompt:
		return m.updatePrompt(key, msg)
	case setSyncing:
		return m, nil // keys ignored while a sync runs
	}
	if m.focus == 0 {
		return m.updateSettingsNav(key)
	}
	switch m.setCat {
	case catTheme:
		return m.updateThemePane(key)
	case catIcons:
		return m.updateIconsPane(key)
	case catPrefs:
		return m.updatePrefsPane(key)
	}
	return m.updateSourcesPane(key)
}

// updateIconsPane toggles Nerd Font glyphs live and persists the choice.
func (m Model) updateIconsPane(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "left", "esc", "tab":
		m.focus = 0
	case " ", "enter":
		nerd = !nerd
		if err := config.SetTopLevelBool(m.configPath, "nerdfont", nerd); err != nil {
			m.setStatusBad, m.setStatus = true, "could not save: "+err.Error()
		} else if nerd {
			m.setStatusBad, m.setStatus = false, "Nerd Font icons on"
		} else {
			m.setStatusBad, m.setStatus = false, "Nerd Font icons off"
		}
	}
	return m, nil
}

// updatePrefsPane edits the persistent preferences: the clipboard timeout and the
// cache-stale threshold. ←/→ adjust the selected row; esc/tab leave the pane.
func (m Model) updatePrefsPane(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "left", "esc", "tab":
		if key == "left" {
			m.adjustPref(-1)
		} else {
			m.focus = 0
		}
	case "h", "-":
		m.adjustPref(-1)
	case "right", "l", "+", "=":
		m.adjustPref(+1)
	case "up", "ctrl+p", "k":
		if m.prefSel > 0 {
			m.prefSel--
			m.setStatusBad, m.setStatus = false, ""
		}
	case "down", "ctrl+n", "j":
		if m.prefSel < 1 {
			m.prefSel++
			m.setStatusBad, m.setStatus = false, ""
		}
	}
	return m, nil
}

// adjustPref changes the selected preference by d steps and persists it.
func (m *Model) adjustPref(d int) {
	switch m.prefSel {
	case 0: // clipboard timeout: ±5s in [5s, 300s]
		m.timeout = clampDur(m.timeout+time.Duration(d)*5*time.Second, 5*time.Second, 300*time.Second)
	case 1: // cache stale-after: ±1h in [1h, 720h]
		m.staleAfter = clampDur(m.staleAfter+time.Duration(d)*time.Hour, time.Hour, 720*time.Hour)
	}
	m.savePrefs()
}

// savePrefs persists the preferences (best-effort, only if a config file exists).
func (m *Model) savePrefs() {
	if m.configPath == "" {
		return
	}
	if _, err := os.Stat(m.configPath); err != nil {
		return
	}
	if err := config.SetPreferences(m.configPath,
		fmt.Sprintf("%ds", int(m.timeout.Seconds())),
		fmt.Sprintf("%dh", int(m.staleAfter.Hours()))); err != nil {
		m.setStatusBad, m.setStatus = true, "could not save: "+err.Error()
		return
	}
	m.setStatusBad, m.setStatus = false, "saved"
}

func clampDur(d, lo, hi time.Duration) time.Duration {
	if d < lo {
		return lo
	}
	if d > hi {
		return hi
	}
	return d
}

// updateSettingsNav is the left pane: pick a category.
func (m Model) updateSettingsNav(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "ctrl+p":
		if m.setCat > 0 {
			m.setCat--
			m.setStatusBad, m.setStatus = false, ""
		}
	case "down", "ctrl+n":
		if m.setCat < len(settingsCats)-1 {
			m.setCat++
			m.setStatusBad, m.setStatus = false, ""
		}
	case "pgup": // short list: page jumps to the ends
		m.setCat, m.setStatusBad, m.setStatus = 0, false, ""
	case "pgdown":
		m.setCat, m.setStatusBad, m.setStatus = len(settingsCats)-1, false, ""
	case "right", "tab", "enter":
		return m.enterCategory(), nil
	case "t":
		m.setCat = catTheme
		return m.enterCategory(), nil
	case "i":
		m.setCat = catIcons
		return m.enterCategory(), nil
	}
	return m, nil
}

// enterCategory moves focus into the right (content) pane, snapshotting the theme
// so the picker can revert on cancel.
func (m Model) enterCategory() Model {
	m.focus = 1
	m.setStatusBad, m.setStatus = false, ""
	if m.setCat == catTheme {
		m.themeOrig = m.themeName
		m.themeSel = 0
		for i, n := range theme.Names() {
			if n == m.themeName {
				m.themeSel = i
				break
			}
		}
	}
	return m
}

// updateSourcesPane is the right pane for the Sources category.
func (m Model) updateSourcesPane(key string) (tea.Model, tea.Cmd) {
	profs := m.sources()
	switch key {
	case "left", "esc", "tab":
		m.focus = 0
	case "up", "ctrl+p":
		if m.setSel > 0 {
			m.setSel--
			m.setStatusBad, m.setStatus = false, ""
		}
	case "down", "ctrl+n":
		if m.setSel < len(profs)-1 {
			m.setSel++
			m.setStatusBad, m.setStatus = false, ""
		}
	case "pgup":
		_, step := m.panelRows()
		m.setSel, m.setStatusBad, m.setStatus = clampIndex(m.setSel-max(1, step), len(profs)), false, ""
	case "pgdown":
		_, step := m.panelRows()
		m.setSel, m.setStatusBad, m.setStatus = clampIndex(m.setSel+max(1, step), len(profs)), false, ""
	case "a":
		return m.openAddForm(), nil
	case "e":
		if m.setSel < len(profs) {
			return m.openEditForm(profs[m.setSel]), nil
		}
	case "p":
		if m.setSel < len(profs) {
			return m.openSavePassword(profs[m.setSel]), nil
		}
	case "s":
		if m.setSel < len(profs) {
			return m.startSync(profs[m.setSel])
		}
	case "d":
		if m.setSel < len(profs) {
			m.setMode = setRemove
			m.rmToggle, m.rmFile, m.rmPw = 0, false, false
		}
	case "x":
		if m.setSel < len(profs) {
			m = m.clearPassword(profs[m.setSel])
		}
	}
	return m, nil
}

func (m Model) updateRemove(key string, profs []config.Source) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.setMode = setList
	case "up", "ctrl+p":
		if m.rmToggle > 0 {
			m.rmToggle--
		}
	case "down", "ctrl+n", "tab":
		if m.rmToggle < 2 {
			m.rmToggle++
		}
	case " ":
		switch m.rmToggle {
		case 0:
			m.rmFile = !m.rmFile
		case 1:
			m.rmPw = !m.rmPw
		}
	case "enter":
		if m.setSel < len(profs) {
			m = m.doRemove(profs[m.setSel])
		}
		m.setMode = setList
	}
	return m, nil
}

// clearPassword forgets the selected source's saved keyring password.
func (m Model) clearPassword(p config.Source) Model {
	var err error
	if p.Type == config.Pleasant {
		err = keyring.ForgetServer(p.Name)
	} else {
		err = keyring.Forget(p.Name)
	}
	if err != nil {
		m.setStatusBad, m.setStatus = true, "could not clear "+p.Name+": "+err.Error()
		return m
	}
	m.setStatusBad, m.setStatus = false, "cleared "+p.Name+"'s saved password"
	m.setKeyring = keyringStatus(m.sources())
	return m
}

// doRemove removes the selected source, optionally deleting its local file and
// forgetting its keyring password (the shared master only when no other Pleasant
// source remains).
func (m Model) doRemove(p config.Source) Model {
	otherPleasant := 0
	for _, q := range m.sources() {
		if q.Name != p.Name && q.Type == config.Pleasant {
			otherPleasant++
		}
	}

	if _, err := config.RemoveSource(m.configPath, p.Name); err != nil {
		m.setStatusBad, m.setStatus = true, "remove failed: "+err.Error()
		return m
	}
	m.setStatusBad, m.setStatus = false, "removed "+p.Name

	if m.rmFile {
		f := p.Path
		if p.Type == config.Pleasant {
			f = p.Cache
		}
		if err := os.Remove(f); err != nil {
			m.setStatus += " (could not delete the file)"
		} else {
			m.setStatus += ", deleted the file"
		}
	}
	if m.rmPw {
		if p.Type == config.Pleasant {
			_ = keyring.ForgetServer(p.Name)
			if otherPleasant == 0 {
				_ = keyring.ForgetMaster()
			}
		} else {
			_ = keyring.Forget(p.Name)
		}
	}

	profs := m.sources()
	if m.setSel >= len(profs) {
		m.setSel = max(0, len(profs)-1)
	}
	m.setKeyring = keyringStatus(profs)
	return m
}

// removeConfirmView is the overlay shown while confirming a source removal.
func (m Model) removeConfirmView() string {
	profs := m.sources()
	if m.setSel >= len(profs) {
		return ""
	}
	p := profs[m.setSel]
	file := p.Path
	if p.Type == config.Pleasant {
		file = p.Cache
	}

	// An armed checkbox on this screen means "and delete their file too", so it
	// is not the new-and-good green. Green here said "danger" and "good" about
	// one act, directly above a red Remove button.
	check := func(on bool) (string, lipgloss.Style) {
		if on {
			return "[x] ", theme.Bad
		}
		return "[ ] ", theme.Faded
	}
	// Painted in pieces. SelRow was handed a string that already carried nested
	// styles, so its background died at the first reset and the highlight
	// covered the checkbox and nothing else — on the one dialog where knowing
	// which toggle is armed matters most.
	row := func(idx int, segs []rowSeg) string {
		var back lipgloss.TerminalColor = lipgloss.NoColor{}
		if m.rmToggle == idx {
			back = theme.SelBg
		}
		return paintRow(append([]rowSeg{{" ", theme.Faded}}, segs...), "", max(4, m.w-2), back)
	}

	fileBox, fileSt := check(m.rmFile)
	pwBox, pwSt := check(m.rmPw)
	lines := []string{
		"",
		row(0, []rowSeg{
			{fileBox, fileSt},
			{"also delete the file  ", theme.Strong},
			{trunc(file, max(4, m.w-30)), theme.Dimmed},
		}),
		row(1, []rowSeg{
			{pwBox, pwSt},
			{"also forget its saved keyring password", theme.Strong},
		}),
		"",
		"  " + button("Remove", true, m.rmToggle == 2),
	}
	body := box("Remove source", fmt.Sprintf("%q", p.Name), lines, m.w, max(3, m.h-1), true)
	return body + "\n" + m.footer(theme.Faded.Render("↑↓ move · space toggle · ↵ apply · esc cancel"))
}

// settingsLeftW is the width of the category pane. Shared by the layout and the
// mouse hit-test so they cannot disagree about where the panes divide.
const settingsLeftW = 22

// settingsRowAt maps a content row of the right pane to the cursor index it
// belongs to.
//
// Every pane starts its rows at a different offset — the sources table spends a
// line on its column headings, the preferences pane an empty one, the theme
// list starts straight away — and this is the one place that knows it. A second
// copy in the mouse code would drift from the renderers, which is exactly the
// bug the tab hit-test was fixed for once already. TestSettingsRowAtMatchesTheRender
// walks the rendered lines and holds the two together.
func settingsRowAt(cat, row, count int) (int, bool) {
	var first int
	switch cat {
	case catSources:
		first = 1 // the NAME/TYPE/LOCATION heading
	case catPrefs:
		first = 1 // a blank line above the two settings
	case catTheme:
		first = 0
	default:
		return 0, false // icons is one toggle, with no row to land on
	}
	idx := row - first
	if idx < 0 || idx >= count {
		return 0, false
	}
	return idx, true
}

// settingsRowCount is how many rows the right pane's cursor can stop on.
func (m Model) settingsRowCount() int {
	switch m.setCat {
	case catSources:
		return len(m.sources())
	case catTheme:
		return len(theme.Names())
	case catPrefs:
		return 2
	}
	return 0
}

// handleSettingsClick routes a left-click on the Settings tab.
//
// The categories are a list and the right pane is a list; both answer a click
// the way every other list in the program does. The forms and prompts this tab
// raises own the whole screen, so a click through one reaches nothing.
func (m Model) handleSettingsClick(x, y int) (tea.Model, tea.Cmd) {
	if m.setMode != setList {
		return m, nil
	}
	// The header takes row 0 and the panel's top border row 1, as in
	// settingsView; the content starts under them.
	panelsH := max(3, m.h-3)
	if y < 2 || y > panelsH-1 {
		return m, nil
	}
	row := y - 2

	if x < settingsLeftW {
		if row < len(settingsCats) {
			m.focus, m.setCat, m.setStatusBad, m.setStatus = 0, row, false, ""
		}
		return m, nil
	}

	m.focus = 1
	idx, ok := settingsRowAt(m.setCat, row, m.settingsRowCount())
	if !ok {
		return m, nil
	}
	m.setStatusBad, m.setStatus = false, ""
	switch m.setCat {
	case catSources:
		m.setSel = idx
	case catTheme:
		// The theme pane previews as the cursor moves, so landing on a row has
		// to preview too — otherwise a click would select a theme the screen
		// does not show.
		return m.applyThemeAt(idx), nil
	case catPrefs:
		m.prefSel = idx
	}
	return m, nil
}
