package tui

import (
	"fmt"
	"os"

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
)

var settingsCats = []string{"Sources", "Theme", "Icons"}

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
			m.setStatus = "could not save: " + err.Error()
		} else if nerd {
			m.setStatus = "Nerd Font icons on"
		} else {
			m.setStatus = "Nerd Font icons off"
		}
	}
	return m, nil
}

// updateSettingsNav is the left pane: pick a category.
func (m Model) updateSettingsNav(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "ctrl+p":
		if m.setCat > 0 {
			m.setCat--
			m.setStatus = ""
		}
	case "down", "ctrl+n":
		if m.setCat < len(settingsCats)-1 {
			m.setCat++
			m.setStatus = ""
		}
	case "pgup": // short list: page jumps to the ends
		m.setCat, m.setStatus = 0, ""
	case "pgdown":
		m.setCat, m.setStatus = len(settingsCats)-1, ""
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
	m.setStatus = ""
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
			m.setStatus = ""
		}
	case "down", "ctrl+n":
		if m.setSel < len(profs)-1 {
			m.setSel++
			m.setStatus = ""
		}
	case "pgup":
		_, step := m.panelRows()
		m.setSel, m.setStatus = clampIndex(m.setSel-max(1, step), len(profs)), ""
	case "pgdown":
		_, step := m.panelRows()
		m.setSel, m.setStatus = clampIndex(m.setSel+max(1, step), len(profs)), ""
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

func (m Model) updateRemove(key string, profs []config.Profile) (tea.Model, tea.Cmd) {
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
func (m Model) clearPassword(p config.Profile) Model {
	var err error
	if p.Type == config.Pleasant {
		err = keyring.ForgetServer(p.Name)
	} else {
		err = keyring.Forget(p.Name)
	}
	if err != nil {
		m.setStatus = "could not clear " + p.Name + ": " + err.Error()
		return m
	}
	m.setStatus = "cleared " + p.Name + "'s saved password"
	m.setKeyring = keyringStatus(m.sources())
	return m
}

// doRemove removes the selected source, optionally deleting its local file and
// forgetting its keyring password (the shared master only when no other Pleasant
// source remains).
func (m Model) doRemove(p config.Profile) Model {
	otherPleasant := 0
	for _, q := range m.sources() {
		if q.Name != p.Name && q.Type == config.Pleasant {
			otherPleasant++
		}
	}

	if _, err := config.RemoveProfile(m.configPath, p.Name); err != nil {
		m.setStatus = "remove failed: " + err.Error()
		return m
	}
	m.setStatus = "removed " + p.Name

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

	check := func(on bool) string {
		if on {
			return theme.Ok.Render("[x] ")
		}
		return theme.Faded.Render("[ ] ")
	}
	row := func(idx int, s string) string {
		if m.rmToggle == idx {
			return theme.SelRow.Render(" " + trunc(s, max(4, m.w-2)) + " ")
		}
		return "  " + s
	}

	lines := []string{
		"",
		row(0, check(m.rmFile)+"also delete the file  "+theme.Dimmed.Render(trunc(file, max(4, m.w-30)))),
		row(1, check(m.rmPw)+"also forget its saved keyring password"),
		"",
		"  " + button("Remove", true, m.rmToggle == 2),
	}
	body := box("Remove source", fmt.Sprintf("%q", p.Name), lines, m.w, max(3, m.h-1), true)
	return body + "\n" + m.footer(theme.Faded.Render("↑↓ move · space toggle · ↵ apply · esc cancel"))
}
