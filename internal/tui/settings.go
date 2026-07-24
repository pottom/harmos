package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/keyring"
	"github.com/pottom/harmos/internal/theme"
)

// Settings sub-modes.
const (
	setList   = iota // browsing the sources table
	setRemove        // the remove-source confirmation overlay
	setForm          // the add/edit form overlay
	setPrompt        // the save-password prompt overlay
)

// updateSettings handles keys while the Settings tab is active. It takes the raw
// message too so overlays with text inputs can forward keystrokes.
func (m Model) updateSettings(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	profs := m.sources()
	switch m.setMode {
	case setRemove:
		return m.updateRemove(key, profs)
	case setForm:
		return m.updateForm(key, msg)
	case setPrompt:
		return m.updatePrompt(key, msg)
	}

	switch key {
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
		theme.Brand.Render("Remove source ") + theme.Strong.Render(fmt.Sprintf("%q", p.Name)),
		"",
		row(0, check(m.rmFile)+"also delete the file  "+theme.Dimmed.Render(trunc(file, max(4, m.w-30)))),
		row(1, check(m.rmPw)+"also forget its saved keyring password"),
		"",
		row(2, theme.Bad.Render("Remove")),
		"",
		theme.Faded.Render("↑↓ move · space toggle · ↵ apply · esc cancel"),
	}
	for len(lines) < m.h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
