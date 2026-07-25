package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/theme"
)

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

// updateThemePane is the right pane for the Theme category: ↑↓ previews live,
// enter saves to the config, left/esc reverts to the theme in effect on entry.
func (m Model) updateThemePane(key string) (tea.Model, tea.Cmd) {
	names := theme.Names()
	switch key {
	case "left", "esc":
		if t, ok := theme.Builtin(m.themeOrig); ok {
			theme.Apply(t)
		}
		m.themeName = m.themeOrig
		m.focus = 0
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
			m.setStatus = "theme saved: " + m.themeName
			m.themeOrig = m.themeName
		}
	}
	return m, nil
}

func (m Model) themeLines(w int) []string {
	// The saved/active theme gets the ● mark. While previewing in the picker
	// (focus 1) the live theme is themeName, so the saved one is themeOrig;
	// otherwise themeName itself is what's active.
	saved := m.themeName
	if m.focus == 1 {
		saved = m.themeOrig
	}
	var out []string
	for i, n := range theme.Names() {
		mark := "  "
		if n == saved {
			mark = theme.Ok.Render("● ")
		}
		switch {
		case m.focus == 1 && i == m.themeSel:
			out = append(out, theme.SelRow.Width(w).Render(trunc("▸ "+n, w)))
		case n == saved:
			out = append(out, mark+theme.Hi.Render(n))
		default:
			out = append(out, mark+theme.Strong.Render(n))
		}
	}
	return out
}
