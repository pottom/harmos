package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/theme"
)

func (m Model) openThemePicker() Model {
	m.setMode = setTheme
	m.setStatus = ""
	m.themeOrig = m.themeName
	m.themeSel = 0
	for i, n := range theme.Names() {
		if n == m.themeName {
			m.themeSel = i
			break
		}
	}
	return m
}

// applyThemeAt previews the theme at idx by making it active immediately.
func (m Model) applyThemeAt(idx int) Model {
	names := theme.Names()
	if idx < 0 || idx >= len(names) {
		return m
	}
	m.themeSel = idx
	if t, ok := theme.Builtin(names[idx]); ok {
		theme.Apply(t)
		m.themeName = names[idx]
	}
	return m
}

func (m Model) updateThemePicker(key string) (tea.Model, tea.Cmd) {
	names := theme.Names()
	switch key {
	case "esc":
		if t, ok := theme.Builtin(m.themeOrig); ok {
			theme.Apply(t)
		}
		m.themeName = m.themeOrig
		m.setMode = setList
	case "up", "ctrl+p":
		if m.themeSel > 0 {
			m = m.applyThemeAt(m.themeSel - 1)
		}
	case "down", "ctrl+n":
		if m.themeSel < len(names)-1 {
			m = m.applyThemeAt(m.themeSel + 1)
		}
	case "enter":
		if err := config.SetTopLevelKey(m.configPath, "theme", m.themeName); err != nil {
			m.setStatus = "could not save theme: " + err.Error()
		} else {
			m.setStatus = "theme set to " + m.themeName
		}
		m.setMode = setList
	}
	return m, nil
}

func (m Model) themePickerView() string {
	var lines []string
	for i, n := range theme.Names() {
		if i == m.themeSel {
			lines = append(lines, theme.SelRow.Render(" "+n+" "))
			continue
		}
		lines = append(lines, "  "+theme.Strong.Render(n))
	}
	body := box("Theme  ·  live preview", m.themeName, lines, m.w, max(3, m.h-1), true)
	return body + "\n" + m.footer(theme.Faded.Render("↑↓ preview · ↵ apply & save · esc cancel"))
}
